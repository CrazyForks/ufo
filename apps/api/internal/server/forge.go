package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"ufo/apps/api/internal/db"
)

const (
	defaultPullRequestBaseBranch  = "orbit"
	defaultForgeCredentialEnv     = "UFO_ROVER_FORGE_TOKEN"
	defaultGitHubAppPrivateKeyEnv = "UFO_ROVER_GITHUB_APP_PRIVATE_KEY"
	defaultCIWaitTimeoutSeconds   = 24 * 60 * 60
	forgeActionStaleSeconds       = 600
	forgeSyncBackoffBaseSec       = 15
	forgeSyncBackoffMaxSec        = 300
)

type forgeConfig struct {
	ID                int64
	PublicID          string
	Key               string
	Provider          string
	BaseURL           string
	Repo              string
	DefaultBaseBranch string
	CredentialKind    string
	Credential        json.RawMessage
	CredentialName    string
}

func forgeConfigFromRow(row db.Forge) (forgeConfig, bool) {
	provider := strings.ToLower(strings.TrimSpace(row.Provider))
	if provider != "github" && provider != "gitlab" {
		return forgeConfig{}, false
	}
	repo := strings.TrimSpace(row.Repo)
	if repo == "" {
		return forgeConfig{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(row.BaseUrl), "/")
	if baseURL == "" {
		switch provider {
		case "github":
			baseURL = "https://api.github.com"
		case "gitlab":
			baseURL = "https://gitlab.com/api/v4"
		}
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return forgeConfig{}, false
	}
	kind := strings.TrimSpace(row.CredentialKind)
	if kind == "" {
		kind = "rover_env"
	}
	cred := row.Credential
	if len(cred) == 0 {
		cred = []byte("{}")
	}
	name := primaryCredentialEnv(kind, cred)
	base := strings.TrimSpace(row.DefaultBaseBranch)
	if base == "" {
		base = "main"
	}
	return forgeConfig{
		ID: row.ID, PublicID: uuidStr(row.PublicID), Key: row.Key,
		Provider: provider, BaseURL: baseURL, Repo: repo,
		DefaultBaseBranch: base, CredentialKind: kind,
		Credential: metadataJSON(cred), CredentialName: name,
	}, true
}

func primaryCredentialEnv(kind string, cred []byte) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(cred, &m)
	str := func(keys ...string) string {
		for _, k := range keys {
			raw, ok := m[k]
			if !ok {
				continue
			}
			var s string
			if json.Unmarshal(raw, &s) == nil {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
		return ""
	}
	switch kind {
	case "github_app":
		if n := str("private_key_env", "name"); n != "" {
			return n
		}
		return defaultGitHubAppPrivateKeyEnv
	case "gitlab_app":
		if n := str("secret_env", "name"); n != "" {
			return n
		}
		return defaultForgeCredentialEnv
	case "secret_ref":
		if n := str("ref", "name"); n != "" {
			return n
		}
		return defaultForgeCredentialEnv
	default:
		if n := str("name"); n != "" {
			return n
		}
		return defaultForgeCredentialEnv
	}
}

func (s *Server) resolveMissionForge(ctx context.Context, mission db.Mission, preferredKey string) (forgeConfig, bool, error) {
	granted, err := s.q.ListGrantedForgesForMission(ctx, db.ListGrantedForgesForMissionParams{
		MissionID: mission.ID, FleetID: mission.FleetID,
	})
	if err != nil {
		return forgeConfig{}, false, err
	}
	if len(granted) == 0 {
		return forgeConfig{}, false, nil
	}
	key := strings.TrimSpace(preferredKey)
	if key != "" {
		for _, row := range granted {
			if row.Key == key {
				cfg, ok := forgeConfigFromRow(row)
				return cfg, ok, nil
			}
		}
		return forgeConfig{}, false, nil
	}
	if len(granted) == 1 {
		cfg, ok := forgeConfigFromRow(granted[0])
		return cfg, ok, nil
	}
	return forgeConfig{}, false, nil
}

func (s *Server) resolveOperationForge(ctx context.Context, op db.Operation, preferredKey string) (forgeConfig, bool, error) {
	mission, err := s.q.GetMission(ctx, op.MissionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return forgeConfig{}, false, nil
		}
		return forgeConfig{}, false, err
	}
	key := strings.TrimSpace(preferredKey)
	if key == "" {
		if k := operationMetadataNestedString(op.Metadata, "forge", "key"); k != "" {
			key = k
		}
	}
	if key == "" {
		if k, ok := metadataString(mission.Metadata, "forge_key"); ok {
			key = k
		}
	}
	return s.resolveMissionForge(ctx, mission, key)
}

func (s *Server) forgeConfigByID(ctx context.Context, fleetID, forgeID int64) (forgeConfig, bool, error) {
	row, err := s.q.GetForge(ctx, db.GetForgeParams{ID: forgeID, FleetID: fleetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return forgeConfig{}, false, nil
		}
		return forgeConfig{}, false, err
	}
	cfg, ok := forgeConfigFromRow(row)
	return cfg, ok, nil
}

func (cfg routineOperationConfig) createPullRequest() bool {
	return cfg.CreatePullRequest
}

func (cfg routineOperationConfig) pullRequestBaseBranch() string {
	if b := strings.TrimSpace(cfg.PullRequestBaseBranch); b != "" {
		return normalizeSourceBranchName(b)
	}
	return defaultPullRequestBaseBranch
}

func (cfg routineOperationConfig) pullRequestLabels() []string {
	return cfg.PullRequestLabels
}

func (cfg routineOperationConfig) checksCommands() []string {
	return cfg.ChecksCommands
}

func expandBranchTemplate(tmpl string, routineKey string, op db.Operation, pulseID string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	if !strings.Contains(tmpl, "{{") {
		return normalizeSourceBranchName(tmpl)
	}
	key := normalizeSourceBranchName(routineKey)
	if key == "" {
		key = "routine"
	}
	seq := fmt.Sprintf("%d", op.Sequence)
	if op.Sequence <= 0 {
		id := strings.ReplaceAll(uuidStr(op.PublicID), "-", "")
		if len(id) > 8 {
			id = id[:8]
		}
		seq = id
	}
	pulse := strings.TrimSpace(pulseID)
	pulse = strings.ReplaceAll(pulse, "-", "")
	if len(pulse) > 8 {
		pulse = pulse[:8]
	}
	repl := map[string]string{
		"{{routine_key}}":  key,
		"{{sequence}}":     seq,
		"{{pulse}}":        pulse,
		"{{operation_id}}": strings.ReplaceAll(uuidStr(op.PublicID), "-", ""),
	}
	out := tmpl
	for k, v := range repl {
		if v == "" {
			v = "x"
		}
		out = strings.ReplaceAll(out, k, v)
	}
	out = normalizeSourceBranchName(out)
	if out == "" {
		return "ufo/" + key + "/" + seq
	}
	return out
}

type acceptedForgeAction struct {
	ID                    string          `json:"id"`
	Kind                  string          `json:"kind"`
	Provider              string          `json:"provider"`
	BaseURL               string          `json:"base_url"`
	Repo                  string          `json:"repo"`
	HeadBranch            string          `json:"head_branch"`
	BaseBranch            string          `json:"base_branch"`
	CommitSHA             string          `json:"commit_sha"`
	Title                 string          `json:"title"`
	Body                  string          `json:"body"`
	CredentialKind        string          `json:"credential_kind"`
	CredentialName        string          `json:"credential_name"`
	Credential            json.RawMessage `json:"credential,omitempty"`
	ForgeKey              string          `json:"forge_key,omitempty"`
	ChecksCommands        []string        `json:"checks_commands,omitempty"`
	ChecksTimeoutSeconds  int             `json:"checks_timeout_seconds,omitempty"`
	ShipBaseSync          string          `json:"ship_base_sync,omitempty"`
	LeaseSeconds          int             `json:"lease_seconds"`
	OperationID           string          `json:"operation_id,omitempty"`
	OperationWorktreeName string          `json:"operation_worktree_name,omitempty"`
	OperationCreatedAt    string          `json:"operation_created_at,omitempty"`
}

type completeForgeActionReq struct {
	Status       string          `json:"status"`
	RemoteURL    string          `json:"remote_url"`
	RemoteNumber *int32          `json:"remote_number"`
	ResultSHA    string          `json:"result_sha"`
	CommitSHA    string          `json:"commit_sha"`
	Message      string          `json:"message"`
	Metadata     json.RawMessage `json:"metadata"`
	PRStatus     string          `json:"pr_status"`
	HeadSHA      string          `json:"head_sha"`
	Mergeable    *bool           `json:"mergeable"`
	CIStatus     string          `json:"ci_status"`
	PRTitle      string          `json:"pr_title"`
}

var validForgeActionKind = map[string]bool{
	"update_base_branch": true, "push_head_branch": true, "merge_head_into_base_branch": true,
	"open_pull_request": true, "sync_pull_request": true,
	"merge_pull_request": true, "discover_pull_request": true,
}

var validForgeActionFinalStatus = map[string]bool{
	"succeeded": true, "failed": true, "conflicted": true,
}

func (s *Server) acceptForgeAction(w http.ResponseWriter, r *http.Request) {
	rv := currentRover(r)
	ctx := r.Context()
	action, err := s.q.AcceptNextForgeAction(ctx, db.AcceptNextForgeActionParams{
		FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
		StaleSeconds: forgeActionStaleSeconds,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		serverError(w, err)
		return
	}
	cfg := forgeConfig{
		Provider: action.Provider, BaseURL: action.BaseUrl, Repo: action.Repo,
		CredentialKind: "rover_env", CredentialName: defaultForgeCredentialEnv,
	}
	if forgeID, ok := metadataInt(action.Metadata, "forge_id"); ok && forgeID > 0 {
		resolved, ok, err := s.forgeConfigByID(ctx, action.FleetID, int64(forgeID))
		if err != nil || !ok {
			httpError(w, http.StatusConflict, "forge connection missing for action")
			return
		}
		cfg = resolved
	} else if action.OperationID.Valid {
		if op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: action.OperationID.Int64, FleetID: action.FleetID}); err == nil {
			preferred := ""
			if k, ok := metadataString(action.Metadata, "forge_key"); ok {
				preferred = k
			}
			if resolved, ok, err := s.resolveOperationForge(ctx, op, preferred); err == nil && ok {
				cfg = resolved
			}
		}
	}
	resp := acceptedForgeAction{
		ID: uuidStr(action.PublicID), Kind: action.Kind,
		Provider: action.Provider, BaseURL: action.BaseUrl, Repo: action.Repo,
		HeadBranch: action.HeadBranch, BaseBranch: action.BaseBranch,
		CommitSHA: action.CommitSha, Title: action.Title, Body: action.Body,
		CredentialKind: cfg.CredentialKind, CredentialName: cfg.CredentialName,
		Credential: cfg.Credential, ForgeKey: cfg.Key,
		ChecksCommands:       forgeMetaStringSlice(action.Metadata, "checks_commands"),
		ChecksTimeoutSeconds: forgeMetaInt(action.Metadata, "checks_timeout_seconds"),
		ShipBaseSync:         forgeMetaString(action.Metadata, "ship_base_sync"),
		LeaseSeconds:         forgeActionStaleSeconds,
	}
	if action.OperationID.Valid {
		op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: action.OperationID.Int64, FleetID: action.FleetID})
		if err == nil {
			resp.OperationID = uuidStr(op.PublicID)
			resp.OperationCreatedAt = op.CreatedAt.Time.UTC().Format(time.RFC3339)
			if name, err := s.operationWorktreeName(ctx, op); err == nil {
				resp.OperationWorktreeName = name
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) completeForgeAction(w http.ResponseWriter, r *http.Request) {
	rv := currentRover(r)
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var req completeForgeActionReq
	if !readJSONLimit(w, r, &req, maxLargeBody) {
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if !validForgeActionFinalStatus[req.Status] {
		httpError(w, http.StatusBadRequest, "invalid forge action state")
		return
	}
	ctx := r.Context()
	num := pgtype.Int4{}
	if req.RemoteNumber != nil {
		num = pgtype.Int4{Int32: *req.RemoteNumber, Valid: true}
	}
	meta := sourceActionMetadata(req.Metadata)
	worker, tx, err := s.transactional(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	if tx != nil {
		defer tx.Rollback(ctx)
	}
	action, err := worker.q.CompleteForgeAction(ctx, db.CompleteForgeActionParams{
		PublicID: pid, FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
		Status: req.Status, RemoteUrl: strings.TrimSpace(req.RemoteURL), RemoteNumber: num,
		ResultSha: strings.TrimSpace(req.ResultSHA), CommitSha: strings.TrimSpace(req.CommitSHA),
		Message: strings.TrimSpace(req.Message), Metadata: meta,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			status, getErr := worker.q.GetForgeActionStatusForRover(ctx, db.GetForgeActionStatusForRoverParams{
				PublicID: pid, FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
			})
			if getErr == nil && validForgeActionFinalStatus[status] {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if errors.Is(getErr, pgx.ErrNoRows) || getErr == nil {
				httpError(w, http.StatusNotFound, "forge action not found")
				return
			}
			err = getErr
		}
		serverError(w, err)
		return
	}
	worker.afterForgeAction(ctx, action, req)
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.forgeActionDTO(action))
}

func (s *Server) heartbeatForgeAction(w http.ResponseWriter, r *http.Request) {
	rv := currentRover(r)
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	_, err := s.q.HeartbeatForgeAction(r.Context(), db.HeartbeatForgeActionParams{
		PublicID: pid, FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			status, getErr := s.q.GetForgeActionStatusForRover(r.Context(), db.GetForgeActionStatusForRoverParams{
				PublicID: pid, FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
			})
			if getErr == nil && validForgeActionFinalStatus[status] {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
				serverError(w, getErr)
				return
			}
			httpError(w, http.StatusNotFound, "forge action not active")
			return
		}
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) forgeActionDTO(a db.ForgeAction) map[string]any {
	out := map[string]any{
		"id": uuidStr(a.PublicID), "kind": a.Kind, "status": a.Status,
		"provider": a.Provider, "base_url": a.BaseUrl, "repo": a.Repo,
		"head_branch": a.HeadBranch, "base_branch": a.BaseBranch,
		"commit_sha": a.CommitSha, "title": a.Title, "body": a.Body,
		"remote_url": a.RemoteUrl, "result_sha": a.ResultSha, "message": a.Message,
		"created_at": a.CreatedAt.Time.UTC(), "updated_at": a.UpdatedAt.Time.UTC(),
	}
	if a.RemoteNumber.Valid {
		out["remote_number"] = a.RemoteNumber.Int32
	}
	return out
}

func (s *Server) afterForgeAction(ctx context.Context, action db.ForgeAction, req completeForgeActionReq) {
	if !action.OperationID.Valid {
		return
	}
	op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: action.OperationID.Int64, FleetID: action.FleetID})
	if err != nil {
		return
	}
	switch action.Kind {
	case "push_head_branch":
		if action.Status != "succeeded" {
			s.failForgeShip(ctx, op, action, "push failed")
			return
		}
		if forgeMetaBool(action.Metadata, "next_open_pull_request") {
			s.enqueueOpenPullRequest(ctx, op, action)
		} else if forgeMetaBool(action.Metadata, "next_merge_head_into_base_branch") {
			s.enqueueMergeHeadIntoBaseBranch(ctx, op, action)
		}
	case "open_pull_request":
		if action.Status != "succeeded" {
			s.failForgeShip(ctx, op, action, "open pull request failed")
			return
		}
		_, _ = recordUFOPullRequest(ctx, s.q, op, action, req, true, "forge_ship")
		s.enqueueSyncPullRequest(ctx, op, action)
	case "sync_pull_request":
		s.applyPullRequestSync(ctx, action, req)
		if action.Status != "succeeded" {
			s.failForgeShip(ctx, op, action, "sync pull request failed")
			return
		}
		ci := strings.TrimSpace(req.CIStatus)
		if ci == "" {
			ci = "unknown"
		}
		if ci == "failure" {
			s.failForgeShip(ctx, op, action, "forge CI failed")
			return
		}
		if ci == "success" || (ci == "unknown" && req.Mergeable != nil && *req.Mergeable) {
			s.enqueueMergePullRequest(ctx, op, action)
			return
		}
		attempts := forgeMetaInt(action.Metadata, "sync_attempts") + 1
		if s.forgeCIWaitTimedOut(action, attempts) {
			s.failForgeShip(ctx, op, action, "timed out waiting for forge CI")
			return
		}
		s.enqueueSyncPullRequestAttempt(ctx, op, action, attempts)
	case "merge_pull_request", "merge_head_into_base_branch":
		if action.Status != "succeeded" {
			s.failForgeShip(ctx, op, action, action.Kind+" failed")
			return
		}
		s.markForgeShipSucceeded(ctx, op, action)
	case "update_base_branch":
		if action.Status == "conflicted" || (action.Status == "failed" && looksLikeGitConflict(action.Message)) {
			if s.requeuePilotForShipBaseSync(ctx, op, action) {
				return
			}
			s.failForgeShip(ctx, op, action, "ship base sync conflicted and pilot resolve was not queued")
			return
		}
		if action.Status != "succeeded" {
			s.failForgeShip(ctx, op, action, "ship base sync failed")
			return
		}
		if forgeMetaBool(action.Metadata, "next_push_head_branch") {
			s.enqueuePushBranchAfterBaseSync(ctx, op, action)
		}
	case "discover_pull_request":
		s.afterDiscoverPullRequest(ctx, op, action, req)
	}
}

func looksLikeGitConflict(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "conflict") || strings.Contains(m, "could not apply") ||
		strings.Contains(m, "needs merge") || strings.Contains(m, "unmerged")
}

func forgeMetaBool(meta []byte, key string) bool {
	raw, ok := metadataMap(meta)[key]
	if !ok {
		return false
	}
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}

func forgeMetaInt(meta []byte, key string) int {
	raw, ok := metadataMap(meta)[key]
	if !ok {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f)
	}
	return 0
}

