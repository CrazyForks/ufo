package server

// Integration tests for the highest-risk authorization paths. They need a real
// PostgreSQL: set UFO_HUB_TEST_DATABASE_URL (CI provides one). Without it they skip, so
// `go test ./...` stays green on a machine with no database.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ufo/apps/api/internal/auth"
	"ufo/apps/api/internal/migrate"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("UFO_HUB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set UFO_HUB_TEST_DATABASE_URL to run authz integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := migrate.Run(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	url := os.Getenv("UFO_HUB_TEST_DATABASE_URL")
	pool := newTestPool(t)
	ctx := context.Background()
	notifier := NewNotifier(url, "ufo_run_queued", "ufo_changed")
	notifier.Start(ctx)
	srv := New(pool, 2*time.Second, notifier)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// do issues a request and returns the status and raw body. Auth is via the
// client's cookie jar (UI) or a bearer token (rover); pass bearer="" for UI.
func do(t *testing.T, c *http.Client, method, url, bearer string, body any) (int, []byte) {
	t.Helper()
	code, b, err := request(c, method, url, bearer, body)
	if err != nil {
		t.Fatal(err)
	}
	return code, b
}

func request(c *http.Client, method, url, bearer string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do %s %s: %w", method, url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res.StatusCode, b, err
}

func field(t *testing.T, body []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal %s: %v (%s)", key, err, body)
	}
	s, _ := m[key].(string)
	return s
}

// signup creates a user and returns a cookie-jar client authenticated as them.
func signup(t *testing.T, ts *httptest.Server, name string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	email := fmt.Sprintf("%s+%d@example.com", name, time.Now().UnixNano())
	if code, b := do(t, c, "POST", ts.URL+"/v1/auth/signup", "", map[string]string{
		"email": email, "password": "password123", "name": name,
	}); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("signup %s: %d %s", name, code, b)
	}
	return c
}

func sessionToken(t *testing.T, c *http.Client, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == sessionCookie {
			return cookie.Value
		}
	}
	t.Fatal("session cookie missing")
	return ""
}

func joinFleet(t *testing.T, ts *httptest.Server, owner, member *http.Client, fq, role string) {
	t.Helper()
	_, mb := do(t, member, "GET", ts.URL+"/v1/me", "", nil)
	email := field(t, mb, "email")
	if code, b := do(t, owner, "POST", ts.URL+"/v1/invitations"+fq, "", map[string]string{"email": email, "role": role}); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("invite: %d %s", code, b)
	}
	_, mineB := do(t, member, "GET", ts.URL+"/v1/invitations/mine", "", nil)
	var mine []map[string]any
	if err := json.Unmarshal(mineB, &mine); err != nil || len(mine) == 0 {
		t.Fatalf("my invitations: %v %s", err, mineB)
	}
	invID, _ := mine[0]["id"].(string)
	if code, b := do(t, member, "POST", ts.URL+"/v1/invitations/"+invID+"/accept", "", nil); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("accept invite: %d %s", code, b)
	}
}

func TestSessionTokensAreHashed(t *testing.T) {
	ts := newTestServer(t)
	client := signup(t, ts, "session-hash")
	token := sessionToken(t, client, ts.URL)
	hash := auth.HashToken(token)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("UFO_HUB_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect for session check: %v", err)
	}
	defer conn.Close(ctx)

	var hashRows, rawRows int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token_hash = $1", hash).Scan(&hashRows); err != nil {
		t.Fatalf("select hashed session: %v", err)
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token_hash = $1", token).Scan(&rawRows); err != nil {
		t.Fatalf("select raw session: %v", err)
	}
	if hashRows != 1 || rawRows != 0 {
		t.Fatalf("session token storage = hash rows %d, raw rows %d", hashRows, rawRows)
	}
	if code, b := do(t, client, "GET", ts.URL+"/v1/me", "", nil); code != http.StatusOK {
		t.Fatalf("hashed session lookup failed: %d %s", code, b)
	}
}

