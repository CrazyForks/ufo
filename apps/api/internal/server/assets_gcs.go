package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	gcsDefaultEndpoint = "https://storage.googleapis.com"
	gcsDefaultTokenURI = "https://oauth2.googleapis.com/token"
	gcsStorageScope    = "https://www.googleapis.com/auth/devstorage.full_control"
)

type gcsServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type gcsAssetStore struct {
	httpClient *http.Client
	bucket     string
	prefix     string
	endpoint   string
	expiresIn  time.Duration
	account    gcsServiceAccount
	privateKey *rsa.PrivateKey
	tokenMu    sync.Mutex
	token      string
	tokenExp   time.Time
}

func newGCSAssetStore() (*gcsAssetStore, error) {
	bucket := strings.TrimSpace(os.Getenv("UFO_HUB_ASSET_GCS_BUCKET"))
	if bucket == "" {
		return nil, fmt.Errorf("UFO_HUB_ASSET_GCS_BUCKET is required")
	}
	account, key, err := loadGCSServiceAccount()
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("UFO_HUB_ASSET_GCS_ENDPOINT")), "/")
	if endpoint == "" {
		endpoint = gcsDefaultEndpoint
	}
	return &gcsAssetStore{
		httpClient: http.DefaultClient,
		bucket:     bucket,
		prefix:     strings.Trim(strings.TrimSpace(os.Getenv("UFO_HUB_ASSET_GCS_PREFIX")), "/"),
		endpoint:   endpoint,
		expiresIn:  time.Duration(envInt("UFO_HUB_ASSET_SIGNED_URL_SECONDS", 900)) * time.Second,
		account:    account,
		privateKey: key,
	}, nil
}

func loadGCSServiceAccount() (gcsServiceAccount, *rsa.PrivateKey, error) {
	raw := strings.TrimSpace(os.Getenv("UFO_HUB_ASSET_GCS_CREDENTIALS_JSON"))
	if raw == "" {
		path := strings.TrimSpace(os.Getenv("UFO_HUB_ASSET_GCS_CREDENTIALS_FILE"))
		if path == "" {
			path = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
		}
		if path == "" {
			return gcsServiceAccount{}, nil, fmt.Errorf("UFO_HUB_ASSET_GCS_CREDENTIALS_FILE or UFO_HUB_ASSET_GCS_CREDENTIALS_JSON is required")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return gcsServiceAccount{}, nil, err
		}
		raw = string(b)
	}
	var account gcsServiceAccount
	if err := json.Unmarshal([]byte(raw), &account); err != nil {
		return gcsServiceAccount{}, nil, err
	}
	if account.ClientEmail == "" || account.PrivateKey == "" {
		return gcsServiceAccount{}, nil, fmt.Errorf("GCS service account credentials are missing client_email or private_key")
	}
	if account.TokenURI == "" {
		account.TokenURI = gcsDefaultTokenURI
	}
	key, err := parseRSAPrivateKey([]byte(account.PrivateKey))
	if err != nil {
		return gcsServiceAccount{}, nil, err
	}
	return account, key, nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid RSA private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("GCS private key is not RSA")
	}
	return key, nil
}

func (s *gcsAssetStore) Backend() string {
	return assetBackendGCS
}

func (s *gcsAssetStore) key(objectKey string) string {
	objectKey = strings.TrimLeft(objectKey, "/")
	if s.prefix == "" {
		return objectKey
	}
	return s.prefix + "/" + objectKey
}

func (s *gcsAssetStore) objectURL(objectKey string) string {
	return s.endpoint + "/" + gcsPathEscape(s.bucket) + "/" + gcsPathEscape(s.key(objectKey))
}

func (s *gcsAssetStore) accessToken(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExp.Add(-time.Minute)) {
		return s.token, nil
	}
	assertion, err := s.jwtAssertion(time.Now().UTC())
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", gcsHTTPError(resp)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("GCS token response missing access_token")
	}
	expiresIn := out.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	s.token = out.AccessToken
	s.tokenExp = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return s.token, nil
}

func (s *gcsAssetStore) jwtAssertion(now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss":   s.account.ClientEmail,
		"scope": gcsStorageScope,
		"aud":   s.account.TokenURI,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sig, err := signRSA256(s.privateKey, unsigned)
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *gcsAssetStore) request(ctx context.Context, method, objectKey string, body io.Reader, opts assetPutOptions) (*http.Response, error) {
	token, err := s.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, s.objectURL(objectKey), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}
	if opts.ByteSize > 0 {
		req.ContentLength = opts.ByteSize
	}
	return s.httpClient.Do(req)
}

func (s *gcsAssetStore) Put(ctx context.Context, objectKey string, body []byte, opts assetPutOptions) error {
	_, err := s.PutReader(ctx, objectKey, bytes.NewReader(body), assetPutOptions{ContentType: opts.ContentType, ByteSize: int64(len(body))})
	return err
}

func (s *gcsAssetStore) PutReader(ctx context.Context, objectKey string, body io.Reader, opts assetPutOptions) (int64, error) {
	counting := &countingReader{r: body}
	resp, err := s.request(ctx, http.MethodPut, objectKey, counting, opts)
	if err != nil {
		return counting.n, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return counting.n, gcsHTTPError(resp)
	}
	return counting.n, nil
}