func forgeMetaString(meta []byte, key string) string {
	raw, ok := metadataMap(meta)[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func forgeMetaStringSlice(meta []byte, key string) []string {
	raw, ok := metadataMap(meta)[key]
	if !ok {
		return nil
	}
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	clean := make([]string, 0, len(out))
	for _, s := range out {
		if t := strings.TrimSpace(s); t != "" {
			clean = append(clean, t)
		}
	}
	return clean
}

func forgeSyncBackoffSec(attempts int) int {
	if attempts < 1 {
		attempts = 1
	}
	sec := forgeSyncBackoffBaseSec
	for i := 1; i < attempts && sec < forgeSyncBackoffMaxSec; i++ {
		sec *= 2
		if sec > forgeSyncBackoffMaxSec {
			sec = forgeSyncBackoffMaxSec
		}
	}
	return sec
}

func (s *Server) forgeCIWaitTimedOut(action db.ForgeAction, nextAttempts int) bool {
	limitSeconds := forgeMetaInt(action.Metadata, "ci_wait_timeout_seconds")
	if limitSeconds <= 0 {
		limitSeconds = defaultCIWaitTimeoutSeconds
	}
	started := forgeMetaString(action.Metadata, "ci_wait_started_at")
	if started == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return nextAttempts > 60
	}
	return time.Since(t) > time.Duration(limitSeconds)*time.Second
}

func effectiveCIWaitTimeoutSeconds(rop routineOperationConfig) int {
	if rop.CIWaitTimeoutSeconds != nil && *rop.CIWaitTimeoutSeconds > 0 {
		return *rop.CIWaitTimeoutSeconds
	}
	return defaultCIWaitTimeoutSeconds
}

func patchOperationLoopMetadata(ctx context.Context, q *db.Queries, op db.Operation, patch map[string]any) error {
	loopMetadata, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return q.MergeOperationLoopMetadata(ctx, db.MergeOperationLoopMetadataParams{
		LoopMetadata: loopMetadata, ID: op.ID, FleetID: op.FleetID,
	})
}

func (s *Server) failForgeShip(ctx context.Context, op db.Operation, action db.ForgeAction, reason string) {
	msg := strings.TrimSpace(action.Message)
	if msg == "" {
		msg = reason
	}
	_ = patchOperationLoopMetadata(ctx, s.q, op, map[string]any{
		"forge_ship_failed":            true,
		"forge_ship_blocked_operation": loopMetadataBool(op.Metadata, "forge_ship_blocked_operation") || op.Status == "done",
		"pending_forge_ship":           false,
	})
	if op2, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: op.ID, FleetID: op.FleetID}); err == nil {
		op = op2
	}
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system",
		Body: fmt.Sprintf("Forge ship (%s) failed: %s", action.Kind, msg),
	})
	if op.Status == "done" {
		_ = s.setOperationStatus(ctx, s.q, op, "blocked")
		if op2, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: op.ID, FleetID: op.FleetID}); err == nil {
			op = op2
		}
	}
	s.notifyMembers(ctx, op.FleetID, op.ID, "forge_ship_failed", "action_required",
		"Forge ship failed: "+op.Title, msg)
	s.maybeQueueDiscoverPullRequest(ctx, op)
}