func TestMemberMutationsReturnNotFoundForNonMember(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "owner")
	outsider := signup(t, ts, "outsider")

	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Acme"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fq := "?fleet=" + field(t, b, "id")
	_, me := do(t, outsider, "GET", ts.URL+"/v1/me", "", nil)
	outsiderID := field(t, me, "id")

	if code, b := do(t, owner, "PATCH", ts.URL+"/v1/members/"+outsiderID+fq, "", map[string]string{"role": "member"}); code != http.StatusNotFound {
		t.Fatalf("patch non-member = %d %s, want 404", code, b)
	}
	if code, b := do(t, owner, "DELETE", ts.URL+"/v1/members/"+outsiderID+fq, "", nil); code != http.StatusNotFound {
		t.Fatalf("delete non-member = %d %s, want 404", code, b)
	}
}

type httpResult struct {
	status int
	err    error
}

func concurrentResults(n int, request func(int) httpResult) []httpResult {
	var wg sync.WaitGroup
	results := make([]httpResult, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = request(i)
		}()
	}
	wg.Wait()
	return results
}

func statuses(t *testing.T, results []httpResult) []int {
	t.Helper()
	out := make([]int, len(results))
	for i, result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		out[i] = result.status
	}
	return out
}

func waitForHook(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s hook was not reached", name)
	}
}

