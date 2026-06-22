package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"ufo/apps/api/internal/db"
)

var ufoEpochPDT = time.Date(2026, time.June, 6, 18, 18, 18, 0, time.FixedZone("PDT", -7*60*60))

func TestIsOwnerOrAdmin(t *testing.T) {
	cases := map[string]bool{"owner": true, "admin": true, "member": false, "": false, "viewer": false}
	for role, want := range cases {
		if got := isOwnerOrAdmin(role); got != want {
			t.Errorf("isOwnerOrAdmin(%q) = %v, want %v", role, got, want)
		}
	}
}

func TestOperationStatusForRun(t *testing.T) {
	type want struct {
		status string
		ok     bool
	}
	cases := map[string]want{
		"succeeded": {"in_review", true},
		"failed":    {"blocked", true},
		"blocked":   {"blocked", true},
		"running":   {"", false},
		"queued":    {"", false},
	}
	for state, w := range cases {
		gotStatus, gotOK := operationStatusForRun(state)
		if gotStatus != w.status || gotOK != w.ok {
			t.Errorf("operationStatusForRun(%q) = (%q,%v), want (%q,%v)", state, gotStatus, gotOK, w.status, w.ok)
		}
	}
}

func TestValidPilotKind(t *testing.T) {
	if !validPilotKind("claude") || !validPilotKind("codex") || !validPilotKind("antigravity") || !validPilotKind("opencode") || !validPilotKind("openclaw") || !validPilotKind("local_1") {
		t.Fatal("known pilot kind rejected")
	}
	if validPilotKind("") || validPilotKind("OpenCode") || validPilotKind("../claude") || validPilotKind("-pilot") || validPilotKind(strings.Repeat("a", 33)) {
		t.Fatal("invalid pilot kind accepted")
	}
}

func TestCORSRejectsDisallowedMutationOrigin(t *testing.T) {
	s := &Server{}
	called := false
	h := s.cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://api.example.test/v1/fleets", nil)
	req.Header.Set("Origin", "https://attacker.example.test")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v, want 403 before handler", rec.Code, called)
	}
}

func TestCORSAllowsSameOriginMutationByDefault(t *testing.T) {
	s := &Server{}
	called := false
	h := s.cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://api.example.test/v1/fleets", nil)
	req.Header.Set("Origin", "http://api.example.test")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("status=%d, same-origin handler was not called", rec.Code)
	}
}

func TestCORSExplicitAllowlistRejectsUnlistedLoopbackOrigin(t *testing.T) {
	s := &Server{allowedOrigins: []string{"https://ufo.example.test"}}
	called := false
	h := s.cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/v1/fleets", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v, want 403 before handler", rec.Code, called)
	}
}

func TestCORSPreflightAllowsPatchAndPut(t *testing.T) {
	s := &Server{allowedOrigins: []string{"https://app.example.test"}}
	called := false
	h := s.cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodOptions, "http://api.example.test/v1/operations/op", nil)
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if rec.Code != http.StatusNoContent || called || !strings.Contains(methods, http.MethodPatch) || !strings.Contains(methods, http.MethodPut) {
		t.Fatalf("status=%d called=%v methods=%q, want 204 without handler and PATCH/PUT allowed", rec.Code, called, methods)
	}
}

func TestReadJSONRequiresApplicationJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	var dst map[string]string

	if readJSON(rec, req, &dst) || rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("readJSON=%v status=%d, want false/415", dst, rec.Code)
	}
}

func TestParseDate(t *testing.T) {
	valid, ok := parseDate(ptr(ufoEpochPDT.Format("2006-01-02")))
	if !ok || !valid.Valid {
		t.Fatal("valid date rejected")
	}
	if _, ok := parseDate(ptr("06/13/2026")); ok {
		t.Fatal("invalid date accepted")
	}
}

func TestApplyStatusToDTOUpdatesLifecycleTimestamps(t *testing.T) {
	op := db.Operation{}
	applyStatusToDTO(&op, "blocked")
	if op.StartedAt.Valid || op.FinishedAt.Valid {
		t.Fatal("blocked status set lifecycle timestamps")
	}

	applyStatusToDTO(&op, "in_progress")
	if op.Status != "in_progress" || !op.StartedAt.Valid || op.FinishedAt.Valid {
		t.Fatal("in_progress did not set status and started_at")
	}

	applyStatusToDTO(&op, "done")
	if op.Status != "done" || !op.StartedAt.Valid || !op.FinishedAt.Valid {
		t.Fatal("done did not preserve started_at and set finished_at")
	}
}

func TestLatestUserCommentAfter(t *testing.T) {
	runStart := pgtype.Timestamptz{Time: ufoEpochPDT.UTC(), Valid: true}
	comments := []db.Comment{
		{AuthorType: "user", Body: "before", CreatedAt: pgtype.Timestamptz{Time: runStart.Time.Add(-time.Second), Valid: true}},
		{AuthorType: "pilot", Body: "after pilot", CreatedAt: pgtype.Timestamptz{Time: runStart.Time.Add(time.Second), Valid: true}},
		{AuthorType: "user", Body: "after", CreatedAt: pgtype.Timestamptz{Time: runStart.Time.Add(2 * time.Second), Valid: true}},
	}
	if got := latestUserCommentAfter(comments, runStart); got != "after" {
		t.Fatalf("latestUserCommentAfter = %q, want after", got)
	}
}

func ptr(s string) *string { return &s }
