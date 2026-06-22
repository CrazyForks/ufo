package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCaptainOrchestration(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "orchestration")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Orchestration"})
	fleet := field(t, fb, "id")
	fq := "?fleet=" + fleet

	enroll := func(autoTags ...string) string {
		_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes"+fq, "", map[string]any{"name": "r"})
		_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/enroll", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": autoTags})
		return field(t, eb, "token")
	}
	roverClaude := enroll("pilot:claude")

	_, cb := do(t, owner, "POST", ts.URL+"/v1/crews"+fq, "", map[string]string{"name": "C"})
	crew := field(t, cb, "id")
	do(t, owner, "PUT", ts.URL+"/v1/crews/"+crew+"/members/pilot/claude"+fq, "", map[string]string{"role": "captain"})
	do(t, owner, "PUT", ts.URL+"/v1/crews/"+crew+"/members/pilot/codex"+fq, "", map[string]string{"role": "member"})

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions"+fq, "", map[string]string{"name": "M", "key": "M"})
	mission := field(t, mb, "id")
	_, ob := do(t, owner, "POST", ts.URL+"/v1/operations"+fq, "", map[string]any{
		"title": "t", "mission_id": mission, "assignee_type": "crew", "assignee_id": crew,
	})
	op := field(t, ob, "id")

	code, claim := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", roverClaude, nil)
	if code != http.StatusOK {
		t.Fatalf("captain claim: %d %s", code, claim)
	}
	if !boolField(t, claim, "can_propose_sub_operations") {
		t.Fatalf("expected can_propose_sub_operations=true on captain claim, got %s", claim)
	}
	captainRun := field(t, claim, "id")

	if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/"+captainRun+"/result", roverClaude, map[string]any{
		"message": "planned", "sub_operations": []map[string]string{{"title": "A"}, {"title": "B"}},
	}); code != http.StatusNoContent {
		t.Fatalf("captain result: %d %s", code, b)
	}
	orchestrating, status, runs, subOperations := operationSnapshot(t, owner, ts.URL, op, fq)
	if !orchestrating || status != "in_progress" {
		t.Fatalf("after split: orchestrating=%v status=%q, want true/in_progress", orchestrating, status)
	}
	if len(subOperations) != 2 {
		t.Fatalf("expected 2 sub-operations, got %d", len(subOperations))
	}
	for _, subOperation := range subOperations {
		if got := field(t, subOperation, "main_operation_id"); got != op {
			t.Fatalf("sub-operation main_operation_id = %q, want %q", got, op)
		}
		if body := field(t, subOperation, "body"); !strings.Contains(body, "Main operation: "+op) {
			t.Fatalf("sub-operation body missing main operation relationship: %q", body)
		}
	}
	if n := signalCount(t, owner, ts.URL, fq); n != 0 {
		t.Fatalf("expected no human signals during orchestration, got %d", n)
	}
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+captainRun, roverClaude, map[string]string{"state": "succeeded"})

	for i := 0; i < len(subOperations); i++ {
		code, cl := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", roverClaude, nil)
		if code != http.StatusOK {
			t.Fatalf("claim sub-operation %d: %d %s", i, code, cl)
		}
		if boolField(t, cl, "can_propose_sub_operations") {
			t.Fatalf("sub-operation claim should not propose sub-operations: %s", cl)
		}
		if prompt := field(t, cl, "prompt"); !strings.Contains(prompt, "Main operation: "+op) {
			t.Fatalf("sub-operation prompt missing main operation relationship: %q", prompt)
		}
		subOperationRun := field(t, cl, "id")
		if i == 0 {
			if code, b := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/"+subOperationRun+"/result", roverClaude, map[string]any{
				"message": "nested plan", "sub_operations": []map[string]string{{"title": "Nested"}},
			}); code != http.StatusNoContent {
				t.Fatalf("nested sub-operation result: %d %s", code, b)
			}
		}
		do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+subOperationRun, roverClaude, map[string]string{"state": "succeeded"})
	}

	orchestrating, status, runs, subOperations = operationSnapshot(t, owner, ts.URL, op, fq)
	if orchestrating {
		t.Fatalf("orchestrating should clear after reconcile")
	}
	if status != "in_progress" {
		t.Fatalf("main operation status after reconvene = %q, want in_progress", status)
	}
	if len(subOperations) != 2 {
		t.Fatalf("nested sub-operation should not be created, got %d sub-operations", len(subOperations))
	}
	assertSubOperationStatuses(t, subOperations, "in_review")
	reconcile := false
	for _, r := range runs {
		if r.State == "queued" && r.Pilot == "claude" {
			reconcile = true
		}
	}
	if !reconcile {
		t.Fatalf("expected a queued captain reconcile run, got runs %+v", runs)
	}

	code, cl := do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/claim", roverClaude, nil)
	if code != http.StatusOK {
		t.Fatalf("claim reconcile: %d %s", code, cl)
	}
	if prompt := field(t, cl, "prompt"); !strings.Contains(prompt, "Sub-operation:") || !strings.Contains(prompt, "Main operation: "+op) {
		t.Fatalf("reconcile prompt missing operation relationship: %q", prompt)
	}
	reconcileRun := field(t, cl, "id")
	do(t, &http.Client{}, "POST", ts.URL+"/v1/rover/runs/"+reconcileRun+"/result", roverClaude, map[string]any{"operation_status": "done"})
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/rover/runs/"+reconcileRun, roverClaude, map[string]string{"state": "succeeded"})

	_, status, _, subOperations = operationSnapshot(t, owner, ts.URL, op, fq)
	if status != "done" {
		t.Fatalf("main operation status after captain approval = %q, want done", status)
	}
	assertSubOperationStatuses(t, subOperations, "done")
}