const maxPRDiscoverTries = 3

func discoverAttemptCount(meta []byte, sha string) int {
	sha = strings.TrimSpace(sha)
	if sha == "" || loopMetadataString(meta, "pr_discover_attempt_sha") != sha {
		return 0
	}
	return loopMetadataInt(meta, "pr_discover_attempts")
}

type discoverFailureClass int

const (
	discoverRetry discoverFailureClass = iota
	discoverStop
)

func classifyDiscoverRoverFailure(msg string) discoverFailureClass {
	m := strings.TrimSpace(msg)
	if m == "" {
		return discoverRetry
	}
	if strings.HasPrefix(m, "no open pull request for ") ||
		strings.HasPrefix(m, "no open merge request for ") ||
		strings.HasSuffix(m, "; not linking") {
		return discoverStop
	}
	return discoverRetry
}

func (s *Server) applyDiscoverFailure(ctx context.Context, op db.Operation, shaKey, msg string, class discoverFailureClass) {
	switch class {
	case discoverStop:
		_ = patchOperationLoopMetadata(ctx, s.q, op, map[string]any{"pr_discover_for_sha": ""})
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: fmt.Sprintf("PR discover: %s", msg),
		})
	default:
		_ = patchOperationLoopMetadata(ctx, s.q, op, map[string]any{"pr_discover_for_sha": ""})
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: fmt.Sprintf("PR discover: %s; will retry.", msg),
		})
		if fresh, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: op.ID, FleetID: op.FleetID}); err == nil {
			s.maybeQueueDiscoverPullRequest(ctx, fresh)
		}
	}
}

func (s *Server) afterDiscoverPullRequest(ctx context.Context, op db.Operation, action db.ForgeAction, req completeForgeActionReq) {
	shaKey := strings.TrimSpace(action.CommitSha)
	if shaKey == "" {
		shaKey = loopMetadataString(op.Metadata, "last_commit_sha")
	}
	if action.Status != "succeeded" {
		msg := strings.TrimSpace(action.Message)
		if msg == "" {
			msg = "no unique open pull request for auto-commit branch"
		}
		s.applyDiscoverFailure(ctx, op, shaKey, msg, classifyDiscoverRoverFailure(msg))
		return
	}
	url := strings.TrimSpace(req.RemoteURL)
	if url == "" {
		url = strings.TrimSpace(action.RemoteUrl)
	}
	patch := map[string]any{
		"forge_ship_blocked_operation": false,
		"forge_ship_failed":            false,
		"pending_forge_ship":           true,
		"shipped":                      false,
	}
	if shaKey != "" {
		patch["pr_discover_for_sha"] = shaKey
	}
	if req.RemoteNumber != nil {
		patch["pull_request_number"] = *req.RemoteNumber
	} else if action.RemoteNumber.Valid {
		patch["pull_request_number"] = action.RemoteNumber.Int32
	}
	if url != "" {
		patch["pull_request_url"] = url
	}
	if err := s.recordDiscoverPullRequestRecovery(ctx, op, action, req, patch); err != nil {
		log.Printf("discover recovery op %d: %v", op.ID, err)
		s.applyDiscoverFailure(ctx, op, shaKey, err.Error(), discoverRetry)
		return
	}
	if op.Status == "blocked" && loopMetadataBool(op.Metadata, "forge_ship_blocked_operation") {
		_ = s.setOperationStatus(ctx, s.q, op, "done")
	}
	s.enqueueSyncPullRequest(ctx, op, action)
	body := "Linked open pull request from auto-commit branch; resuming forge ship."
	if url != "" {
		body = fmt.Sprintf("Linked open pull request %s; resuming forge ship.", url)
	}
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system", Body: body,
	})
}