func assertStillInFlight(t *testing.T, result <-chan httpResult, name string) {
	t.Helper()
	select {
	case r := <-result:
		t.Fatalf("%s returned before the overlapping request was released: status=%d err=%v", name, r.status, r.err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestConcurrentInvariants(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "concurrency-owner")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Concurrency"})
	fleet := field(t, fb, "id")
	fq := "?fleet=" + fleet

	t.Run("one-time enrollment code", func(t *testing.T) {
		_, b := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "one"})
		enrollmentCode := field(t, b, "code")
		locked := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		serverTestHooks.Store(testHooks{afterEnrollmentCodeLocked: func() {
			once.Do(func() {
				close(locked)
				<-release
			})
		}})
		t.Cleanup(func() { serverTestHooks.Store(testHooks{}) })

		result := make(chan httpResult, 2)
		go func() {
			code, _, err := request(&http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r"})
			result <- httpResult{code, err}
		}()
		waitForHook(t, locked, "enrollment")
		go func() {
			code, _, err := request(&http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r"})
			result <- httpResult{code, err}
		}()
		assertStillInFlight(t, result, "second enrollment")
		close(release)
		statuses := statuses(t, []httpResult{<-result, <-result})
		if !((statuses[0] == http.StatusCreated && statuses[1] == http.StatusUnauthorized) ||
			(statuses[1] == http.StatusCreated && statuses[0] == http.StatusUnauthorized)) {
			t.Fatalf("concurrent enrollment statuses = %v, want one 201 and one 401", statuses)
		}
	})

	t.Run("multi-use enrollment code", func(t *testing.T) {
		code, b := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "pair", "uses": 2})
		if code != http.StatusCreated {
			t.Fatalf("create multi-use code: %d %s", code, b)
		}
		enrollmentCode := field(t, b, "code")
		codeID := field(t, b, "id")
		if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r1"}); code != http.StatusCreated {
			t.Fatalf("first enroll: %d %s", code, b)
		}
		var codes []map[string]any
		if code, b := do(t, owner, "GET", ts.URL+"/v1/enrollment-codes"+fq, "", nil); code != http.StatusOK {
			t.Fatalf("list codes: %d %s", code, b)
		} else if err := json.Unmarshal(b, &codes); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range codes {
			if item["id"] == codeID {
				found = true
				if item["remaining_uses"] != float64(1) {
					t.Fatalf("remaining_uses after first enroll = %v, want 1", item["remaining_uses"])
				}
			}
		}
		if !found {
			t.Fatal("multi-use code disappeared after first use")
		}
		if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r2"}); code != http.StatusCreated {
			t.Fatalf("second enroll: %d %s", code, b)
		}
		if code, _ := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r3"}); code != http.StatusUnauthorized {
			t.Fatalf("third enroll = %d, want 401", code)
		}
	})

	t.Run("one active run", func(t *testing.T) {
		// A claude-capable rover so dispatch succeeds (else the operation blocks).
		_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
		_, _ = do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": []string{"pilot:claude"}})
		_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "CONC"})
		mission := field(t, mb, "id")
		_, ob := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{"title": "t", "mission_id": mission})
		operation := field(t, ob, "id")
		results := concurrentResults(2, func(_ int) httpResult {
			code, _, err := request(owner, "PATCH", ts.URL+"/v1/operations/"+operation+fq, "", map[string]any{"assignee_type": "pilot", "assignee_id": "claude"})
			return httpResult{code, err}
		})
		statuses := statuses(t, results)
		if !((statuses[0] == http.StatusOK && statuses[1] == http.StatusConflict) ||
			(statuses[1] == http.StatusOK && statuses[0] == http.StatusConflict)) {
			t.Fatalf("concurrent dispatch statuses = %v, want one 200 and one 409", statuses)
		}
	})

	t.Run("at least one owner", func(t *testing.T) {
		second := signup(t, ts, "concurrency-second")
		_, secondMe := do(t, second, "GET", ts.URL+"/v1/me", "", nil)
		secondID := field(t, secondMe, "id")
		secondEmail := field(t, secondMe, "email")
		_, ownerMe := do(t, owner, "GET", ts.URL+"/v1/me", "", nil)
		ownerID := field(t, ownerMe, "id")
		_, ib := do(t, owner, "POST", ts.URL+"/v1/invitations"+fq, "", map[string]string{"email": secondEmail, "role": "member"})
		do(t, second, "POST", ts.URL+"/v1/invitations/"+field(t, ib, "id")+"/accept", "", nil)
		if code, b := do(t, owner, "PATCH", ts.URL+"/v1/members/"+secondID+fq, "", map[string]string{"role": "owner"}); code != http.StatusNoContent {
			t.Fatalf("promote second owner: %d %s", code, b)
		}

		locked := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		serverTestHooks.Store(testHooks{afterRoleFleetLocked: func() {
			once.Do(func() {
				close(locked)
				<-release
			})
		}})
		t.Cleanup(func() { serverTestHooks.Store(testHooks{}) })

		result := make(chan httpResult, 2)
		go func() {
			code, _, err := request(owner, "PATCH", ts.URL+"/v1/members/"+secondID+fq, "", map[string]string{"role": "member"})
			result <- httpResult{code, err}
		}()
		waitForHook(t, locked, "role")
		go func() {
			code, _, err := request(second, "PATCH", ts.URL+"/v1/members/"+ownerID+fq, "", map[string]string{"role": "member"})
			result <- httpResult{code, err}
		}()
		assertStillInFlight(t, result, "second owner demotion")
		close(release)
		statuses := statuses(t, []httpResult{<-result, <-result})
		if !((statuses[0] == http.StatusNoContent && statuses[1] == http.StatusForbidden) ||
			(statuses[1] == http.StatusNoContent && statuses[0] == http.StatusForbidden)) {
			t.Fatalf("concurrent owner demotion statuses = %v, want one 204 and one 403", statuses)
		}
	})
}

