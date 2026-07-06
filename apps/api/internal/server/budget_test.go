package server

import (
	"testing"
	"time"
)

func TestParseSpendBudget(t *testing.T) {
	b := parseSpendBudget([]byte(`{}`))
	if b.limited() {
		t.Fatal("empty metadata should be unlimited")
	}
	b = parseSpendBudget([]byte(`{"budget":{"max_runs":40}}`))
	if !b.limited() || b.Period != budgetPeriodWeek || b.MaxRuns == nil || *b.MaxRuns != 40 {
		t.Fatalf("default week budget = %+v", b)
	}
	b = parseSpendBudget([]byte(`{"budget":{"period":"calendar_month","max_tokens":1000}}`))
	if b.Period != budgetPeriodMonth || b.MaxTokens == nil || *b.MaxTokens != 1000 {
		t.Fatalf("month token budget = %+v", b)
	}
	b = parseSpendBudget([]byte(`{"budget":{"period":"rolling_5h","max_runs":1}}`))
	if b.limited() {
		t.Fatal("unknown period must not enforce")
	}
	b = parseSpendBudget([]byte(`{"budget":{"max_runs":0}}`))
	if b.limited() {
		t.Fatal("non-positive max is unlimited")
	}
	b = parseSpendBudget([]byte(`{"budget":null}`))
	if b.limited() {
		t.Fatal("null budget must be unlimited")
	}
}

func TestPeriodWindowUTCWeekAndMonth(t *testing.T) {
	now := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC) // Wed → 2026-W27
	win, ok := periodWindowUTC(budgetPeriodWeek, now)
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