func (s *Server) recordDiscoverPullRequestRecovery(ctx context.Context, op db.Operation, action db.ForgeAction, req completeForgeActionReq, patch map[string]any) error {
	if s.pool == nil {
		linked, linkMsg := recordUFOPullRequest(ctx, s.q, op, action, req, false, "forge_discover")
		if !linked {
			if linkMsg == "" {
				linkMsg = "linking the pull request in UFO failed"
			}
			return errors.New(linkMsg)
		}
		if err := patchOperationLoopMetadata(ctx, s.q, op, patch); err != nil {
			return fmt.Errorf("update operation recovery state: %w", err)
		}
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	linked, linkMsg := recordUFOPullRequest(ctx, qtx, op, action, req, false, "forge_discover")
	if !linked {
		if linkMsg == "" {
			linkMsg = "linking the pull request in UFO failed"
		}
		return errors.New(linkMsg)
	}
	if err := patchOperationLoopMetadata(ctx, qtx, op, patch); err != nil {
		return fmt.Errorf("update operation recovery state: %w", err)
	}
	return tx.Commit(ctx)
}

func forgeSHAEqual(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return a == b
}

func matchingOpenPR(prs []db.PullRequest, provider, baseURL, repo, headBranch, baseBranch, headSHA string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	headBranch = strings.TrimSpace(headBranch)
	baseBranch = strings.TrimSpace(baseBranch)
	headSHA = strings.TrimSpace(headSHA)
	for _, pr := range prs {
		if !strings.EqualFold(strings.TrimSpace(pr.Status), "open") {
			continue
		}
		if strings.ToLower(strings.TrimSpace(pr.Provider)) != provider {
			continue
		}
		if strings.TrimRight(strings.TrimSpace(pr.BaseUrl), "/") != baseURL {
			continue
		}
		if strings.Trim(strings.TrimSpace(pr.Repo), "/") != repo {
			continue
		}
		if strings.TrimSpace(pr.HeadBranch) != headBranch {
			continue
		}
		if baseBranch != "" && strings.TrimSpace(pr.BaseBranch) != baseBranch {
			continue
		}
		if headSHA != "" {
			prSHA := strings.TrimSpace(pr.HeadSha)
			if prSHA == "" || !forgeSHAEqual(prSHA, headSHA) {
				continue
			}
		}
		return true
	}
	return false
}

func (s *Server) operationHasMatchingOpenPullRequest(ctx context.Context, op db.Operation, provider, baseURL, repo, headBranch, baseBranch, headSHA string) bool {
	prs, err := s.q.ListPullRequestsForOperation(ctx, pgtype.Int8{Int64: op.ID, Valid: true})
	if err != nil {
		return false
	}
	return matchingOpenPR(prs, provider, baseURL, repo, headBranch, baseBranch, headSHA)
}

func (s *Server) maybeQueueDiscoverPullRequest(ctx context.Context, op db.Operation) {
	if loopMetadataBool(op.Metadata, "shipped") {
		return
	}
	if !loopMetadataBool(op.Metadata, "forge_ship_failed") {
		return
	}
	if !s.operationWantsForgeShip(ctx, op) {
		return
	}
	head := s.operationAutoCommitBranch(ctx, op)
	if head == "" {
		head = loopMetadataString(op.Metadata, "last_commit_branch")
	}
	if head == "" {
		return
	}
	base := s.operationShipBaseBranch(ctx, op)
	if base == "" {
		base = defaultPullRequestBaseBranch
	}
	sha := strings.TrimSpace(loopMetadataString(op.Metadata, "last_commit_sha"))
	if sha == "" {
		return
	}
	if loopMetadataString(op.Metadata, "pr_discover_for_sha") == sha {
		return
	}
	if discoverAttemptCount(op.Metadata, sha) >= maxPRDiscoverTries {
		return
	}
	routine, hasRoutine := s.loopRoutine(ctx, op)
	rop := routineOperationConfig{}
	if hasRoutine {
		rop = routineOperationConfigFromMetadata(routine)
	} else {
		rop.CreatePullRequest = true
		rop.PullRequestBaseBranch = defaultPullRequestBaseBranch
	}
	cfg, ok, err := s.resolveOperationForge(ctx, op, rop.ForgeKey)
	if err != nil || !ok {
		return
	}
	roverID := pgtype.Int8{}
	if run, err := s.q.LatestSourceRunForOperation(ctx, db.LatestSourceRunForOperationParams{
		OperationID: op.ID, FleetID: op.FleetID,
	}); err == nil && run.RoverID.Valid {
		roverID = run.RoverID
	}
	routineID := pgtype.Int8{}
	if hasRoutine {
		routineID = pgtype.Int8{Int64: routine.ID, Valid: true}
	}
	metaMap := map[string]json.RawMessage{
		"forge_ship":  jsonRaw(true),
		"pr_discover": jsonRaw(true),
		"forge_id":    jsonRaw(cfg.ID),
		"forge_key":   jsonRaw(cfg.Key),
	}
	tries := discoverAttemptCount(op.Metadata, sha) + 1
	if tries > 1 {
		metaMap["not_before"] = jsonRaw(time.Now().UTC().Add(time.Duration(forgeSyncBackoffSec(tries-1)) * time.Second).Format(time.RFC3339))
	}
	metaPatch := map[string]any{
		"pr_discover_for_sha":     sha,
		"pr_discover_attempt_sha": sha,
		"pr_discover_attempts":    tries,
	}
	params := db.CreateForgeActionParams{
		FleetID: op.FleetID, OperationID: pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID: routineID, RoverID: roverID,
		Kind:     "discover_pull_request",
		Provider: cfg.Provider, BaseUrl: cfg.BaseURL, Repo: cfg.Repo,
		HeadBranch: head, BaseBranch: base,
		CommitSha: sha,
		Title:     op.Title,
		Metadata:  metadataBytes(metaMap),
	}
	if err := s.createForgeActionWithOpMeta(ctx, params, op, metaPatch); err != nil {
		return
	}
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system",
		Body: fmt.Sprintf("Queuing PR discover for %s → %s (link open forge PR if exactly one).", head, base),
	})
}

func (s *Server) markForgeShipSucceeded(ctx context.Context, op db.Operation, action db.ForgeAction) {
	patch := map[string]any{
		"forge_ship_blocked_operation": false,
		"forge_ship_failed":            false,
		"pending_forge_ship":           false,
		"shipped":                      true,
	}
	if action.RemoteNumber.Valid {
		patch["pull_request_number"] = action.RemoteNumber.Int32
	}
	if u := strings.TrimSpace(action.RemoteUrl); u != "" {
		patch["pull_request_url"] = u
	}
	if sha := strings.TrimSpace(action.ResultSha); sha != "" {
		patch["integrated_sha"] = sha
	} else if sha := strings.TrimSpace(action.CommitSha); sha != "" {
		patch["integrated_sha"] = sha
	}
	_ = patchOperationLoopMetadata(ctx, s.q, op, patch)
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system",
		Body: fmt.Sprintf("Shipped to %s via %s.", action.BaseBranch, action.Kind),
	})
}