// TestOwnerOrAdminGatingAndTokenMasking covers findings #3: only owners/admins may
// manage rover credentials, and listings never expose full enrollment codes.
func TestOwnerOrAdminGatingAndTokenMasking(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "owner")

	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Acme"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	// A second user joins as a plain member via invite → accept.
	member := signup(t, ts, "member")
	var meEmail string
	if _, mb := do(t, member, "GET", ts.URL+"/v1/me", "", nil); true {
		meEmail = field(t, mb, "email")
	}
	if code, b := do(t, owner, "POST", ts.URL+"/v1/invitations"+fq, "", map[string]string{"email": meEmail, "role": "member"}); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("invite: %d %s", code, b)
	}
	_, mineB := do(t, member, "GET", ts.URL+"/v1/invitations/mine", "", nil)
	var mine []map[string]any
	if err := json.Unmarshal(mineB, &mine); err != nil || len(mine) == 0 {
		t.Fatalf("my invitations: %v %s", err, mineB)
	}
	invID, _ := mine[0]["id"].(string)
	if code, b := do(t, member, "POST", ts.URL+"/v1/invitations/"+invID+"/accept", "", nil); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("accept invite: %d %s", code, b)
	}

	// Owner can create an enrollment code (and sees the full value once).
	code, b = do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "rover"})
	if code != http.StatusCreated {
		t.Fatalf("owner create enrollment code: %d %s", code, b)
	}
	fullCode := field(t, b, "code")
	codeID := field(t, b, "id")
	if len(fullCode) < 10 {
		t.Fatalf("expected a full code at creation, got %q", fullCode)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("UFO_HUB_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect for secret check: %v", err)
	}
	defer conn.Close(ctx)
	var storedCodeHash string
	if err := conn.QueryRow(ctx, "SELECT code_hash FROM enrollment_codes WHERE public_id = $1", codeID).Scan(&storedCodeHash); err != nil {
		t.Fatalf("select code hash: %v", err)
	}
	if storedCodeHash != auth.HashToken(fullCode) || storedCodeHash == fullCode {
		t.Fatalf("enrollment code stored unsafely: got %q", storedCodeHash)
	}

	// Member is forbidden from listing/creating/deleting codes and deleting rovers.
	for _, tc := range []struct {
		method, path string
	}{
		{"GET", "/v1/enrollment-codes" + fq},
		{"POST", "/v1/enrollment-codes" + fq},
		{"DELETE", "/v1/enrollment-codes/" + codeID + fq},
	} {
		if code, b := do(t, member, tc.method, ts.URL+tc.path, "", map[string]any{"name": "x"}); code != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403 (%s)", tc.method, tc.path, code, b)
		}
	}

	// Owner listing must mask the code (no full secret on the wire).
	_, lb := do(t, owner, "GET", ts.URL+"/v1/enrollment-codes"+fq, "", nil)
	if strings.Contains(string(lb), fullCode) {
		t.Errorf("enrollment code listing leaked the full code: %s", lb)
	}
	if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", fullCode, map[string]any{"name": "r"}); code != http.StatusCreated {
		t.Fatalf("enroll: %d %s", code, b)
	} else {
		connectionToken := field(t, b, "token")
		roverID := field(t, b, "id")
		var storedTokenHash string
		if err := conn.QueryRow(ctx, "SELECT token_hash FROM rovers WHERE public_id = $1", roverID).Scan(&storedTokenHash); err != nil {
			t.Fatalf("select token hash: %v", err)
		}
		if storedTokenHash != auth.HashToken(connectionToken) || storedTokenHash == connectionToken {
			t.Fatalf("rover token stored unsafely: got %q", storedTokenHash)
		}
	}
}

