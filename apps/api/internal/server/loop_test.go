package server

import (
	"encoding/json"
	"strings"
	"testing"

	"ufo/apps/api/internal/db"
)

func TestOperationLoopRoutinePublicID(t *testing.T) {
	meta := operationMetadataWithLoop([]byte(`{}`), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "11111111-2222-3333-4444-555555555555")
	id, ok := operationLoopRoutinePublicID(meta)
	if !ok || id != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("got %q ok=%v meta=%s", id, ok, meta)
	}
	if _, ok := operationLoopRoutinePublicID([]byte(`{}`)); ok {
		t.Fatal("empty meta should not have loop routine")
	}
}

func TestIsTerminalOperationStatus(t *testing.T) {
	if !isTerminalOperationStatus("done") || !isTerminalOperationStatus("canceled") {
		t.Fatal("done/canceled terminal")
	}
	if isTerminalOperationStatus("in_progress") || isTerminalOperationStatus("in_review") {
		t.Fatal("open statuses not terminal")
	}
}

func TestRePulseOnCloseConfigDefault(t *testing.T) {
	cfg := routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{}}`)})
	if !cfg.RePulseOnClose || !cfg.SkipIfActive {
		t.Fatalf("defaults: re_pulse=%v skip=%v", cfg.RePulseOnClose, cfg.SkipIfActive)
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"pulse":{"re_pulse_on_close":false}}}`)})
	if cfg.RePulseOnClose {
		t.Fatal("expected re_pulse_on_close false")
	}
}

func TestAutoCommitBranchConfig(t *testing.T) {
	cfg := routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{}}`)})
	if cfg.AutoCommitBranch != "" {
		t.Fatalf("default auto_commit.branch = %q", cfg.AutoCommitBranch)
	}
	if !cfg.DropWorktreeOnCommit {
		t.Fatal("default auto_commit.drop_worktree should be true")
	}
	if cfg.CreatePullRequest {
		t.Fatal("default pull_request.create should be false (opt-in ship)")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"pull_request":{"create":true}}}`)})
	if !cfg.CreatePullRequest {
		t.Fatal("pull_request.create true not honored")
	}
	if cfg.pullRequestBaseBranch() != defaultPullRequestBaseBranch {
		t.Fatalf("default base = %q", cfg.pullRequestBaseBranch())
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit":{"branch":"feature/auto"}}}`)})
	if cfg.AutoCommitBranch != "feature/auto" {
		t.Fatalf("auto_commit.branch = %q, want feature/auto", cfg.AutoCommitBranch)
	}
	if !cfg.DropWorktreeOnCommit {
		t.Fatal("drop_worktree should default true when only branch set")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit":{"branch":"feature/auto","drop_worktree":false}}}`)})
	if cfg.DropWorktreeOnCommit {
		t.Fatal("auto_commit.drop_worktree false not honored")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit":{"branch":"../evil"}}}`)})
	if cfg.AutoCommitBranch != "" {
		t.Fatalf("invalid branch should normalize empty, got %q", cfg.AutoCommitBranch)
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"ship_base":{"branch":"integration","reference":"main","sync":"merge"},"pull_request":{"create":false,"labels":["ufo"],"ci_wait_timeout_seconds":60},"forge":{"key":"primary"}}}`)})
	if cfg.CreatePullRequest {
		t.Fatal("pull_request.create false not honored")
	}
	if cfg.pullRequestBaseBranch() != "integration" {
		t.Fatalf("ship_base.branch = %q", cfg.pullRequestBaseBranch())
	}
	if cfg.ShipBaseReference != "main" {
		t.Fatalf("ship_base.reference = %q", cfg.ShipBaseReference)
	}
	if cfg.ShipBaseSync != shipBaseSyncMerge {
		t.Fatalf("ship_base.sync = %q", cfg.ShipBaseSync)
	}
	if len(cfg.PullRequestLabels) != 1 || cfg.PullRequestLabels[0] != "ufo" {
		t.Fatalf("labels = %v", cfg.PullRequestLabels)
	}
	if cfg.CIWaitTimeoutSeconds == nil || *cfg.CIWaitTimeoutSeconds != 60 {
		t.Fatalf("ci wait = %v", cfg.CIWaitTimeoutSeconds)
	}
	if cfg.ForgeKey != "primary" {
		t.Fatalf("forge.key = %q", cfg.ForgeKey)
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{}}`)})
	if cfg.ShipBaseSync != shipBaseSyncMerge {
		t.Fatalf("default ship_base.sync = %q", cfg.ShipBaseSync)
	}
}