func recordUFOPullRequest(ctx context.Context, q *db.Queries, op db.Operation, action db.ForgeAction, req completeForgeActionReq, createdByUFO bool, source string) (linked bool, detail string) {
	if !action.RemoteNumber.Valid && req.RemoteNumber == nil {
		return false, "no remote pull request number"
	}
	num := action.RemoteNumber
	if req.RemoteNumber != nil {
		num = pgtype.Int4{Int32: *req.RemoteNumber, Valid: true}
	}
	url := strings.TrimSpace(action.RemoteUrl)
	if u := strings.TrimSpace(req.RemoteURL); u != "" {
		url = u
	}
	title := action.Title
	if t := strings.TrimSpace(req.PRTitle); t != "" {
		title = t
	}
	status := "open"
	if st := strings.TrimSpace(req.PRStatus); st != "" {
		status = st
	}
	ci := strings.TrimSpace(req.CIStatus)
	head := strings.TrimSpace(req.HeadSHA)
	if head == "" {
		head = action.CommitSha
	}
	var mergeable pgtype.Bool
	if req.Mergeable != nil {
		mergeable = pgtype.Bool{Bool: *req.Mergeable, Valid: true}
	}
	routineID := pgtype.Int8{}
	if action.RoutineID.Valid {
		routineID = action.RoutineID
	}
	opID := pgtype.Int8{Int64: op.ID, Valid: true}
	meta := metadataBytes(map[string]json.RawMessage{"source": jsonRaw(source)})
	existing, err := q.GetPullRequestByForgeIdentity(ctx, db.GetPullRequestByForgeIdentityParams{
		FleetID: op.FleetID, Provider: action.Provider, BaseUrl: action.BaseUrl, Repo: action.Repo, Number: num,
	})
	if err == nil {
		_, err = q.RelinkPullRequestToOperation(ctx, db.RelinkPullRequestToOperationParams{
			ID: existing.ID, FleetID: op.FleetID, OperationID: opID, RoutineID: routineID,
			HeadBranch: action.HeadBranch, BaseBranch: action.BaseBranch,
			Url: url, Title: title, Status: status, HeadSha: head, Mergeable: mergeable, CiStatus: ci,
			Metadata: meta,
		})
		if err != nil {
			log.Printf("relink UFO pull request op %d: %v", op.ID, err)
			return false, "database error while linking the pull request"
		}
		return true, ""
	}
	_, err = q.CreatePullRequest(ctx, db.CreatePullRequestParams{
		FleetID: op.FleetID, OperationID: opID, RoutineID: routineID,
		Provider: action.Provider, BaseUrl: action.BaseUrl, Repo: action.Repo,
		HeadBranch: action.HeadBranch, BaseBranch: action.BaseBranch,
		Url: url, Title: title, Status: status, Number: num,
		CreatedByUfo: createdByUFO, HeadSha: head, Mergeable: mergeable, CiStatus: ci,
		Metadata: meta,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existing, gerr := q.GetPullRequestByForgeIdentity(ctx, db.GetPullRequestByForgeIdentityParams{
				FleetID: op.FleetID, Provider: action.Provider, BaseUrl: action.BaseUrl, Repo: action.Repo, Number: num,
			})
			if gerr != nil {
				return false, "database error while linking the pull request"
			}
			_, rerr := q.RelinkPullRequestToOperation(ctx, db.RelinkPullRequestToOperationParams{
				ID: existing.ID, FleetID: op.FleetID, OperationID: opID, RoutineID: routineID,
				HeadBranch: action.HeadBranch, BaseBranch: action.BaseBranch,
				Url: url, Title: title, Status: status, HeadSha: head, Mergeable: mergeable, CiStatus: ci,
				Metadata: meta,
			})
			if rerr != nil {
				log.Printf("relink UFO pull request after conflict op %d: %v", op.ID, rerr)
				return false, "database error while linking the pull request"
			}
			return true, ""
		}
		log.Printf("record UFO pull request op %d: %v", op.ID, err)
		return false, "database error while linking the pull request"
	}
	return true, ""
}

func (s *Server) applyPullRequestSync(ctx context.Context, action db.ForgeAction, req completeForgeActionReq) {
	if !action.PullRequestID.Valid && !action.RemoteNumber.Valid && req.RemoteNumber == nil {
		return
	}
	if !action.OperationID.Valid {
		return
	}
	prs, err := s.q.ListPullRequestsForOperation(ctx, pgtype.Int8{Int64: action.OperationID.Int64, Valid: true})
	if err != nil {
		return
	}
	want := int32(0)
	if req.RemoteNumber != nil {
		want = *req.RemoteNumber
	} else if action.RemoteNumber.Valid {
		want = action.RemoteNumber.Int32
	}
	for _, pr := range prs {
		if !pr.Number.Valid || (want != 0 && pr.Number.Int32 != want) {
			if want != 0 {
				continue
			}
		}
		status := pr.Status
		if s := strings.TrimSpace(req.PRStatus); s != "" {
			status = s
		}
		ci := pr.CiStatus
		if c := strings.TrimSpace(req.CIStatus); c != "" {
			ci = c
		}
		head := pr.HeadSha
		if h := strings.TrimSpace(req.HeadSHA); h != "" {
			head = h
		}
		mergeable := pr.Mergeable
		if req.Mergeable != nil {
			mergeable = pgtype.Bool{Bool: *req.Mergeable, Valid: true}
		}
		url := pr.Url
		if u := strings.TrimSpace(req.RemoteURL); u != "" {
			url = u
		}
		title := pr.Title
		if t := strings.TrimSpace(req.PRTitle); t != "" {
			title = t
		}
		_, _ = s.q.UpdatePullRequestSync(ctx, db.UpdatePullRequestSyncParams{
			ID: pr.ID, FleetID: action.FleetID, Status: status, HeadSha: head,
			Mergeable: mergeable, CiStatus: ci, Url: url, Title: title,
		})
		break
	}
}

func (s *Server) enqueueOpenPullRequest(ctx context.Context, op db.Operation, push db.ForgeAction) {
	metaMap := map[string]json.RawMessage{"forge_ship": jsonRaw(true)}
	if k := forgeMetaString(push.Metadata, "forge_key"); k != "" {
		metaMap["forge_key"] = jsonRaw(k)
	}
	if id := forgeMetaInt(push.Metadata, "forge_id"); id > 0 {
		metaMap["forge_id"] = jsonRaw(id)
	}
	if v := forgeMetaInt(push.Metadata, "ci_wait_timeout_seconds"); v > 0 {
		metaMap["ci_wait_timeout_seconds"] = jsonRaw(v)
	} else {
		metaMap["ci_wait_timeout_seconds"] = jsonRaw(defaultCIWaitTimeoutSeconds)
	}
	title := op.Title
	if title == "" {
		title = push.HeadBranch
	}
	_, err := s.q.CreateForgeAction(ctx, db.CreateForgeActionParams{
		FleetID:     push.FleetID,
		OperationID: pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID:   push.RoutineID,
		RoverID:     push.RoverID,
		Kind:        "open_pull_request",
		Provider:    push.Provider, BaseUrl: push.BaseUrl, Repo: push.Repo,
		HeadBranch: push.HeadBranch, BaseBranch: push.BaseBranch,
		CommitSha: push.CommitSha, Title: title,
		Body:     fmt.Sprintf("UFO ship for operation %s", uuidStr(op.PublicID)),
		Metadata: metadataBytes(metaMap),
	})
	if err != nil {
		log.Printf("enqueue open_pull_request op %d: %v", op.ID, err)
		s.failForgeShip(ctx, op, push, "could not queue open_pull_request")
	}
}

func (s *Server) enqueueSyncPullRequest(ctx context.Context, op db.Operation, open db.ForgeAction) {
	s.enqueueSyncPullRequestAttempt(ctx, op, open, 1)
}

func (s *Server) enqueueSyncPullRequestAttempt(ctx context.Context, op db.Operation, prev db.ForgeAction, attempts int) {
	metaMap := map[string]json.RawMessage{
		"forge_ship":    jsonRaw(true),
		"sync_attempts": jsonRaw(attempts),
	}
	if v := forgeMetaInt(prev.Metadata, "ci_wait_timeout_seconds"); v > 0 {
		metaMap["ci_wait_timeout_seconds"] = jsonRaw(v)
	} else {
		metaMap["ci_wait_timeout_seconds"] = jsonRaw(defaultCIWaitTimeoutSeconds)
	}
	started := forgeMetaString(prev.Metadata, "ci_wait_started_at")
	if started == "" {
		started = time.Now().UTC().Format(time.RFC3339)
	}
	metaMap["ci_wait_started_at"] = jsonRaw(started)
	if k := forgeMetaString(prev.Metadata, "forge_key"); k != "" {
		metaMap["forge_key"] = jsonRaw(k)
	}
	if id := forgeMetaInt(prev.Metadata, "forge_id"); id > 0 {
		metaMap["forge_id"] = jsonRaw(id)
	}
	if prev.RemoteNumber.Valid {
		metaMap["remote_number"] = jsonRaw(prev.RemoteNumber.Int32)
		metaMap["remote_url"] = jsonRaw(prev.RemoteUrl)
	}
	if attempts > 1 {
		delay := forgeSyncBackoffSec(attempts - 1)
		metaMap["not_before"] = jsonRaw(time.Now().UTC().Add(time.Duration(delay) * time.Second).Format(time.RFC3339))
	}
	_, err := s.q.CreateForgeAction(ctx, db.CreateForgeActionParams{
		FleetID:       prev.FleetID,
		OperationID:   pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID:     prev.RoutineID,
		PullRequestID: prev.PullRequestID,
		RoverID:       prev.RoverID,
		Kind:          "sync_pull_request",
		Provider:      prev.Provider, BaseUrl: prev.BaseUrl, Repo: prev.Repo,
		HeadBranch: prev.HeadBranch, BaseBranch: prev.BaseBranch,
		CommitSha: prev.CommitSha,
		Metadata:  metadataBytes(metaMap),
	})
	if err != nil {
		log.Printf("enqueue sync_pull_request op %d: %v", op.ID, err)
		s.failForgeShip(ctx, op, prev, "could not queue sync_pull_request")
	}
}