// TestRoverRunOwnership covers finding #2: a rover may not mutate a run it did
// not claim.
func TestRoverRunOwnership(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "owner")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Operations"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	// Two rovers enrolled via two one-time codes. Only rover A advertises the
	// required pilot capability, so rover B must not steal and block the run.
	enroll := func(autoTags ...string) string {
		_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
		enrollmentCode := field(t, tb, "code")
		_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r", "auto_tags": autoTags})
		return field(t, eb, "token") // connection token
	}
	roverA := enroll("os:macos", "arch:aarch64", "pilot:claude")
	roverB := enroll("os:linux", "arch:x86_64", "pilot:codex")

	// A mission + an operation assigned to the claude pilot → auto-queues a run
	// (rover A advertises pilot:claude, so the claude pilot has a rover to drive).
	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")
	code, ob := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{
		"title": "t", "body": "echo hi", "mission_id": mission, "assignee_type": "pilot", "assignee_id": "claude",
	})
	if code != http.StatusCreated {
		t.Fatalf("create operation: %d %s", code, ob)
	}
	operationID := field(t, ob, "id")

	assertRunState := func(wantState string, wantQueued, wantWorking int64) {
		_, detailBody := do(t, owner, "GET", ts.URL+"/v1/operations/"+operationID+fq, "", nil)
		var detail struct {
			Operation struct {
				ActiveRunState string `json:"active_run_state"`
			} `json:"operation"`
		}
		if err := json.Unmarshal(detailBody, &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Operation.ActiveRunState != wantState {
			t.Fatalf("active_run_state = %q, want %q", detail.Operation.ActiveRunState, wantState)
		}
		_, countsBody := do(t, owner, "GET", ts.URL+"/v1/operations/working"+fq, "", nil)
		var counts struct {
			Queued  int64 `json:"queued"`
			Working int64 `json:"working"`
		}
		if err := json.Unmarshal(countsBody, &counts); err != nil {
			t.Fatal(err)
		}
		if counts.Queued != wantQueued || counts.Working != wantWorking {
			t.Fatalf("working counts = queued:%d working:%d, want queued:%d working:%d", counts.Queued, counts.Working, wantQueued, wantWorking)
		}
	}
	assertRunState("queued", 1, 0)

	// Rover B is otherwise tag-compatible, but it cannot claim a Claude run.
	if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", roverB, nil); code != http.StatusNoContent {
		t.Fatalf("rover B claim = %d, want 204 (%s)", code, b)
	}

	// Rover A claims the run.
	code, cb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", roverA, nil)
	if code != http.StatusOK {
		t.Fatalf("rover A claim: %d %s", code, cb)
	}
	runID := field(t, cb, "id")
	if runID == "" {
		t.Fatalf("no run claimed: %s", cb)
	}

	// Rover B (did not claim) must not be able to change the run's state.
	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+runID, roverB, map[string]string{"state": "running"}); code != http.StatusNotFound {
		t.Errorf("rover B set-state = %d, want 404 (%s)", code, b)
	}
	// Rover A (the owner of the run) can.
	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+runID, roverA, map[string]string{"state": "running"}); code != http.StatusOK && code != http.StatusNoContent {
		t.Errorf("rover A set-state = %d, want ok (%s)", code, b)
	}
	assertRunState("running", 0, 1)

}

func TestRoverListReportsBusyUnits(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "rover-usage")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Rover usage"})
	fq := "?fleet=" + field(t, fb, "id")

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": []string{"pilot:claude"}})
	rover := field(t, eb, "token")
	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/me", rover, map[string]any{"auto_tags": []string{"pilot:claude"}, "units": 2}); code != http.StatusOK {
		t.Fatalf("set rover units: %d %s", code, b)
	}

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "USE"})
	mission := field(t, mb, "id")
	for i := 0; i < 2; i++ {
		do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{"title": fmt.Sprintf("t%d", i), "mission_id": mission, "assignee_type": "pilot", "assignee_id": "claude"})
		if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", rover, nil); code != http.StatusOK {
			t.Fatalf("claim %d: %d %s", i, code, b)
		}
	}

	_, rb := do(t, owner, "GET", ts.URL+"/v1/rovers"+fq, "", nil)
	var rovers []struct {
		Status    string `json:"status"`
		Units     int    `json:"units"`
		BusyUnits int    `json:"busy_units"`
	}
	if err := json.Unmarshal(rb, &rovers); err != nil || len(rovers) != 1 {
		t.Fatalf("list rovers: %v %s", err, rb)
	}
	if rovers[0].Status != "busy" || rovers[0].Units != 2 || rovers[0].BusyUnits != 2 {
		t.Fatalf("rover usage = %+v, want busy 2/2", rovers[0])
	}
}