func TestSourceActionDropWorktreeOnSuccess(t *testing.T) {
	if sourceActionDropWorktreeOnSuccess([]byte(`{}`)) {
		t.Fatal("empty metadata should not drop")
	}
	if !sourceActionDropWorktreeOnSuccess([]byte(`{"drop_worktree_on_success":true}`)) {
		t.Fatal("want true")
	}
	if sourceActionDropWorktreeOnSuccess([]byte(`{"drop_worktree_on_success":false}`)) {
		t.Fatal("want false")
	}
	if sourceActionDropWorktreeOnSuccess([]byte(`{"auto_commit":true}`)) {
		t.Fatal("auto_commit alone must not imply drop")
	}
}

func TestSourceActionHadChanges(t *testing.T) {
	if sourceActionHadChanges([]byte(`{}`)) {
		t.Fatal("empty")
	}
	if !sourceActionHadChanges([]byte(`{"had_changes":true}`)) {
		t.Fatal("want true")
	}
	if sourceActionHadChanges([]byte(`{"had_changes":false}`)) {
		t.Fatal("want false")
	}
}

func TestSourceActionWantsRePulse(t *testing.T) {
	if sourceActionWantsRePulse([]byte(`{}`)) {
		t.Fatal("empty metadata")
	}
	if !sourceActionWantsRePulse([]byte(`{"re_pulse_on_success":true}`)) {
		t.Fatal("want true")
	}
	if sourceActionWantsRePulse([]byte(`{"re_pulse_on_success":false}`)) {
		t.Fatal("want false")
	}
}

func TestAutoCommitMadeProgressPrefersTipFlags(t *testing.T) {
	action := db.SourceAction{Metadata: []byte(`{"had_changes":false,"tip_advanced":true,"new_commit":false}`)}
	if !autoCommitMadeProgress(action) {
		t.Fatal("tip_advanced true should count as progress")
	}
	action = db.SourceAction{Metadata: []byte(`{"had_changes":true,"tip_advanced":false,"new_commit":false}`)}
	if autoCommitMadeProgress(action) {
		t.Fatal("explicit tip_advanced false should win over had_changes")
	}
	action = db.SourceAction{Metadata: []byte(`{"had_changes":true}`)}
	if !autoCommitMadeProgress(action) {
		t.Fatal("legacy had_changes alone should still count")
	}
	action = db.SourceAction{Metadata: []byte(`{"new_commit":true}`)}
	if !autoCommitMadeProgress(action) {
		t.Fatal("new_commit true should count as progress")
	}
}

func TestAutoCommitPostSuccessAction(t *testing.T) {
	if got := autoCommitPostSuccessAction(true, 0, true, true); got != "repulse" {
		t.Fatalf("progress+repulse+ship = %q, want repulse", got)
	}
	if got := autoCommitPostSuccessAction(true, 0, true, false); got != "repulse" {
		t.Fatalf("progress+repulse = %q, want repulse", got)
	}
	if got := autoCommitPostSuccessAction(true, 0, false, true); got != "ship_oneshot" {
		t.Fatalf("progress+oneshot ship = %q, want ship_oneshot", got)
	}
	if got := autoCommitPostSuccessAction(false, maxLoopEmptyStreak, true, false); got != "ship_or_pause" {
		t.Fatalf("empty streak = %q, want ship_or_pause", got)
	}
	if got := autoCommitPostSuccessAction(false, 1, true, false); got != "repulse" {
		t.Fatalf("partial empty = %q, want repulse", got)
	}
	if got := autoCommitPostSuccessAction(true, 0, false, false); got != "none" {
		t.Fatalf("no repulse no ship = %q, want none", got)
	}
}

func TestWorktreeRefs(t *testing.T) {
	base, fallback := worktreeRefs("ufo-auto", "orbit")
	if base != "ufo-auto" || fallback != "orbit" {
		t.Fatalf("auto+ship = %q/%q, want ufo-auto/orbit", base, fallback)
	}
	base, fallback = worktreeRefs("", "orbit")
	if base != "orbit" || fallback != "" {
		t.Fatalf("ship only = %q/%q, want orbit/", base, fallback)
	}
	base, fallback = worktreeRefs("orbit", "orbit")
	if base != "orbit" || fallback != "" {
		t.Fatalf("same tip+base = %q/%q, want orbit/", base, fallback)
	}
	base, fallback = worktreeRefs("dev-auto", "")
	if base != "dev-auto" || fallback != "" {
		t.Fatalf("auto only = %q/%q, want dev-auto/", base, fallback)
	}
	base, fallback = worktreeRefs("", "")
	if base != "" || fallback != "" {
		t.Fatalf("empty = %q/%q, want empty (plain ops must not seed a ship base)", base, fallback)
	}
}