func (s *Server) enqueueMergePullRequest(ctx context.Context, op db.Operation, sync db.ForgeAction) {
	meta := metadataBytes(map[string]json.RawMessage{"forge_ship": jsonRaw(true)})
	_, err := s.q.CreateForgeAction(ctx, db.CreateForgeActionParams{
		FleetID:       sync.FleetID,
		OperationID:   pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID:     sync.RoutineID,
		PullRequestID: sync.PullRequestID,
		RoverID:       sync.RoverID,
		Kind:          "merge_pull_request",
		Provider:      sync.Provider, BaseUrl: sync.BaseUrl, Repo: sync.Repo,
		HeadBranch: sync.HeadBranch, BaseBranch: sync.BaseBranch,
		CommitSha: sync.CommitSha, Metadata: meta,
	})
	if err != nil {
		s.failForgeShip(ctx, op, sync, "could not queue merge_pull_request")
	}
}

func (s *Server) enqueueMergeHeadIntoBaseBranch(ctx context.Context, op db.Operation, push db.ForgeAction) {
	metaMap := map[string]json.RawMessage{"forge_ship": jsonRaw(true)}
	if k := forgeMetaString(push.Metadata, "forge_key"); k != "" {
		metaMap["forge_key"] = jsonRaw(k)
	}
	if id := forgeMetaInt(push.Metadata, "forge_id"); id > 0 {
		metaMap["forge_id"] = jsonRaw(id)
	}
	_, err := s.q.CreateForgeAction(ctx, db.CreateForgeActionParams{
		FleetID:     push.FleetID,
		OperationID: pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID:   push.RoutineID,
		RoverID:     push.RoverID,
		Kind:        "merge_head_into_base_branch",
		Provider:    push.Provider, BaseUrl: push.BaseUrl, Repo: push.Repo,
		HeadBranch: push.HeadBranch, BaseBranch: push.BaseBranch,
		CommitSha: push.CommitSha, Metadata: metadataBytes(metaMap),
	})
	if err != nil {
		s.failForgeShip(ctx, op, push, "could not queue merge_head_into_base_branch")
	}
}

func (s *Server) operationWantsForgeShip(ctx context.Context, op db.Operation) bool {
	if v, ok := operationMetadataBool(op.Metadata, "pull_request", "create"); ok {
		return v
	}
	if routine, ok := s.loopRoutine(ctx, op); ok {
		return routineOperationConfigFromMetadata(routine).createPullRequest()
	}
	return false
}

func (s *Server) maybeQueueForgeShip(ctx context.Context, op db.Operation, headBranch, commitSHA string) bool {
	if loopMetadataBool(op.Metadata, "shipped") {
		return false
	}
	if loopMetadataBool(op.Metadata, "pending_forge_ship") {
		return false
	}
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		if sha := loopMetadataString(op.Metadata, "last_commit_sha"); sha != "" {
			commitSHA = sha
		}
	}
	if commitSHA == "" {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: "Forge ship skipped: no commit sha on the auto-commit tip.",
		})
		return false
	}
	routine, hasRoutine := s.loopRoutine(ctx, op)
	rop := routineOperationConfig{}
	if hasRoutine {
		rop = routineOperationConfigFromMetadata(routine)
	} else {
		rop.CreatePullRequest = true
		rop.PullRequestBaseBranch = defaultPullRequestBaseBranch
		rop.ShipBaseSync = shipBaseSyncMerge
	}
	cfg, ok, err := s.resolveOperationForge(ctx, op, rop.ForgeKey)
	if err != nil || !ok {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: "No-progress threshold reached but no forge is available for this mission; not shipping.",
		})
		return false
	}
	head := strings.TrimSpace(headBranch)
	if head == "" {
		if b := operationMetadataNestedString(op.Metadata, "auto_commit", "branch"); b != "" {
			head = b
		}
	}
	if head == "" {
		if b := loopMetadataString(op.Metadata, "last_commit_branch"); b != "" {
			head = b
		}
	}
	if head == "" {
		head = s.operationAutoCommitBranch(ctx, op)
	}
	if head == "" {
		return false
	}
	base := s.operationShipBaseBranch(ctx, op)
	if base == "" {
		base = defaultPullRequestBaseBranch
	}
	if s.operationHasMatchingOpenPullRequest(ctx, op, cfg.Provider, cfg.BaseURL, cfg.Repo, head, base, commitSHA) {
		return false
	}
	createPR := true
	if v, ok := operationMetadataBool(op.Metadata, "pull_request", "create"); ok {
		createPR = v
	} else if hasRoutine {
		createPR = rop.createPullRequest()
	}
	roverID := pgtype.Int8{}
	if run, err := s.q.LatestSourceRunForOperation(ctx, db.LatestSourceRunForOperationParams{
		OperationID: op.ID, FleetID: op.FleetID,
	}); err == nil && run.RoverID.Valid {
		roverID = run.RoverID
	}
	routineID := pgtype.Int8{}
	if hasRoutine {
		routineID = pgtype.Int8{Int64: routine.ID, Valid: true}
	}
	reference := s.operationShipBaseReference(ctx, op)
	if reference == "" {
		reference = normalizeSourceBranchName(rop.ShipBaseReference)
	}
	syncMode := s.operationShipBaseSync(ctx, op)
	metaMap := s.forgeShipActionMetadata(cfg, rop, createPR, syncMode)
	metaMap["ship_head_branch"] = jsonRaw(head)
	metaMap["ship_commit_sha"] = jsonRaw(commitSHA)
	kind := "push_head_branch"
	actionHead := head
	actionBase := base
	actionSHA := commitSHA
	if reference != "" && reference != base {
		kind = "update_base_branch"
		actionHead = reference
		actionBase = base
		actionSHA = ""
		metaMap["next_push_head_branch"] = jsonRaw(true)
		metaMap["ship_base_sync"] = jsonRaw(syncMode)
	}
	err = s.createForgeActionWithOpMeta(ctx, db.CreateForgeActionParams{
		FleetID:     op.FleetID,
		OperationID: pgtype.Int8{Int64: op.ID, Valid: true},
		RoutineID:   routineID,
		RoverID:     roverID,
		Kind:        kind,
		Provider:    cfg.Provider, BaseUrl: cfg.BaseURL, Repo: cfg.Repo,
		HeadBranch: actionHead, BaseBranch: actionBase, CommitSha: actionSHA,
		Metadata: metadataBytes(metaMap),
	}, op, map[string]any{
		"pending_forge_ship": true,
		"shipped":            false,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return true
		}
		log.Printf("queue forge ship op %d: %v", op.ID, err)
		return false
	}
	if kind == "update_base_branch" {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: fmt.Sprintf("Queuing ship base sync of %s from %s (%s), then ship %s onto %s via %s (pull_request.create=%v).",
				base, reference, syncMode, head, base, cfg.Key, createPR),
		})
	} else {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: fmt.Sprintf("Queuing forge ship of %s onto %s via %s (pull_request.create=%v).", head, base, cfg.Key, createPR),
		})
	}
	return true
}