func TestCancelRunStopsHeartbeat(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "cancel-run")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Cancel"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": []string{"pilot:claude"}})
	rover := field(t, eb, "token")

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")
	if code, b := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{
		"title": "t", "mission_id": mission, "assignee_type": "pilot", "assignee_id": "claude",
	}); code != http.StatusCreated {
		t.Fatalf("create operation: %d %s", code, b)
	}

	code, cb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", rover, nil)
	if code != http.StatusOK {
		t.Fatalf("claim: %d %s", code, cb)
	}
	runID := field(t, cb, "id")

	if code, b := do(t, owner, "POST", ts.URL+"/v1/runs/"+runID+"/cancel"+fq, "", nil); code != http.StatusOK {
		t.Fatalf("cancel: %d %s", code, b)
	}
	if code, b := do(t, &http.Client{}, "PUT", ts.URL+"/v1/rover/runs/"+runID+"/heartbeat", rover, nil); code != http.StatusNotFound {
		t.Fatalf("heartbeat after cancel = %d, want 404 (%s)", code, b)
	}
	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+runID, rover, map[string]string{"state": "succeeded"}); code != http.StatusNotFound {
		t.Fatalf("state after cancel = %d, want 404 (%s)", code, b)
	}

	_, detail := do(t, owner, "GET", ts.URL+"/v1/operations/"+field(t, cb, "operation_id")+fq, "", nil)
	var got struct {
		Operation struct {
			Status string `json:"status"`
		} `json:"operation"`
		Runs []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(detail, &got); err != nil {
		t.Fatalf("decode detail: %v (%s)", err, detail)
	}
	if got.Operation.Status != "in_review" {
		t.Fatalf("operation status after cancel = %q, want in_review", got.Operation.Status)
	}
	for _, run := range got.Runs {
		if run.ID == runID && run.State != "canceled" {
			t.Fatalf("run state after cancel = %q, want canceled", run.State)
		}
	}
}

func TestRevokedRoverConnectionTokenIsRejected(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "revoke-rover")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Rovers"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
	enrollmentCode := field(t, tb, "code")
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r"})
	connectionToken := field(t, eb, "token")

	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/me", connectionToken, map[string]any{"auto_tags": []string{"pilot:claude"}}); code != http.StatusOK {
		t.Fatalf("connection token before revoke = %d, want 200 (%s)", code, b)
	}

	_, rb := do(t, owner, "GET", ts.URL+"/v1/rovers"+fq, "", nil)
	var rovers []map[string]any
	if err := json.Unmarshal(rb, &rovers); err != nil || len(rovers) != 1 {
		t.Fatalf("list rovers: %v %s", err, rb)
	}
	roverID, _ := rovers[0]["id"].(string)
	if roverID == "" {
		t.Fatalf("listed rover has no id: %s", rb)
	}
	if code, b := do(t, owner, "DELETE", ts.URL+"/v1/rovers/"+roverID+fq, "", nil); code != http.StatusNoContent {
		t.Fatalf("delete rover: %d %s", code, b)
	}

	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/me", connectionToken, map[string]any{"auto_tags": []string{"pilot:claude"}}); code != http.StatusUnauthorized {
		t.Fatalf("connection token after revoke = %d, want 401 (%s)", code, b)
	}
}

func TestRoverNameCanBeChangedFromUIAndLocalRefresh(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "rename-rover")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Rovers"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
	enrollmentCode := field(t, tb, "code")
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "old"})
	connectionToken := field(t, eb, "token")

	_, rb := do(t, owner, "GET", ts.URL+"/v1/rovers"+fq, "", nil)
	var rovers []map[string]any
	if err := json.Unmarshal(rb, &rovers); err != nil || len(rovers) != 1 {
		t.Fatalf("list rovers: %v %s", err, rb)
	}
	roverID, _ := rovers[0]["id"].(string)
	if code, b := do(t, owner, "PATCH", ts.URL+"/v1/rovers/"+roverID+fq, "", map[string]string{"name": "ui-name"}); code != http.StatusNoContent {
		t.Fatalf("rename rover: %d %s", code, b)
	}
	_, rb = do(t, owner, "GET", ts.URL+"/v1/rovers"+fq, "", nil)
	if err := json.Unmarshal(rb, &rovers); err != nil || rovers[0]["name"] != "ui-name" {
		t.Fatalf("name after UI rename: %v %s", err, rb)
	}

	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/me", connectionToken, map[string]any{"name": "local-name", "auto_tags": []string{"pilot:claude"}}); code != http.StatusOK || field(t, b, "name") != "ui-name" {
		t.Fatalf("local refresh: %d %s", code, b)
	}
	_, rb = do(t, owner, "GET", ts.URL+"/v1/rovers"+fq, "", nil)
	if err := json.Unmarshal(rb, &rovers); err != nil || rovers[0]["name"] != "ui-name" {
		t.Fatalf("name after local refresh: %v %s", err, rb)
	}
}

