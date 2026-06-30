package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestCaptainOrchestration(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "orchestration")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "Orchestration"})
	fleet := field(t, fb, "id")
	fq := fleet

	enroll := func(autoTags ...string) string {
		_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes", "", map[string]any{"fleet_id": fq, "name": "r"})
		_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rovers", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": autoTags})
		return field(t, eb, "token")
	}
	roverClaude := enroll("pilot:claude")
	roverCodex := enroll("pilot:codex")

	_, cb := do(t, owner, "POST", ts.URL+"/v1/crews", "", map[string]string{"fleet_id": fq, "name": "C"})
	crew := field(t, cb, "id")
	do(t, owner, "PUT", ts.URL+"/v1/crews/"+crew+"/members/pilot/claude", "", map[string]string{"role": "captain"})
	do(t, owner, "PUT", ts.URL+"/v1/crews/"+crew+"/members/pilot/codex", "", map[string]string{"role": "member"})

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions", "", map[string]string{"fleet_id": fq, "name": "M", "key": "M"})
	mission := field(t, mb, "id")
	_, ob := do(t, owner, "POST", ts.URL+"/v1/operations", "", map[string]any{
		"fleet_id": fq, "title": "t", "mission_id": mission, "assignee_type": "crew", "assignee_id": crew,
	})
	op := field(t, ob, "id")

	code, claim := do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", roverClaude, nil)
	if code != http.StatusOK {
		t.Fatalf("captain claim: %d %s", code, claim)
	}
	if !boolField(t, claim, "can_propose_sub_operations") {
		t.Fatalf("expected can_propose_sub_operations=true on captain claim, got %s", claim)
	}
	captainRun := field(t, claim, "id")

	if code, b := do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+captainRun+"/result", roverClaude, map[string]any{
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
	assertSubOperationPilots(t, subOperations, "codex", "claude")
	if n := signalCount(t, owner, ts.URL, fq); n != 0 {
		t.Fatalf("expected no human signals during orchestration, got %d", n)
	}
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/runs/"+captainRun, roverClaude, map[string]string{"state": "succeeded"})

	for i, claimant := range []struct {
		pilot string
		token string
	}{{"codex", roverCodex}, {"claude", roverClaude}} {
		code, cl := do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", claimant.token, nil)
		if code != http.StatusOK {
			t.Fatalf("claim sub-operation %d: %d %s", i, code, cl)
		}
		if got := field(t, cl, "pilot"); got != claimant.pilot {
			t.Fatalf("claim sub-operation %d pilot = %q, want %q", i, got, claimant.pilot)
		}
		if boolField(t, cl, "can_propose_sub_operations") {
			t.Fatalf("sub-operation claim should not propose sub-operations: %s", cl)
		}
		if prompt := field(t, cl, "prompt"); !strings.Contains(prompt, "Main operation: "+op) {
			t.Fatalf("sub-operation prompt missing main operation relationship: %q", prompt)
		}
		subOperationRun := field(t, cl, "id")
		if i == 0 {
			if code, b := do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+subOperationRun+"/result", claimant.token, map[string]any{
				"message": "nested plan", "sub_operations": []map[string]string{{"title": "Nested"}},
			}); code != http.StatusNoContent {
				t.Fatalf("nested sub-operation result: %d %s", code, b)
			}
		}
		do(t, &http.Client{}, "PATCH", ts.URL+"/v1/runs/"+subOperationRun, claimant.token, map[string]string{"state": "succeeded"})
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
	assertBoardHidesSubOperations(t, owner, ts.URL, fq)
	reconcile := false
	for _, r := range runs {
		if r.State == "queued" && r.Pilot == "claude" {
			reconcile = true
		}
	}
	if !reconcile {
		t.Fatalf("expected a queued captain reconcile run, got runs %+v", runs)
	}

	code, cl := do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", roverClaude, nil)
	if code != http.StatusOK {
		t.Fatalf("claim reconcile: %d %s", code, cl)
	}
	if prompt := field(t, cl, "prompt"); !strings.Contains(prompt, "Sub-operation:") || !strings.Contains(prompt, "Main operation: "+op) {
		t.Fatalf("reconcile prompt missing operation relationship: %q", prompt)
	} else if !strings.Contains(prompt, "finish with @@UFO_STATUS:done@@ so UFO closes the reviewed sub-operations") ||
		!strings.Contains(prompt, "end with @@UFO_SUB_OPERATIONS_FEEDBACK@@") ||
		!strings.Contains(prompt, "UFO will post each body to that same sub-operation and resume its pilot") ||
		!strings.Contains(prompt, "If a sub-operation report is incomplete but you can verify the answer yourself") {
		t.Fatalf("reconcile prompt missing gatekeeper instructions: %q", prompt)
	}
	reconcileRun := field(t, cl, "id")
	var firstSubOperation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(subOperations[0], &firstSubOperation); err != nil {
		t.Fatalf("decode first sub-operation: %v", err)
	}
	do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+reconcileRun+"/result", roverClaude, map[string]any{
		"message": "redo A",
		"sub_operations_feedback": []map[string]string{{
			"operation_id": firstSubOperation.ID,
			"body":         "Please tighten A.",
		}},
	})
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/runs/"+reconcileRun, roverClaude, map[string]string{"state": "succeeded"})

	orchestrating, status, runs, subOperations = operationSnapshot(t, owner, ts.URL, op, fq)
	if !orchestrating || status != "in_progress" {
		t.Fatalf("after captain feedback: orchestrating=%v status=%q, want true/in_progress", orchestrating, status)
	}
	statuses := subOperationStatuses(t, subOperations)
	if statuses[0] != "in_progress" || statuses[1] != "done" {
		t.Fatalf("sub-operation statuses after feedback = %v, want [in_progress done]", statuses)
	}
	if got := activeRunState(t, subOperations[0]); got != "queued" {
		t.Fatalf("expected redo run queued, got sub-operation active_run_state %q and main runs %+v", got, runs)
	}
	assertCommentAuthor(t, owner, ts.URL, firstSubOperation.ID, "Please tighten A.", "pilot", "claude")

	code, cl = do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", roverCodex, nil)
	if code != http.StatusOK {
		t.Fatalf("claim redo: %d %s", code, cl)
	}
	redoRun := field(t, cl, "id")
	if prompt := field(t, cl, "prompt"); !strings.Contains(prompt, "Please tighten A.") {
		t.Fatalf("redo prompt missing captain feedback: %q", prompt)
	}
	do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+redoRun+"/result", roverCodex, map[string]any{"message": "A fixed"})
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/runs/"+redoRun, roverCodex, map[string]string{"state": "succeeded"})

	code, cl = do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", roverClaude, nil)
	if code != http.StatusOK {
		t.Fatalf("claim second reconcile: %d %s", code, cl)
	}
	reconcileRun = field(t, cl, "id")
	do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+reconcileRun+"/result", roverClaude, map[string]any{"operation_status": "done"})
	do(t, &http.Client{}, "PATCH", ts.URL+"/v1/runs/"+reconcileRun, roverClaude, map[string]string{"state": "succeeded"})

	_, status, _, subOperations = operationSnapshot(t, owner, ts.URL, op, fq)
	if status != "done" {
		t.Fatalf("main operation status after captain approval = %q, want done", status)
	}
	assertSubOperationStatuses(t, subOperations, "done")

	if code, b := postOperationComment(t, owner, ts.URL, firstSubOperation.ID, "One more thing."); code != http.StatusCreated {
		t.Fatalf("comment on sub-operation = %d, want 201 (%s)", code, b)
	}
	_, status, _, subOperations = operationSnapshot(t, owner, ts.URL, op, fq)
	if status != "in_progress" {
		t.Fatalf("main operation status after sub-operation resumes = %q, want in_progress", status)
	}
	statuses = subOperationStatuses(t, subOperations)
	if statuses[0] != "in_progress" || statuses[1] != "done" {
		t.Fatalf("sub-operation statuses after manual follow-up = %v, want [in_progress done]", statuses)
	}
}