func (s *Server) forgeShipActionMetadata(cfg forgeConfig, rop routineOperationConfig, createPR bool, syncMode string) map[string]json.RawMessage {
	metaMap := map[string]json.RawMessage{
		"forge_ship": jsonRaw(true),
		"forge_id":   jsonRaw(cfg.ID),
		"forge_key":  jsonRaw(cfg.Key),
	}
	if createPR {
		metaMap["next_open_pull_request"] = jsonRaw(true)
	} else {
		metaMap["next_merge_head_into_base_branch"] = jsonRaw(true)
	}
	if labels := rop.pullRequestLabels(); len(labels) > 0 {
		metaMap["labels"] = jsonRaw(labels)
	}
	if cmds := rop.checksCommands(); len(cmds) > 0 {
		metaMap["checks_commands"] = jsonRaw(cmds)
		if rop.ChecksTimeoutSeconds > 0 {
			metaMap["checks_timeout_seconds"] = jsonRaw(rop.ChecksTimeoutSeconds)
		}
	}
	metaMap["ci_wait_timeout_seconds"] = jsonRaw(effectiveCIWaitTimeoutSeconds(rop))
	if s := normalizeShipBaseSync(syncMode); s != "" {
		metaMap["ship_base_sync"] = jsonRaw(s)
	}
	return metaMap
}

const maxShipBaseResolveTries = 5

func (s *Server) requeuePilotForShipBaseSync(ctx context.Context, op db.Operation, action db.ForgeAction) bool {
	tries := loopMetadataInt(op.Metadata, "ship_base_resolve_tries")
	if tries >= maxShipBaseResolveTries {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "system",
			Body: fmt.Sprintf("Ship base sync still conflicted after %d pilot resolve attempts.", tries),
		})
		return false
	}
	atype := ""
	if op.AssigneeType.Valid {
		atype = op.AssigneeType.String
	}
	kind := s.resolvePilotKind(ctx, s.q, atype, textValue(op.AssigneePilotKind), idValue(op.AssigneeID))
	if kind == "" {
		kind = s.fleetPickOtherPilot(ctx, s.q, op.FleetID, nil)
	}
	if kind == "" || !s.fleetHasRoverFor(ctx, s.q, op.FleetID, kind) {
		return false
	}
	tries++
	_ = patchOperationLoopMetadata(ctx, s.q, op, map[string]any{
		"ship_base_resolve_tries": tries,
		"shipped":                 false,
		"pending_forge_ship":      true,
	})
	base := strings.TrimSpace(action.BaseBranch)
	ref := strings.TrimSpace(action.HeadBranch)
	syncMode := forgeMetaString(action.Metadata, "ship_base_sync")
	if syncMode == "" {
		syncMode = shipBaseSyncMerge
	}
	var b strings.Builder
	b.WriteString("Ship base sync hit a git conflict while updating the shadow integration branch. Resolve it with product context and finish without waiting for a human.\n\n")
	fmt.Fprintf(&b, "Ship base: `%s`\nReference: `%s`\nSync: %s\nDetail: %s\n", base, ref, syncMode, strings.TrimSpace(action.Message))
	b.WriteString("\nResolve merge/rebase conflicts so the ship base tracks the reference and keeps UFO-landed work. Remove all conflict markers. Prefer keeping both human-line intent and already-landed loop work.\n")
	b.WriteString("When the tree is clean and correct, finish with `@@UFO_STATUS:done@@` so UFO retries ship base sync and continues the loop.\n")
	prompt := b.String()
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system", Body: prompt,
	})
	if err := s.dispatchRun(ctx, s.q, op, kind, prompt, runSourceShipBaseResolve); err != nil {
		if errors.Is(err, errActiveRun) {
			_ = s.setOperationStatus(ctx, s.q, op, "in_progress")
			return true
		}
		return false
	}
	_ = s.setOperationStatus(ctx, s.q, op, "in_progress")
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system",
		Body: fmt.Sprintf("Re-queued %s to resolve ship base sync (attempt %d/%d).", kind, tries, maxShipBaseResolveTries),
	})
	return true
}

func (s *Server) maybeRetryForgeShipAfterPilot(ctx context.Context, op db.Operation) bool {
	if !loopMetadataBool(op.Metadata, "pending_forge_ship") {
		return false
	}
	head := loopMetadataString(op.Metadata, "last_commit_branch")
	sha := loopMetadataString(op.Metadata, "last_commit_sha")
	patch := map[string]any{"pending_forge_ship": false}
	op.Metadata = mergeOperationLoopMetadata(op.Metadata, patch)
	if err := patchOperationLoopMetadata(ctx, s.q, op, patch); err != nil {
		return false
	}
	if head == "" || sha == "" {
		return false
	}
	return s.maybeQueueForgeShip(ctx, op, head, sha)
}