func (s *gcsAssetStore) PresignUpload(_ context.Context, objectKey string, opts assetPutOptions) (assetUploadTarget, error) {
	headers := map[string]string{"host": s.signedHost()}
	outHeaders := map[string]string{}
	if opts.ContentType != "" {
		headers["content-type"] = opts.ContentType
		outHeaders["Content-Type"] = opts.ContentType
	}
	if opts.ByteSize > 0 {
		headers["content-length"] = fmt.Sprintf("%d", opts.ByteSize)
	}
	u, err := s.signedURL(http.MethodPut, objectKey, headers, nil)
	if err != nil {
		return assetUploadTarget{}, err
	}
	return assetUploadTarget{Method: http.MethodPut, URL: u, Headers: outHeaders, ExpiresAt: time.Now().UTC().Add(s.expiresIn)}, nil
}

func (s *gcsAssetStore) PresignGet(_ context.Context, objectKey string, opts assetGetOptions) (assetUploadTarget, error) {
	query := map[string]string{}
	if opts.ContentType != "" {
		query["response-content-type"] = opts.ContentType
	}
	if opts.Filename != "" {
		query["response-content-disposition"] = assetContentDisposition(opts.Disposition, opts.Filename)
	}
	u, err := s.signedURL(http.MethodGet, objectKey, map[string]string{"host": s.signedHost()}, query)
	if err != nil {
		return assetUploadTarget{}, err
	}
	return assetUploadTarget{Method: http.MethodGet, URL: u, Headers: map[string]string{}, ExpiresAt: time.Now().UTC().Add(s.expiresIn)}, nil
}

func (s *gcsAssetStore) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	resp, err := s.request(ctx, http.MethodGet, objectKey, nil, assetPutOptions{})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, gcsHTTPError(resp)
	}
	return resp.Body, nil
}

func (s *gcsAssetStore) Delete(ctx context.Context, objectKey string) error {
	resp, err := s.request(ctx, http.MethodDelete, objectKey, nil, assetPutOptions{})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return gcsHTTPError(resp)
	}
	return nil
}

func (s *gcsAssetStore) Stat(ctx context.Context, objectKey string) (assetStat, error) {
	resp, err := s.request(ctx, http.MethodHead, objectKey, nil, assetPutOptions{})
	if err != nil {
		return assetStat{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return assetStat{}, gcsHTTPError(resp)
	}
	return assetStat{ByteSize: resp.ContentLength, Checksums: gcsHashes(resp.Header.Get("X-Goog-Hash"))}, nil
}

func (s *gcsAssetStore) signedHost() string {
	u, err := url.Parse(s.endpoint)
	if err != nil || u.Host == "" {
		return "storage.googleapis.com"
	}
	return u.Host
}

func (s *gcsAssetStore) signedURL(method, objectKey string, headers, query map[string]string) (string, error) {
	now := time.Now().UTC()
	date := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	scope := date + "/auto/storage/goog4_request"
	canonicalURI := "/" + gcsPathEscape(s.bucket) + "/" + gcsPathEscape(s.key(objectKey))

	params := map[string]string{}
	for k, v := range query {
		params[k] = v
	}
	params["X-Goog-Algorithm"] = "GOOG4-RSA-SHA256"
	params["X-Goog-Credential"] = s.account.ClientEmail + "/" + scope
	params["X-Goog-Date"] = timestamp
	params["X-Goog-Expires"] = fmt.Sprintf("%d", int(s.expiresIn.Seconds()))
	params["X-Goog-SignedHeaders"] = signedHeaderNames(headers)

	canonicalQuery := canonicalQueryString(params)
	canonicalHeaders := canonicalHeaders(headers)
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		params["X-Goog-SignedHeaders"],
		"UNSIGNED-PAYLOAD",
	}, "\n")
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"GOOG4-RSA-SHA256",
		timestamp,
		scope,
		hex.EncodeToString(requestHash[:]),
	}, "\n")
	sig, err := signRSA256(s.privateKey, stringToSign)
	if err != nil {
		return "", err
	}
	return s.endpoint + canonicalURI + "?" + canonicalQuery + "&X-Goog-Signature=" + hex.EncodeToString(sig), nil
}

func signRSA256(key *rsa.PrivateKey, s string) ([]byte, error) {
	sum := sha256.Sum256([]byte(s))
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
}

func signedHeaderNames(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, strings.ToLower(strings.TrimSpace(k)))
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
}

func canonicalHeaders(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	byKey := map[string]string{}
	for k, v := range headers {
		key := strings.ToLower(strings.TrimSpace(k))
		keys = append(keys, key)
		byKey[key] = strings.TrimSpace(v)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(':')
		b.WriteString(byKey[key])
		b.WriteByte('\n')
	}
	return b.String()
}

func canonicalQueryString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, gcsQueryEscape(k)+"="+gcsQueryEscape(params[k]))
	}
	return strings.Join(parts, "&")
}

func gcsPathEscape(s string) string {
	parts := strings.Split(s, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func gcsQueryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func gcsHashes(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k != "" && v != "" {
			k = strings.ToLower(k)
			out[k] = normalizeChecksum(k, v)
		}
	}
	return out
}

func gcsHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if len(body) == 0 {
		return fmt.Errorf("GCS returned %s", resp.Status)
	}
	return fmt.Errorf("GCS returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