func TestPilotCanCreateTopLevelOperationFromConversation(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "top-level-create")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "TopLevelCreate"})
	fleet := field(t, fb, "id")
	fq := fleet

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes", "", map[string]string{"fleet_id": fq, "name": "r"})
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rovers", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": []string{"pilot:claude"}})
	rover := field(t, eb, "token")

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions", "", map[string]string{"fleet_id": fq, "name": "M", "key": "M"})
	mission := field(t, mb, "id")
	_, ob := do(t, owner, "POST", ts.URL+"/v1/operations", "", map[string]any{
		"fleet_id": fq, "title": "Current", "mission_id": mission, "assignee_type": "pilot", "assignee_id": "claude",
	})
	op := field(t, ob, "id")

	code, claim := do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", rover, nil)
	if code != http.StatusOK {
		t.Fatalf("claim: %d %s", code, claim)
	}
	run := field(t, claim, "id")
	if code, b := do(t, &http.Client{}, "PUT", ts.URL+"/v1/runs/"+run+"/result", rover, map[string]any{
		"message": "Created it.",
		"operations": []map[string]string{{
			"title": "Discuss repo memory",
			"body":  "Explore a file-and-git based context pack.",
		}},
	}); code != http.StatusNoContent {
		t.Fatalf("result: %d %s", code, b)
	}

	_, listBody := do(t, owner, "GET", testFleetFilteredURL(ts.URL, fq, "/operations?status=in_progress"), "", nil)
	var ops []json.RawMessage
	if err := json.Unmarshal(listBody, &ops); err != nil {
		t.Fatalf("decode in-progress operations: %v (%s)", err, listBody)
	}
	var found json.RawMessage
	for _, candidate := range ops {
		if field(t, candidate, "title") == "Discuss repo memory" {
			found = candidate
			break
		}
	}
	if len(found) == 0 {
		t.Fatalf("created top-level operation not found in progress: %s", listBody)
	}
	newOp := field(t, found, "id")
	if got := field(t, found, "main_operation_id"); got != "" {
		t.Fatalf("created operation main_operation_id = %q, want empty", got)
	}
	if got := field(t, found, "assignee_pilot_kind"); got != "claude" {
		t.Fatalf("created operation assignee_pilot_kind = %q, want claude", got)
	}
	if body := field(t, found, "body"); !strings.Contains(body, "Source operation: "+op) {
		t.Fatalf("created operation body missing source operation: %q", body)
	}
	_, _, createdRuns, _ := operationSnapshot(t, owner, ts.URL, newOp, fq)
	queued := false
	for _, r := range createdRuns {
		if r.Pilot == "claude" && r.State == "queued" {
			queued = true
		}
	}
	if !queued {
		t.Fatalf("created top-level operation runs = %+v, want queued claude run", createdRuns)
	}
	_, _, _, subOperations := operationSnapshot(t, owner, ts.URL, op, fq)
	if len(subOperations) != 0 {
		t.Fatalf("top-level creation should not create sub-operations: %s", subOperations)
	}
}