func TestCrewRename(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "rename-crew")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Crews"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fq := "?fleet=" + field(t, b, "id")
	code, b = do(t, owner, "POST", ts.URL+"/v1/crews"+fq, "", map[string]string{"name": "Alpha"})
	if code != http.StatusCreated {
		t.Fatalf("create crew: %d %s", code, b)
	}
	crewID := field(t, b, "id")
	if code, b := do(t, owner, "PATCH", ts.URL+"/v1/crews/"+crewID+fq, "", map[string]string{"name": " Beta "}); code != http.StatusNoContent {
		t.Fatalf("rename crew: %d %s", code, b)
	}
	_, b = do(t, owner, "GET", ts.URL+"/v1/crews"+fq, "", nil)
	var crews []map[string]any
	if err := json.Unmarshal(b, &crews); err != nil || crews[0]["name"] != "Beta" {
		t.Fatalf("name after rename: %v %s", err, b)
	}
	if code, b := do(t, owner, "PATCH", ts.URL+"/v1/crews/"+crewID+fq, "", map[string]string{"name": " "}); code != http.StatusBadRequest {
		t.Fatalf("empty rename: %d %s", code, b)
	}
}

func TestCrewAdministrationRequiresOwnerOrAdminAndValidRole(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "crew-owner")
	member := signup(t, ts, "crew-member")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Crews"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fq := "?fleet=" + field(t, b, "id")
	joinFleet(t, ts, owner, member, fq, "member")

	code, b = do(t, owner, "POST", ts.URL+"/v1/crews"+fq, "", map[string]string{"name": "Alpha"})
	if code != http.StatusCreated {
		t.Fatalf("create crew: %d %s", code, b)
	}
	crewID := field(t, b, "id")
	if code, b := do(t, owner, "PUT", ts.URL+"/v1/crews/"+crewID+"/members/pilot/claude"+fq, "", map[string]string{"role": "boss"}); code != http.StatusBadRequest {
		t.Fatalf("invalid crew role = %d, want 400 (%s)", code, b)
	}
	if code, b := do(t, owner, "PUT", ts.URL+"/v1/crews/"+crewID+"/members/pilot/claude"+fq, "", map[string]string{"role": "captain"}); code != http.StatusNoContent {
		t.Fatalf("owner add captain: %d %s", code, b)
	}

	for name, req := range map[string]struct {
		method string
		path   string
		body   any
	}{
		"create": {"POST", "/v1/crews" + fq, map[string]string{"name": "Evil"}},
		"rename": {"PATCH", "/v1/crews/" + crewID + fq, map[string]string{"name": "Evil"}},
		"add":    {"PUT", "/v1/crews/" + crewID + "/members/pilot/codex" + fq, map[string]string{"role": "member"}},
		"remove": {"DELETE", "/v1/crews/" + crewID + "/members/pilot/claude" + fq, nil},
		"delete": {"DELETE", "/v1/crews/" + crewID + fq, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if code, b := do(t, member, req.method, ts.URL+req.path, "", req.body); code != http.StatusForbidden {
				t.Fatalf("plain member %s crew = %d, want 403 (%s)", name, code, b)
			}
		})
	}
}