func boolField(t *testing.T, body []byte, key string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal %s: %v (%s)", key, err, body)
	}
	b, _ := m[key].(bool)
	return b
}

func signalCount(t *testing.T, c *http.Client, base, fq string) int {
	t.Helper()
	_, b := do(t, c, "GET", base+"/v1/signals"+fq, "", nil)
	var s []json.RawMessage
	_ = json.Unmarshal(b, &s)
	return len(s)
}

func operationSnapshot(t *testing.T, c *http.Client, base, operationID, fq string) (bool, string, []struct{ Pilot, State string }, []json.RawMessage) {
	t.Helper()
	_, b := do(t, c, "GET", base+"/v1/operations/"+operationID+fq, "", nil)
	var d struct {
		Operation struct {
			Status        string `json:"status"`
			Orchestrating bool   `json:"orchestrating"`
		} `json:"operation"`
		Runs []struct {
			Pilot string `json:"pilot"`
			State string `json:"state"`
		} `json:"runs"`
		SubOperations []json.RawMessage `json:"sub_operations"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("decode operation detail: %v (%s)", err, b)
	}
	runs := make([]struct{ Pilot, State string }, len(d.Runs))
	for i, r := range d.Runs {
		runs[i] = struct{ Pilot, State string }{r.Pilot, r.State}
	}
	return d.Operation.Orchestrating, d.Operation.Status, runs, d.SubOperations
}

func subOperationStatuses(t *testing.T, subOperations []json.RawMessage) []string {
	t.Helper()
	statuses := make([]string, 0, len(subOperations))
	for _, subOperation := range subOperations {
		var op struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(subOperation, &op); err != nil {
			t.Fatalf("decode sub-operation: %v (%s)", err, subOperation)
		}
		statuses = append(statuses, op.Status)
	}
	return statuses
}

func assertSubOperationStatuses(t *testing.T, subOperations []json.RawMessage, want string) {
	t.Helper()
	for i, status := range subOperationStatuses(t, subOperations) {
		if status != want {
			t.Fatalf("sub-operation %d status = %q, want %s", i, status, want)
		}
	}
}
