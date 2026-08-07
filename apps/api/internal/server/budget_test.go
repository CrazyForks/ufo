package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseSpendBudget(t *testing.T) {
	b := parseSpendBudget([]byte(`{}`))
	if b.limited() {
		t.Fatal("empty metadata should be unlimited")
	}
	b = parseSpendBudget([]byte(`{"budget":null}`))
	if b.limited() {
		t.Fatal("null budget must be unlimited")
	}
	b = parseSpendBudget([]byte(`{"budget":{"max_runs":0}}`))
	if b.limited() {
		t.Fatal("non-positive max is unlimited")
	}
	b = parseSpendBudget([]byte(`{"budget":{"period":"rolling_5h","max_runs":1}}`))
	if b.limited() {
		t.Fatal("unknown period must not enforce")
	}

	b = parseSpendBudget([]byte(`{"budget":{"period":"calendar_day","max_runs":2}}`))
	if b.Period != budgetPeriodDay || b.MaxRuns == nil || *b.MaxRuns != 2 {
		t.Fatalf("day run budget = %+v", b)
	}
	b = parseSpendBudget([]byte(`{"budget":{"period":"calendar_week","max_runs":5}}`))
	if b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 5 {
		t.Fatalf("week run budget = %+v", b)
	}
	b = parseSpendBudget([]byte(`{"budget":{"period":"calendar_month","max_tokens":1000}}`))
	if b.Period != budgetPeriodMonth || b.MaxTokens == nil || *b.MaxTokens != 1000 {
		t.Fatalf("month token budget = %+v", b)
	}
	b = parseSpendBudget([]byte(`{"budget":{"max_runs":40}}`))
	if !b.limited() || b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 40 {
		t.Fatalf("omitted period defaults to week = %+v", b)
	}
}

func TestMetadataTouchesBudget(t *testing.T) {
	if metadataTouchesBudget([]byte(`{}`)) || metadataTouchesBudget([]byte(`{"context":"x"}`)) {
		t.Fatal("no budget key")
	}
	if !metadataTouchesBudget([]byte(`{"budget":null}`)) || !metadataTouchesBudget([]byte(`{"budget":{"max_runs":1}}`)) {
		t.Fatal("budget key present")
	}
}

func TestPeriodWindowUTC(t *testing.T) {
	now := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC) // Wed → 2026-W27
	win, ok := periodWindowUTC(budgetPeriodDay, now)
	if !ok || win.Key != "2026-07-01" {
		t.Fatalf("day = %+v ok=%v", win, ok)
	}
	if !win.Start.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) ||
		!win.End.Equal(time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day bounds = %v .. %v", win.Start, win.End)
	}

	win, ok = periodWindowUTC(budgetPeriodWeek, now)
	if !ok {
		t.Fatal("week window")
	}
	if win.Key != "2026-W27" {
		t.Fatalf("week key = %q", win.Key)
	}
	if !win.Start.Equal(time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("week start = %v", win.Start)
	}
	if !win.End.Equal(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("week end = %v", win.End)
	}

	win, ok = periodWindowUTC(budgetPeriodMonth, now)
	if !ok || win.Key != "2026-07" {
		t.Fatalf("month = %+v ok=%v", win, ok)
	}
	if !win.Start.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) ||
		!win.End.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month bounds = %v .. %v", win.Start, win.End)
	}
}

func TestNormalizeRunUsageTotals(t *testing.T) {
	u := normalizeRunUsage(&runUsagePayload{
		InputTokens:  10,
		OutputTokens: 5,
		Source:       "pilot",
	})
	if u.TotalTokens != 15 {
		t.Fatalf("total = %d", u.TotalTokens)
	}
	neg := int64(-3)
	u = normalizeRunUsage(&runUsagePayload{DurationMs: &neg, CostMicros: &neg})
	if u.DurationMs == nil || *u.DurationMs != 0 || u.CostMicros == nil || *u.CostMicros != 0 {
		t.Fatalf("negatives not clamped: duration=%v cost=%v", u.DurationMs, u.CostMicros)
	}
	if u.Source != "" {
		t.Fatalf("empty provenance want \"\", got %q", u.Source)
	}
}