func TestRoverTagRefreshNotifiesFleet(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "rover-notify")
	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Rovers"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fq := "?fleet=" + field(t, b, "id")

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
	enrollmentCode := field(t, tb, "code")
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", enrollmentCode, map[string]any{"name": "r"})
	connectionToken := field(t, eb, "token")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, os.Getenv("UFO_HUB_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("listen connect: %v", err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "listen ufo_changed"); err != nil {
		t.Fatalf("listen: %v", err)
	}

	if code, b := do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/me", connectionToken, map[string]any{"auto_tags": []string{"pilot:claude"}}); code != http.StatusOK {
		t.Fatalf("refresh tags: %d %s", code, b)
	}
	n, err := conn.WaitForNotification(ctx)
	if err != nil {
		t.Fatalf("wait notification: %v", err)
	}
	var payload struct {
		Type string `json:"t"`
	}
	if err := json.Unmarshal([]byte(n.Payload), &payload); err != nil {
		t.Fatalf("decode notification payload %q: %v", n.Payload, err)
	}
	if payload.Type != "rover" {
		t.Fatalf("notification payload = %s, want rover event", n.Payload)
	}
}

// TestTenantIsolation covers the fleet-scoped user lookup: a user outside a fleet
// can't be assigned operations or added to its crews even if their id is known.
func TestTenantIsolation(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "owner")
	outsider := signup(t, ts, "outsider")

	// outsider's public id (they are NOT a member of the owner's group fleet).
	_, ob := do(t, outsider, "GET", ts.URL+"/v1/me", "", nil)
	outsiderID := field(t, ob, "id")

	code, b := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Acme"})
	if code != http.StatusCreated {
		t.Fatalf("create fleet: %d %s", code, b)
	}
	fleet := field(t, b, "id")
	fq := "?fleet=" + fleet

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")

	// Assigning an operation to a non-member must be rejected.
	if code, b := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{
		"title": "t", "body": "", "mission_id": mission, "assignee_type": "user", "assignee_id": outsiderID,
	}); code != http.StatusBadRequest {
		t.Errorf("assign operation to outsider = %d, want 400 (%s)", code, b)
	}

	// Adding a non-member to a crew must be rejected.
	_, cb := do(t, owner, "POST", ts.URL+"/v1/crews"+fq, "", map[string]string{"name": "C"})
	crew := field(t, cb, "id")
	if code, b := do(t, owner, "PUT", ts.URL+"/v1/crews/"+crew+"/members/user/"+outsiderID+fq, "", map[string]string{}); code != http.StatusBadRequest {
		t.Errorf("add outsider to crew = %d, want 400 (%s)", code, b)
	}
}

func TestCreateOperationValidation(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "validation")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Validation"})
	fleet := field(t, fb, "id")
	fq := "?fleet=" + fleet
	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")

	for name, body := range map[string]map[string]any{
		"priority":      {"title": "t", "mission_id": mission, "priority": 5},
		"date":          {"title": "t", "mission_id": mission, "start_date": "tomorrow"},
		"assignee_type": {"title": "t", "mission_id": mission, "assignee_type": "pilot"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, b := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", body); code != http.StatusBadRequest {
				t.Fatalf("create invalid operation = %d, want 400 (%s)", code, b)
			}
		})
	}
}

func TestCreateOperationCanSkipImmediateStart(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "startskip")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "StartSkip"})
	fleet := field(t, fb, "id")
	fq := "?fleet=" + fleet
	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")

	code, b := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{
		"title": "t", "mission_id": mission, "assignee_type": "pilot", "assignee_id": "claude", "start_immediately": false,
	})
	if code != http.StatusCreated {
		t.Fatalf("create operation = %d, want 201 (%s)", code, b)
	}
	if status := field(t, b, "status"); status != "todo" {
		t.Fatalf("status = %q, want todo", status)
	}

	op := field(t, b, "id")
	_, detail := do(t, owner, "GET", ts.URL+"/v1/operations/"+op+fq, "", nil)
	var d struct {
		Runs []any `json:"runs"`
	}
	if err := json.Unmarshal(detail, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Runs) != 0 {
		t.Fatalf("runs = %d, want 0", len(d.Runs))
	}
}