func TestChecksCommandSingularAlias(t *testing.T) {
	cfg := routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(
		`{"operation":{"checks":{"command":"go test ./...","timeout_seconds":60}}}`,
	)})
	if len(cfg.ChecksCommands) != 1 || cfg.ChecksCommands[0] != "go test ./..." {
		t.Fatalf("checks.command = %v", cfg.ChecksCommands)
	}
	if cfg.ChecksTimeoutSeconds != 60 {
		t.Fatalf("timeout = %d", cfg.ChecksTimeoutSeconds)
	}
}

func TestParseChecksConfigCommandsArray(t *testing.T) {
	cmds, timeout := parseChecksConfig([]byte(
		`{"commands":["go test ./internal/server/","npm test"],"timeout_seconds":120}`,
	))
	if len(cmds) != 2 || cmds[0] != "go test ./internal/server/" || cmds[1] != "npm test" {
		t.Fatalf("cmds = %v", cmds)
	}
	if timeout != 120 {
		t.Fatalf("timeout = %d", timeout)
	}
	meta := []byte(`{"checks":{"command":"make test","timeout_seconds":30}}`)
	cmds, timeout = checksFromMetadataMap(metadataMap(meta))
	if len(cmds) != 1 || cmds[0] != "make test" || timeout != 30 {
		t.Fatalf("op checks = %v timeout=%d", cmds, timeout)
	}
}

func TestAutoCommitSourceActionStampsChecksShape(t *testing.T) {
	cmds, timeout := parseChecksConfig([]byte(
		`{"commands":["go test ./..."],"timeout_seconds":90}`,
	))
	meta := metadataBytes(map[string]json.RawMessage{
		"auto_commit":              jsonRaw(true),
		"re_pulse_on_success":      jsonRaw(true),
		"drop_worktree_on_success": jsonRaw(true),
		"checks_commands":          jsonRaw(cmds),
		"checks_timeout_seconds":   jsonRaw(timeout),
	})
	gotCmds := forgeMetaStringSlice(meta, "checks_commands")
	gotTimeout := forgeMetaInt(meta, "checks_timeout_seconds")
	if len(gotCmds) != 1 || gotCmds[0] != "go test ./..." || gotTimeout != 90 {
		t.Fatalf("stamped checks = %v timeout=%d", gotCmds, gotTimeout)
	}
	if !sourceActionWantsRePulse(meta) {
		t.Fatal("auto-commit metadata must set re_pulse_on_success for failed-check requeue")
	}
}

func TestStripPilotDirectiveMarkers(t *testing.T) {
	in := "Shipped widget panel.\n\n@@UFO_STATUS:done@@\n"
	if got := stripPilotDirectiveMarkers(in); got != "Shipped widget panel." {
		t.Fatalf("got %q", got)
	}
	in = "Plan:\n- a\n@@UFO_SUB_OPERATIONS@@\n[{\"title\":\"x\"}]"
	if got := stripPilotDirectiveMarkers(in); got != "Plan:\n- a" {
		t.Fatalf("got %q", got)
	}
	if got := stripPilotDirectiveMarkers("plain report"); got != "plain report" {
		t.Fatalf("got %q", got)
	}
}

func TestMetadataStringListPreservesCase(t *testing.T) {
	raw := []byte(`{"changed_files":["apps/API/Server.go","README.md"]}`)
	files, ok := metadataStringList(raw, "changed_files")
	if !ok || len(files) != 2 {
		t.Fatalf("got %v ok=%v", files, ok)
	}
	if files[0] != "apps/API/Server.go" || files[1] != "README.md" {
		t.Fatalf("case-preserving list = %v", files)
	}
	tags, ok := metadataStringSlice(raw, "changed_files")
	if !ok || tags[0] != "apps/api/server.go" {
		t.Fatalf("metadataStringSlice should lowercase tags, got %v", tags)
	}
}

func TestExpandBranchTemplate(t *testing.T) {
	op := db.Operation{Sequence: 42}
	got := expandBranchTemplate("ufo/{{routine_key}}/{{sequence}}", "DemoLoop", op, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got == "" {
		t.Fatal("empty expand")
	}
	if !strings.Contains(got, "42") {
		t.Fatalf("expand = %q, want sequence", got)
	}
	lit := expandBranchTemplate("feature/auto", "x", op, "")
	if lit != "feature/auto" {
		t.Fatalf("literal = %q", lit)
	}
}