func TestManualSubOperationClaimIncludesMainContext(t *testing.T) {
	ts := newTestServer(t)
	owner := signup(t, ts, "manual-sub-context")
	_, fb := do(t, owner, "POST", ts.URL+"/v1/fleets", "", map[string]string{"name": "ManualSubContext"})
	fq := field(t, fb, "id")

	_, tb := do(t, owner, "POST", ts.URL+"/v1/enrollment-codes", "", map[string]any{"fleet_id": fq, "name": "r"})
	_, eb := do(t, &http.Client{}, "POST", ts.URL+"/v1/rovers", field(t, tb, "code"), map[string]any{"name": "r", "auto_tags": []string{"pilot:claude"}})
	rover := field(t, eb, "token")

	_, mb := do(t, owner, "POST", ts.URL+"/v1/missions", "", map[string]string{"fleet_id": fq, "name": "M", "key": "M"})
	mission := field(t, mb, "id")
	_, mainBody := do(t, owner, "POST", ts.URL+"/v1/operations", "", map[string]any{
		"fleet_id": fq, "title": "Main operation", "body": "Main operation context", "mission_id": mission,
	})
	mainOperation := field(t, mainBody, "id")
	if code, b := postOperationComment(t, owner, ts.URL, mainOperation, "Main comment"); code != http.StatusCreated {
		t.Fatalf("main comment: %d %s", code, b)
	}
	_, subBody := do(t, owner, "POST", ts.URL+"/v1/operations", "", map[string]any{
		"fleet_id": fq, "title": "Sub-operation", "body": "Sub-operation context", "mission_id": mission,
		"main_operation_id": mainOperation, "assignee_type": "pilot", "assignee_id": "claude",
	})
	subOperation := field(t, subBody, "id")
	if got := field(t, subBody, "main_operation_id"); got != mainOperation {
		t.Fatalf("sub-operation main_operation_id = %q, want %q", got, mainOperation)
	}
	if code, b := postOperationComment(t, owner, ts.URL, subOperation, "Sub-operation comment"); code != http.StatusCreated {
		t.Fatalf("sub-operation comment: %d %s", code, b)
	}

	code, claim := do(t, &http.Client{}, "POST", ts.URL+"/v1/runs/claim", rover, nil)
	if code != http.StatusOK {
		t.Fatalf("claim sub-operation: %d %s", code, claim)
	}
	if got := field(t, claim, "operation_id"); got != subOperation {
		t.Fatalf("claimed operation = %q, want %q", got, subOperation)
	}
	prompt := field(t, claim, "prompt")
	for _, want := range []string{"Sub-operation", "Sub-operation context", "Main operation", "Main operation context", "Human: Main comment", "Human: Sub-operation comment"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
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
	_, b := do(t, c, "GET", testFleetFilteredURL(base, fq, "/signals"), "", nil)
	var s []json.RawMessage
	_ = json.Unmarshal(b, &s)
	return len(s)
}

func operationSnapshot(t *testing.T, c *http.Client, base, operationID, fq string) (bool, string, []struct{ Pilot, State string }, []json.RawMessage) {
	t.Helper()
	_, b := do(t, c, "GET", base+"/v1/operations/"+operationID, "", nil)
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

func activeRunState(t *testing.T, subOperation json.RawMessage) string {
	t.Helper()
	var op struct {
		ActiveRunState string `json:"active_run_state"`
	}
	if err := json.Unmarshal(subOperation, &op); err != nil {
		t.Fatalf("decode sub-operation: %v (%s)", err, subOperation)
	}
	return op.ActiveRunState
}

func assertSubOperationStatuses(t *testing.T, subOperations []json.RawMessage, want string) {
	t.Helper()
	for i, status := range subOperationStatuses(t, subOperations) {
		if status != want {
			t.Fatalf("sub-operation %d status = %q, want %s", i, status, want)
		}
	}
}

func assertSubOperationPilots(t *testing.T, subOperations []json.RawMessage, want ...string) {
	t.Helper()
	got := map[string]bool{}
	for _, subOperation := range subOperations {
		pilot := field(t, subOperation, "assignee_pilot_kind")
		if pilot != "" {
			got[pilot] = true
		}
	}
	for _, pilot := range want {
		if !got[pilot] {
			t.Fatalf("sub-operation pilots = %v, want %s", got, pilot)
		}
	}
}

func assertCommentAuthor(t *testing.T, c *http.Client, base, operationID, body, authorType, pilotKind string) {
	t.Helper()
	_, b := do(t, c, "GET", base+"/v1/operations/"+operationID+"/comments", "", nil)
	var comments []struct {
		Body            string  `json:"body"`
		AuthorType      string  `json:"author_type"`
		AuthorPilotKind *string `json:"author_pilot_kind"`
	}
	if err := json.Unmarshal(b, &comments); err != nil {
		t.Fatalf("decode comments: %v (%s)", err, b)
	}
	for _, comment := range comments {
		if comment.Body != body {
			continue
		}
		if comment.AuthorType != authorType {
			t.Fatalf("comment %q author_type = %q, want %q", body, comment.AuthorType, authorType)
		}
		if pilotKind != "" && (comment.AuthorPilotKind == nil || *comment.AuthorPilotKind != pilotKind) {
			t.Fatalf("comment %q author_pilot_kind = %v, want %q", body, comment.AuthorPilotKind, pilotKind)
		}
		return
	}
	t.Fatalf("comment %q not found in %+v", body, comments)
}

func assertBoardHidesSubOperations(t *testing.T, c *http.Client, base, fq string) {
	t.Helper()
	_, body := do(t, c, "GET", testFleetFilteredURL(base, fq, "/operations?status=in_review"), "", nil)
	var ops []json.RawMessage
	if err := json.Unmarshal(body, &ops); err != nil {
		t.Fatalf("decode board operations: %v (%s)", err, body)
	}
	if len(ops) != 0 {
		t.Fatalf("board in_review column included sub-operations: %s", body)
	}

	_, body = do(t, c, "GET", testFleetFilteredURL(base, fq, "/operations?status=in_progress"), "", nil)
	var mainOperations []struct {
		SubOperationProgress struct {
			Total      int64    `json:"total"`
			InReview   int64    `json:"in_review"`
			PilotKinds []string `json:"pilot_kinds"`
		} `json:"sub_operation_progress"`
	}
	if err := json.Unmarshal(body, &mainOperations); err != nil {
		t.Fatalf("decode main board operations: %v (%s)", err, body)
	}
	if len(mainOperations) != 1 || mainOperations[0].SubOperationProgress.Total != 2 || mainOperations[0].SubOperationProgress.InReview != 2 {
		t.Fatalf("main board progress = %+v, want one main operation with 2 in-review sub-operations", mainOperations)
	}
	if !slices.Contains(mainOperations[0].SubOperationProgress.PilotKinds, "claude") || !slices.Contains(mainOperations[0].SubOperationProgress.PilotKinds, "codex") {
		t.Fatalf("main board sub-operation pilots = %v, want claude and codex", mainOperations[0].SubOperationProgress.PilotKinds)
	}

	_, body = do(t, c, "GET", testFleetFilteredURL(base, fq, "/operations/counts"), "", nil)
	var counts map[string]int64
	if err := json.Unmarshal(body, &counts); err != nil {
		t.Fatalf("decode board counts: %v (%s)", err, body)
	}
	if counts["in_review"] != 0 {
		t.Fatalf("board counts included in_review sub-operations: %v", counts)
	}
}