func (s *Server) createForgeActionWithOpMeta(ctx context.Context, params db.CreateForgeActionParams, op db.Operation, metaPatch map[string]any) error {
	if s.pool == nil {
		if _, err := s.q.CreateForgeAction(ctx, params); err != nil {
			return err
		}
		return patchOperationLoopMetadata(ctx, s.q, op, metaPatch)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if _, err := qtx.CreateForgeAction(ctx, params); err != nil {
		return err
	}
	if err := patchOperationLoopMetadata(ctx, qtx, op, metaPatch); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) enqueuePushBranchAfterBaseSync(ctx context.Context, op db.Operation, ensure db.ForgeAction) {
	head := forgeMetaString(ensure.Metadata, "ship_head_branch")
	sha := forgeMetaString(ensure.Metadata, "ship_commit_sha")
	if head == "" {
		head = strings.TrimSpace(ensure.HeadBranch)
	}
	if sha == "" {
		sha = strings.TrimSpace(ensure.CommitSha)
	}
	if head == "" || sha == "" {
		s.failForgeShip(ctx, op, ensure, "ship base synced but push head/sha missing")
		return
	}
	metaMap := metadataMap(ensure.Metadata)
	delete(metaMap, "next_push_head_branch")
	delete(metaMap, "ship_base_sync")
	_, err := s.q.CreateForgeAction(ctx, db.CreateForgeActionParams{
		FleetID:     ensure.FleetID,
		OperationID: ensure.OperationID,
		RoutineID:   ensure.RoutineID,
		RoverID:     ensure.RoverID,
		Kind:        "push_head_branch",
		Provider:    ensure.Provider, BaseUrl: ensure.BaseUrl, Repo: ensure.Repo,
		HeadBranch: head, BaseBranch: ensure.BaseBranch, CommitSha: sha,
		Metadata: metadataBytes(metaMap),
	})
	if err != nil {
		log.Printf("enqueue push after base sync op %d: %v", op.ID, err)
		s.failForgeShip(ctx, op, ensure, "could not queue push after ship base sync")
	}
}

func loopMetadataBool(meta []byte, key string) bool {
	raw, ok := nestedMetadataMap(meta, "loop")[key]
	if !ok {
		return false
	}
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}

type fleetForgeDTO struct {
	ID                string          `json:"id"`
	Key               string          `json:"key"`
	Name              string          `json:"name"`
	Provider          string          `json:"provider"`
	BaseURL           string          `json:"base_url"`
	Repo              string          `json:"repo"`
	DefaultBaseBranch string          `json:"default_base_branch"`
	CredentialKind    string          `json:"credential_kind"`
	Credential        json.RawMessage `json:"credential"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func toForgeDTO(row db.Forge) fleetForgeDTO {
	return fleetForgeDTO{
		ID: uuidStr(row.PublicID), Key: row.Key, Name: row.Name,
		Provider: row.Provider, BaseURL: row.BaseUrl, Repo: row.Repo,
		DefaultBaseBranch: row.DefaultBaseBranch,
		CredentialKind:    row.CredentialKind,
		Credential:        metadataJSON(row.Credential),
		Metadata:          metadataJSON(row.Metadata),
		CreatedAt:         row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

type fleetForgeReq struct {
	FleetID           string          `json:"fleet_id"`
	Key               string          `json:"key"`
	Name              string          `json:"name"`
	Provider          string          `json:"provider"`
	BaseURL           string          `json:"base_url"`
	Repo              string          `json:"repo"`
	DefaultBaseBranch string          `json:"default_base_branch"`
	CredentialKind    string          `json:"credential_kind"`
	Credential        json.RawMessage `json:"credential"`
	Metadata          json.RawMessage `json:"metadata"`
}

func normalizeForgeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for i, r := range s {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
			continue
		}
		if i > 0 && ((r >= '0' && r <= '9') || r == '_' || r == '-') {
			b.WriteRune(r)
			continue
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func defaultBaseURL(provider, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" {
		return baseURL
	}
	switch provider {
	case "github":
		return "https://api.github.com"
	case "gitlab":
		return "https://gitlab.com/api/v4"
	default:
		return ""
	}
}

func normalizeForgeCredential(kind string, raw json.RawMessage) ([]byte, string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "rover_env"
	}
	switch kind {
	case "rover_env", "github_app", "gitlab_app", "secret_ref":
	default:
		return nil, "", fmt.Errorf("unsupported credential_kind")
	}
	cred := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &cred); err != nil {
			return nil, "", fmt.Errorf("credential must be an object")
		}
	}
	switch kind {
	case "rover_env":
		name, _ := cred["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			name = defaultForgeCredentialEnv
		}
		cred = map[string]any{"name": name}
	case "github_app":
		appID := strings.TrimSpace(fmt.Sprint(cred["app_id"]))
		instID := strings.TrimSpace(fmt.Sprint(cred["installation_id"]))
		if appID == "" || appID == "<nil>" || instID == "" || instID == "<nil>" {
			return nil, "", fmt.Errorf("github_app requires app_id and installation_id")
		}
		pkEnv, _ := cred["private_key_env"].(string)
		pkEnv = strings.TrimSpace(pkEnv)
		if pkEnv == "" {
			pkEnv = defaultGitHubAppPrivateKeyEnv
		}
		cred = map[string]any{
			"app_id": appID, "installation_id": instID, "private_key_env": pkEnv,
		}
	case "gitlab_app":
		appID, _ := cred["application_id"].(string)
		appID = strings.TrimSpace(appID)
		if appID == "" {
			return nil, "", fmt.Errorf("gitlab_app requires application_id")
		}
		secretEnv, _ := cred["secret_env"].(string)
		secretEnv = strings.TrimSpace(secretEnv)
		if secretEnv == "" {
			secretEnv = defaultForgeCredentialEnv
		}
		refreshEnv, _ := cred["refresh_token_env"].(string)
		refreshEnv = strings.TrimSpace(refreshEnv)
		out := map[string]any{"application_id": appID, "secret_env": secretEnv}
		if refreshEnv != "" {
			out["refresh_token_env"] = refreshEnv
		}
		cred = out
	case "secret_ref":
		ref, _ := cred["ref"].(string)
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return nil, "", fmt.Errorf("secret_ref requires ref")
		}
		backend, _ := cred["backend"].(string)
		backend = strings.TrimSpace(backend)
		if backend == "" {
			backend = "env"
		}
		cred = map[string]any{"backend": backend, "ref": ref}
	}
	b, err := json.Marshal(cred)
	if err != nil {
		return nil, "", err
	}
	return b, kind, nil
}

func (s *Server) parseForgeBody(w http.ResponseWriter, req fleetForgeReq) (db.CreateForgeParams, bool) {
	key := normalizeForgeKey(req.Key)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	repo := strings.TrimSpace(req.Repo)
	if key == "" || (provider != "github" && provider != "gitlab") || repo == "" {
		httpError(w, http.StatusBadRequest, "key, provider (github|gitlab), and repo are required")
		return db.CreateForgeParams{}, false
	}
	baseURL := defaultBaseURL(provider, req.BaseURL)
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		httpError(w, http.StatusBadRequest, "invalid base_url")
		return db.CreateForgeParams{}, false
	}
	cred, kind, err := normalizeForgeCredential(req.CredentialKind, req.Credential)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return db.CreateForgeParams{}, false
	}
	baseBranch := strings.TrimSpace(req.DefaultBaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	meta := metadataBytes(nil)
	if len(req.Metadata) > 0 {
		var ok bool
		meta, ok = jsonObjectBytes(w, req.Metadata, "metadata")
		if !ok {
			return db.CreateForgeParams{}, false
		}
	}
	return db.CreateForgeParams{
		Key: key, Name: strings.TrimSpace(req.Name), Provider: provider,
		BaseUrl: baseURL, Repo: repo, DefaultBaseBranch: baseBranch,
		CredentialKind: kind, Credential: cred, Metadata: meta,
	}, true
}

func (s *Server) listForges(w http.ResponseWriter, r *http.Request) {
	fleetIDs, ok := s.fleetIDsFromQuery(w, r)
	if !ok {
		return
	}
	var out []fleetForgeDTO
	for _, wid := range fleetIDs {
		rows, err := s.q.ListForges(r.Context(), wid)
		if err != nil {
			serverError(w, err)
			return
		}
		for _, row := range rows {
			out = append(out, toForgeDTO(row))
		}
	}
	if out == nil {
		out = []fleetForgeDTO{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createForge(w http.ResponseWriter, r *http.Request) {
	var req fleetForgeReq
	if !readJSON(w, r, &req) {
		return
	}
	wid, ok := s.fleetIDFromBody(w, r, req.FleetID)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	params, ok := s.parseForgeBody(w, req)
	if !ok {
		return
	}
	params.FleetID = wid
	row, err := s.q.CreateForge(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "that forge key is already used in this fleet")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toForgeDTO(row))
}

func (s *Server) getForge(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	fleetIDs, ok := s.fleetIDsFromQuery(w, r)
	if !ok {
		return
	}
	for _, wid := range fleetIDs {
		row, err := s.q.GetForgeByPublicID(r.Context(), db.GetForgeByPublicIDParams{
			PublicID: pid, FleetID: wid,
		})
		if err == nil {
			writeJSON(w, http.StatusOK, toForgeDTO(row))
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			serverError(w, err)
			return
		}
	}
	httpError(w, http.StatusNotFound, "forge not found")
}

func (s *Server) updateForge(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var req fleetForgeReq
	if !readJSON(w, r, &req) {
		return
	}
	var row db.Forge
	var found bool
	if req.FleetID != "" {
		wid, ok := s.fleetIDFromBody(w, r, req.FleetID)
		if !ok {
			return
		}
		if !s.requireOwnerOrAdmin(w, r, wid) {
			return
		}
		var err error
		row, err = s.q.GetForgeByPublicID(r.Context(), db.GetForgeByPublicIDParams{
			PublicID: pid, FleetID: wid,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "forge not found")
				return
			}
			serverError(w, err)
			return
		}
		found = true
	} else {
		fleetIDs, ok := s.fleetIDsFromQuery(w, r)
		if !ok {
			return
		}
		for _, wid := range fleetIDs {
			candidate, err := s.q.GetForgeByPublicID(r.Context(), db.GetForgeByPublicIDParams{
				PublicID: pid, FleetID: wid,
			})
			if err == nil {
				if !s.requireOwnerOrAdmin(w, r, wid) {
					return
				}
				row = candidate
				found = true
				break
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				serverError(w, err)
				return
			}
		}
	}
	if !found {
		httpError(w, http.StatusNotFound, "forge not found")
		return
	}
	params, ok := s.parseForgeBody(w, req)
	if !ok {
		return
	}
	updated, err := s.q.UpdateForge(r.Context(), db.UpdateForgeParams{
		ID: row.ID, FleetID: row.FleetID,
		Key: params.Key, Name: params.Name, Provider: params.Provider,
		BaseUrl: params.BaseUrl, Repo: params.Repo, DefaultBaseBranch: params.DefaultBaseBranch,
		CredentialKind: params.CredentialKind, Credential: params.Credential, Metadata: params.Metadata,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "that forge key is already used in this fleet")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toForgeDTO(updated))
}

func (s *Server) deleteForge(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	fleetIDs, ok := s.fleetIDsFromQuery(w, r)
	if !ok {
		return
	}
	for _, wid := range fleetIDs {
		row, err := s.q.GetForgeByPublicID(r.Context(), db.GetForgeByPublicIDParams{
			PublicID: pid, FleetID: wid,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			serverError(w, err)
			return
		}
		if !s.requireOwnerOrAdmin(w, r, wid) {
			return
		}
		if _, err := s.q.DeleteForge(r.Context(), db.DeleteForgeParams{ID: row.ID, FleetID: wid}); err != nil {
			serverError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpError(w, http.StatusNotFound, "forge not found")
}