func TestOperationMetadataWithBudget(t *testing.T) {
	day, ok := operationMetadataWithBudget([]byte(`{"context":"keep"}`), json.RawMessage(`{"period":"calendar_day","max_runs":1}`))
	if !ok {
		t.Fatal("calendar_day budget patch")
	}
	b := parseSpendBudget(day)
	if b.Period != budgetPeriodDay || b.MaxRuns == nil || *b.MaxRuns != 1 {
		t.Fatalf("day budget = %+v", b)
	}
	if _, has := metadataMap(day)["context"]; !has {
		t.Fatal("must keep existing metadata")
	}
	week, ok := operationMetadataWithBudget(day, json.RawMessage(`{"period":"calendar_week","max_runs":3}`))
	if !ok {
		t.Fatal("calendar_week budget patch")
	}
	b = parseSpendBudget(week)
	if b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 3 {
		t.Fatalf("week budget = %+v", b)
	}
	month, ok := operationMetadataWithBudget(week, json.RawMessage(`{"period":"calendar_month","max_tokens":1000}`))
	if !ok {
		t.Fatal("calendar_month budget patch")
	}
	b = parseSpendBudget(month)
	if b.Period != budgetPeriodMonth || b.MaxTokens == nil || *b.MaxTokens != 1000 {
		t.Fatalf("month budget = %+v", b)
	}
	cleared, ok := operationMetadataWithBudget(month, json.RawMessage(`null`))
	if !ok {
		t.Fatal("null budget patch")
	}
	if parseSpendBudget(cleared).limited() {
		t.Fatalf("cleared still limited: %s", cleared)
	}
	if _, ok := operationMetadataWithBudget([]byte(`{}`), json.RawMessage(`{"period":"rolling_5h","max_runs":1}`)); ok {
		t.Fatal("invalid period must be rejected")
	}
}

func TestStampSpendBudgetInMetadata(t *testing.T) {
	day, ok := stampSpendBudgetInMetadata([]byte(`{"context":"keep","budget":{"period":"calendar_day","max_runs":2}}`))
	if !ok {
		t.Fatal("calendar_day stamp")
	}
	b := parseSpendBudget(day)
	if b.Period != budgetPeriodDay || b.MaxRuns == nil || *b.MaxRuns != 2 {
		t.Fatalf("day stamp = %+v", b)
	}
	if _, has := metadataMap(day)["context"]; !has {
		t.Fatal("must keep existing metadata")
	}
	week, ok := stampSpendBudgetInMetadata([]byte(`{"budget":{"period":"calendar_week","max_runs":3}}`))
	if !ok {
		t.Fatal("calendar_week stamp")
	}
	b = parseSpendBudget(week)
	if b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 3 {
		t.Fatalf("week stamp = %+v", b)
	}
	month, ok := stampSpendBudgetInMetadata([]byte(`{"budget":{"period":"calendar_month","max_tokens":1000}}`))
	if !ok {
		t.Fatal("calendar_month stamp")
	}
	b = parseSpendBudget(month)
	if b.Period != budgetPeriodMonth || b.MaxTokens == nil || *b.MaxTokens != 1000 {
		t.Fatalf("month stamp = %+v", b)
	}
	omitted, ok := stampSpendBudgetInMetadata([]byte(`{"budget":{"max_runs":4}}`))
	if !ok {
		t.Fatal("omitted period stamp")
	}
	b = parseSpendBudget(omitted)
	if b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 4 {
		t.Fatalf("omitted period defaults to week = %+v", b)
	}
	cleared, ok := stampSpendBudgetInMetadata([]byte(`{"budget":null}`))
	if !ok {
		t.Fatal("null budget")
	}
	if parseSpendBudget(cleared).limited() {
		t.Fatalf("cleared still limited: %s", cleared)
	}
	if _, ok := stampSpendBudgetInMetadata([]byte(`{"budget":{"period":"rolling_5h","max_runs":1}}`)); ok {
		t.Fatal("invalid period must be rejected")
	}
	untouched, ok := stampSpendBudgetInMetadata([]byte(`{"context":"only"}`))
	if !ok {
		t.Fatal("metadata without budget")
	}
	got := metadataMap(untouched)
	if _, has := got["budget"]; has {
		t.Fatal("must not invent budget")
	}
	if string(got["context"]) != `"only"` {
		t.Fatalf("untouched = %s", untouched)
	}
}
