package server

import (
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
		t.Fatalf("defaults: %+v", cfg)
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"re_pulse_on_close":false}}`)})
	if cfg.RePulseOnClose {
		t.Fatal("explicit false")
	}
}

func TestAutoCommitBranchConfig(t *testing.T) {
	cfg := routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{}}`)})
	if cfg.AutoCommitBranch != "" {
		t.Fatalf("default auto_commit_branch = %q", cfg.AutoCommitBranch)
	}
	if !cfg.DropWorktreeOnCommit {
		t.Fatal("default drop_worktree_on_commit should be true")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit_branch":"dev-auto"}}`)})
	if cfg.AutoCommitBranch != "dev-auto" {
		t.Fatalf("auto_commit_branch = %q, want dev-auto", cfg.AutoCommitBranch)
	}
	if !cfg.DropWorktreeOnCommit {
		t.Fatal("drop_worktree_on_commit should default true when only branch set")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit_branch":"dev-auto","drop_worktree_on_commit":false}}`)})
	if cfg.DropWorktreeOnCommit {
		t.Fatal("drop_worktree_on_commit false not honored")
	}
	cfg = routineOperationConfigFromMetadata(db.Routine{Metadata: []byte(`{"operation":{"auto_commit_branch":"../evil"}}`)})
	if cfg.AutoCommitBranch != "" {
		t.Fatalf("invalid branch should normalize empty, got %q", cfg.AutoCommitBranch)
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
