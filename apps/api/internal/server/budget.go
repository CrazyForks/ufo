package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"ufo/apps/api/internal/db"
)

const (
	budgetPeriodDay   = "calendar_day"
	budgetPeriodWeek  = "calendar_week"
	budgetPeriodMonth = "calendar_month"
)

func validBudgetPeriod(period string) bool {
	switch period {
	case budgetPeriodDay, budgetPeriodWeek, budgetPeriodMonth:
		return true
	default:
		return false
	}
}

type spendBudget struct {
	Period       string
	MaxRuns      *int64
	MaxTokens    *int64
	MaxUSDMicros *int64
}

func (b spendBudget) limited() bool {
	return b.MaxRuns != nil || b.MaxTokens != nil || b.MaxUSDMicros != nil
}

type budgetPeriodWindow struct {
	Start time.Time
	End   time.Time
	Key   string
}

func parseSpendBudget(metadata []byte) spendBudget {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &root); err != nil || len(root) == 0 {
		return spendBudget{}
	}
	raw, ok := root["budget"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return spendBudget{}
	}
	b, ok := parseSpendBudgetBody(raw)
	if !ok {
		return spendBudget{}
	}
	return b
}

func parseSpendBudgetBody(raw json.RawMessage) (spendBudget, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return spendBudget{}, true
	}
	var body struct {
		Period       string `json:"period"`
		MaxRuns      *int64 `json:"max_runs"`
		MaxTokens    *int64 `json:"max_tokens"`
		MaxUSDMicros *int64 `json:"max_usd_micros"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return spendBudget{}, false
	}
	b := spendBudget{
		Period:       strings.TrimSpace(body.Period),
		MaxRuns:      positiveOrNil(body.MaxRuns),
		MaxTokens:    positiveOrNil(body.MaxTokens),
		MaxUSDMicros: positiveOrNil(body.MaxUSDMicros),
	}
	if !b.limited() {
		if b.Period != "" && !validBudgetPeriod(b.Period) {
			return spendBudget{}, false
		}
		return spendBudget{}, true
	}
	if b.Period == "" {
		b.Period = budgetPeriodWeek
	}
	if !validBudgetPeriod(b.Period) {
		return spendBudget{}, false
	}
	return b, true
}

func normalizeSpendBudgetRaw(raw json.RawMessage) (json.RawMessage, bool) {
	b, ok := parseSpendBudgetBody(raw)
	if !ok {
		return nil, false
	}
	if !b.limited() {
		return json.RawMessage("null"), true
	}
	body := map[string]any{"period": b.Period}
	if b.MaxRuns != nil {
		body["max_runs"] = *b.MaxRuns
	}
	if b.MaxTokens != nil {
		body["max_tokens"] = *b.MaxTokens
	}
	if b.MaxUSDMicros != nil {
		body["max_usd_micros"] = *b.MaxUSDMicros
	}
	enc, err := json.Marshal(body)
	if err != nil {
		return nil, false
	}
	return enc, true
}

func stampSpendBudgetInMetadata(metadata []byte) ([]byte, bool) {
	m := metadataMap(metadata)
	raw, ok := m["budget"]
	if !ok {
		return metadata, true
	}
	next, ok := normalizeSpendBudgetRaw(raw)
	if !ok {
		return metadata, false
	}
	m["budget"] = next
	return metadataBytes(m), true
}

func metadataTouchesBudget(metadata []byte) bool {
	_, ok := metadataMap(metadata)["budget"]
	return ok
}

func positiveOrNil(v *int64) *int64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func periodWindowUTC(period string, now time.Time) (budgetPeriodWindow, bool) {
	now = now.UTC()
	switch period {
	case budgetPeriodDay:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return budgetPeriodWindow{
			Start: start,
			End:   start.AddDate(0, 0, 1),
			Key:   start.Format("2006-01-02"),
		}, true
	case budgetPeriodWeek:
		start := isoWeekStartUTC(now)
		end := start.AddDate(0, 0, 7)
		y, w := now.ISOWeek()
		return budgetPeriodWindow{
			Start: start,
			End:   end,
			Key:   fmt.Sprintf("%04d-W%02d", y, w),
		}, true
	case budgetPeriodMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		return budgetPeriodWindow{
			Start: start,
			End:   end,
			Key:   start.Format("2006-01"),
		}, true
	default:
		return budgetPeriodWindow{}, false
	}
}

func isoWeekStartUTC(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7 // Mon=0 ... Sun=6
	return day.AddDate(0, 0, -offset)
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

type budgetScope string

const (
	budgetScopeRover     budgetScope = "rover"
	budgetScopeFleet     budgetScope = "fleet"
	budgetScopeMission   budgetScope = "mission"
	budgetScopeOperation budgetScope = "operation"
)

func (s *Server) budgetExceeded(ctx context.Context, scope budgetScope, id int64, b spendBudget) (bool, string, error) {
	if !b.limited() {
		return false, "", nil
	}
	win, ok := periodWindowUTC(b.Period, time.Now())
	if !ok {
		return false, "", nil
	}
	key := fmt.Sprintf("%s:%d:%s", scope, id, win.Key)
	if b.MaxRuns != nil {
		n, err := s.countTerminalRuns(ctx, scope, id, win)
		if err != nil {
			return false, key, err
		}
		if n >= *b.MaxRuns {
			return true, key, nil
		}
	}
	if b.MaxTokens != nil || b.MaxUSDMicros != nil {
		tokens, cost, err := s.sumUsage(ctx, scope, id, win)
		if err != nil {
			return false, key, err
		}
		if b.MaxTokens != nil && tokens >= *b.MaxTokens {
			return true, key, nil
		}
		if b.MaxUSDMicros != nil && cost >= *b.MaxUSDMicros {
			return true, key, nil
		}
	}
	return false, key, nil
}

func (s *Server) countTerminalRuns(ctx context.Context, scope budgetScope, id int64, win budgetPeriodWindow) (int64, error) {
	start, end := timestamptz(win.Start), timestamptz(win.End)
	switch scope {
	case budgetScopeRover:
		return s.q.CountRoverTerminalRunsInRange(ctx, db.CountRoverTerminalRunsInRangeParams{
			RoverID: pgtype.Int8{Int64: id, Valid: true}, StartAt: start, EndAt: end,
		})
	case budgetScopeFleet:
		return s.q.CountFleetTerminalRunsInRange(ctx, db.CountFleetTerminalRunsInRangeParams{
			FleetID: id, StartAt: start, EndAt: end,
		})
	case budgetScopeMission:
		return s.q.CountMissionTerminalRunsInRange(ctx, db.CountMissionTerminalRunsInRangeParams{
			MissionID: pgtype.Int8{Int64: id, Valid: true}, StartAt: start, EndAt: end,
		})
	case budgetScopeOperation:
		return s.q.CountOperationTerminalRunsInRange(ctx, db.CountOperationTerminalRunsInRangeParams{
			OperationID: id, StartAt: start, EndAt: end,
		})
	default:
		return 0, nil
	}
}

func (s *Server) sumUsage(ctx context.Context, scope budgetScope, id int64, win budgetPeriodWindow) (tokens, cost int64, err error) {
	start, end := timestamptz(win.Start), timestamptz(win.End)
	switch scope {
	case budgetScopeRover:
		row, err := s.q.SumRoverUsageInRange(ctx, db.SumRoverUsageInRangeParams{
			RoverID: pgtype.Int8{Int64: id, Valid: true}, StartAt: start, EndAt: end,
		})
		return row.TotalTokens, row.CostMicros, err
	case budgetScopeFleet:
		row, err := s.q.SumFleetUsageInRange(ctx, db.SumFleetUsageInRangeParams{
			FleetID: id, StartAt: start, EndAt: end,
		})
		return row.TotalTokens, row.CostMicros, err
	case budgetScopeMission:
		row, err := s.q.SumMissionUsageInRange(ctx, db.SumMissionUsageInRangeParams{
			MissionID: pgtype.Int8{Int64: id, Valid: true}, StartAt: start, EndAt: end,
		})
		return row.TotalTokens, row.CostMicros, err
	case budgetScopeOperation:
		row, err := s.q.SumOperationUsageInRange(ctx, db.SumOperationUsageInRangeParams{
			OperationID: id, StartAt: start, EndAt: end,
		})
		return row.TotalTokens, row.CostMicros, err
	default:
		return 0, 0, nil
	}
}

func (s *Server) routineLoopBudgetBlocks(ctx context.Context, routine db.Routine) (blocked bool, periodKey string, err error) {
	fleet, err := s.q.GetFleetByID(ctx, routine.FleetID)
	if err != nil {
		return false, "", err
	}
	if hit, key, err := s.budgetExceeded(ctx, budgetScopeFleet, fleet.ID, parseSpendBudget(fleet.Metadata)); err != nil || hit {
		return hit, key, err
	}
	mission, err := s.q.GetMission(ctx, routine.MissionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", err
	}
	return s.budgetExceeded(ctx, budgetScopeMission, mission.ID, parseSpendBudget(mission.Metadata))
}

func (s *Server) preAcceptBudgetBlocks(ctx context.Context, rv db.Rover) (blocked bool, periodKey string, err error) {
	if hit, key, err := s.budgetExceeded(ctx, budgetScopeRover, rv.ID, parseSpendBudget(rv.Metadata)); err != nil || hit {
		return hit, key, err
	}
	fleet, err := s.q.GetFleetByID(ctx, rv.FleetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", err
	}
	return s.budgetExceeded(ctx, budgetScopeFleet, fleet.ID, parseSpendBudget(fleet.Metadata))
}

func (s *Server) postAcceptBudgetBlocks(ctx context.Context, run db.Run) (blocked bool, periodKey string, err error) {
	if run.MissionID.Valid {
		mission, err := s.q.GetMission(ctx, run.MissionID.Int64)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, "", err
		}
		if err == nil {
			if hit, key, err := s.budgetExceeded(ctx, budgetScopeMission, mission.ID, parseSpendBudget(mission.Metadata)); err != nil || hit {
				return hit, key, err
			}
		}
	}
	op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: run.OperationID, FleetID: run.FleetID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		return false, "", err
	}
	return s.budgetExceeded(ctx, budgetScopeOperation, op.ID, parseSpendBudget(op.Metadata))
}

func (s *Server) maybeSignalBudgetExhausted(ctx context.Context, fleetID int64, periodKey, roverName string) {
	if periodKey == "" {
		return
	}
	scope, id, ok := parseBudgetPeriodKey(periodKey)
	if !ok {
		return
	}
	meta, err := s.budgetScopeMetadata(ctx, fleetID, scope, id)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("budget exhaust load: %s %d: %v", scope, id, err)
		}
		return
	}
	if budgetAlreadySignaled(meta, periodKey) {
		return
	}
	flag, _ := json.Marshal(map[string]any{
		"budget_accounting": map[string]any{
			"exhausted_period_key": periodKey,
			"exhausted_at":         time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err := s.mergeBudgetScopeMetadata(ctx, fleetID, scope, id, flag); err != nil {
		log.Printf("budget exhaust metadata: %s %d: %v", scope, id, err)
	}
	title, body := budgetExhaustedCopy(scope, periodKey, roverName)
	opID := int64(0)
	if scope == budgetScopeOperation {
		opID = id
	}
	s.notifyMembers(ctx, fleetID, opID, "budget_exhausted", "attention", title, body)
}

func parseBudgetPeriodKey(periodKey string) (scope budgetScope, id int64, ok bool) {
	scopeStr, rest, cut := strings.Cut(periodKey, ":")
	if !cut {
		return "", 0, false
	}
	idStr, _, cut := strings.Cut(rest, ":")
	if !cut {
		return "", 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}
	scope = budgetScope(scopeStr)
	switch scope {
	case budgetScopeRover, budgetScopeFleet, budgetScopeMission, budgetScopeOperation:
		return scope, id, true
	default:
		return "", 0, false
	}
}

func budgetAlreadySignaled(metadata []byte, periodKey string) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(metadata, &root) != nil {
		return false
	}
	raw, ok := root["budget_accounting"]
	if !ok {
		return false
	}
	var acc struct {
		ExhaustedPeriodKey string `json:"exhausted_period_key"`
	}
	return json.Unmarshal(raw, &acc) == nil && acc.ExhaustedPeriodKey == periodKey
}

func (s *Server) budgetScopeMetadata(ctx context.Context, fleetID int64, scope budgetScope, id int64) ([]byte, error) {
	switch scope {
	case budgetScopeRover:
		row, err := s.q.GetRoverByID(ctx, id)
		return row.Metadata, err
	case budgetScopeFleet:
		row, err := s.q.GetFleetByID(ctx, id)
		return row.Metadata, err
	case budgetScopeMission:
		row, err := s.q.GetMission(ctx, id)
		return row.Metadata, err
	case budgetScopeOperation:
		row, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: id, FleetID: fleetID})
		return row.Metadata, err
	default:
		return nil, fmt.Errorf("unknown budget scope %q", scope)
	}
}

func (s *Server) mergeBudgetScopeMetadata(ctx context.Context, fleetID int64, scope budgetScope, id int64, flag []byte) error {
	switch scope {
	case budgetScopeRover:
		return s.q.MergeRoverMetadata(ctx, db.MergeRoverMetadataParams{ID: id, Metadata: flag})
	case budgetScopeFleet:
		return s.q.MergeFleetMetadata(ctx, db.MergeFleetMetadataParams{ID: id, Metadata: flag})
	case budgetScopeMission:
		return s.q.MergeMissionMetadata(ctx, db.MergeMissionMetadataParams{ID: id, Metadata: flag})
	case budgetScopeOperation:
		return s.q.MergeOperationMetadata(ctx, db.MergeOperationMetadataParams{
			ID: id, FleetID: fleetID, Metadata: flag,
		})
	default:
		return fmt.Errorf("unknown budget scope %q", scope)
	}
}

func budgetExhaustedCopy(scope budgetScope, periodKey, roverName string) (title, body string) {
	switch scope {
	case budgetScopeMission:
		return "Mission budget exhausted",
			fmt.Sprintf("A mission spend budget for %s is exhausted; matching work will not be accepted until the period resets or the cap is raised.", periodKey)
	case budgetScopeOperation:
		return "Operation budget exhausted",
			fmt.Sprintf("An operation spend budget for %s is exhausted; matching work will not be accepted until the period resets or the cap is raised.", periodKey)
	case budgetScopeFleet:
		return "Fleet budget exhausted",
			fmt.Sprintf("A fleet spend budget for %s is exhausted; rovers will not accept matching work until the period resets or the cap is raised.", periodKey)
	default:
		return fmt.Sprintf("Rover %s budget exhausted", roverName),
			fmt.Sprintf("A spend budget for %s is exhausted; this rover will not accept matching work until the period resets or the cap is raised.", periodKey)
	}
}

type runUsagePayload struct {
	Provider         string          `json:"provider"`
	Model            string          `json:"model"`
	Source           string          `json:"source"`
	InputTokens      int64           `json:"input_tokens"`
	OutputTokens     int64           `json:"output_tokens"`
	CacheReadTokens  int64           `json:"cache_read_tokens"`
	CacheWriteTokens int64           `json:"cache_write_tokens"`
	ReasoningTokens  int64           `json:"reasoning_tokens"`
	TotalTokens      int64           `json:"total_tokens"`
	DurationMs       *int64          `json:"duration_ms"`
	CostMicros       *int64          `json:"cost_micros"`
	Metadata         json.RawMessage `json:"metadata"`
}

type usageTotalsDTO struct {
	Runs         int64  `json:"runs"`
	TotalTokens  int64  `json:"total_tokens"`
	CostMicros   int64  `json:"cost_micros"`
	MaxRuns      *int64 `json:"max_runs,omitempty"`
	MaxTokens    *int64 `json:"max_tokens,omitempty"`
	MaxUSDMicros *int64 `json:"max_usd_micros,omitempty"`
}

type missionUsageDTO struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	usageTotalsDTO
}

type pilotUsageDTO struct {
	Pilot string `json:"pilot"`
	usageTotalsDTO
}

type roverUsageDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	usageTotalsDTO
}

type operationUsageDTO struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	MissionID string `json:"mission_id"`
	usageTotalsDTO
}

type dayUsageDTO struct {
	Day string `json:"day"`
	usageTotalsDTO
}

type usageSummaryDTO struct {
	Period     string              `json:"period"`
	PeriodKey  string              `json:"period_key"`
	StartAt    time.Time           `json:"start_at"`
	EndAt      time.Time           `json:"end_at"`
	Fleet      usageTotalsDTO      `json:"fleet"`
	Missions   []missionUsageDTO   `json:"missions"`
	Pilots     []pilotUsageDTO     `json:"pilots"`
	Rovers     []roverUsageDTO     `json:"rovers"`
	Operations []operationUsageDTO `json:"operations"`
	Days       []dayUsageDTO       `json:"days"`
}

type usageFact struct {
	runs   int64
	tokens int64
	cost   int64
}

func addUsageRuns(dst map[string]*usageFact, key string, runs int64) {
	if key == "" && runs == 0 {
		return
	}
	f := dst[key]
	if f == nil {
		f = &usageFact{}
		dst[key] = f
	}
	f.runs += runs
}

func addUsageTokens(dst map[string]*usageFact, key string, tokens, cost int64) {
	if key == "" && tokens == 0 && cost == 0 {
		return
	}
	f := dst[key]
	if f == nil {
		f = &usageFact{}
		dst[key] = f
	}
	f.tokens += tokens
	f.cost += cost
}

func budgetLimitFields(b spendBudget) (maxRuns, maxTokens, maxUSD *int64) {
	return b.MaxRuns, b.MaxTokens, b.MaxUSDMicros
}

func usageTotalsFromBudget(runs, tokens, cost int64, b spendBudget) usageTotalsDTO {
	mr, mt, mu := budgetLimitFields(b)
	return usageTotalsDTO{
		Runs: runs, TotalTokens: tokens, CostMicros: cost,
		MaxRuns: mr, MaxTokens: mt, MaxUSDMicros: mu,
	}
}

func operationCode(missionKey string, sequence int32) string {
	if sequence > 0 && missionKey != "" {
		return fmt.Sprintf("%s-%d", missionKey, sequence)
	}
	return missionKey
}

func emptyUsageSummary(period string, win budgetPeriodWindow) usageSummaryDTO {
	return usageSummaryDTO{
		Period:     period,
		PeriodKey:  win.Key,
		StartAt:    win.Start,
		EndAt:      win.End,
		Missions:   []missionUsageDTO{},
		Pilots:     []pilotUsageDTO{},
		Rovers:     []roverUsageDTO{},
		Operations: []operationUsageDTO{},
		Days:       []dayUsageDTO{},
	}
}

func (s *Server) fleetUsageTotals(ctx context.Context, fleet db.Fleet, win budgetPeriodWindow) (usageTotalsDTO, error) {
	runs, err := s.countTerminalRuns(ctx, budgetScopeFleet, fleet.ID, win)
	if err != nil {
		return usageTotalsDTO{}, err
	}
	tokens, cost, err := s.sumUsage(ctx, budgetScopeFleet, fleet.ID, win)
	if err != nil {
		return usageTotalsDTO{}, err
	}
	return usageTotalsFromBudget(runs, tokens, cost, parseSpendBudget(fleet.Metadata)), nil
}

func (s *Server) getUsage(w http.ResponseWriter, r *http.Request) {
	rawFleet := strings.TrimSpace(r.URL.Query().Get("fleet_id"))
	if rawFleet == "" {
		httpError(w, http.StatusBadRequest, "fleet_id is required")
		return
	}
	fleetID, ok := s.resolveFleetPublicID(w, r, rawFleet)
	if !ok {
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = budgetPeriodWeek
	}
	if !validBudgetPeriod(period) {
		httpError(w, http.StatusBadRequest, "period must be calendar_day, calendar_week, or calendar_month")
		return
	}
	rawMission := strings.TrimSpace(r.URL.Query().Get("mission_id"))
	rawOperation := strings.TrimSpace(r.URL.Query().Get("operation_id"))
	if rawMission != "" && rawOperation != "" {
		httpError(w, http.StatusBadRequest, "mission_id and operation_id cannot both be set")
		return
	}
	win, ok := periodWindowUTC(period, time.Now())
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid period")
		return
	}
	ctx := r.Context()
	fleet, err := s.q.GetFleetByID(ctx, fleetID)
	if err != nil {
		serverError(w, err)
		return
	}
	if rawOperation != "" {
		out, err := s.usageForOperation(ctx, fleet, rawOperation, period, win)
		if err != nil {
			if errors.Is(err, errInvalidUsageScope) {
				httpError(w, http.StatusBadRequest, "invalid operation_id")
				return
			}
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "operation not found")
				return
			}
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if rawMission != "" {
		out, err := s.usageForMission(ctx, fleet, rawMission, period, win)
		if err != nil {
			if errors.Is(err, errInvalidUsageScope) {
				httpError(w, http.StatusBadRequest, "invalid mission_id")
				return
			}
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "mission not found")
				return
			}
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	out := emptyUsageSummary(period, win)
	out.Fleet, err = s.fleetUsageTotals(ctx, fleet, win)
	if err != nil {
		serverError(w, err)
		return
	}
	start, end := timestamptz(win.Start), timestamptz(win.End)
	missions, err := s.q.ListMissionUsageInRange(ctx, db.ListMissionUsageInRangeParams{
		FleetID: fleet.ID, StartAt: start, EndAt: end,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	for _, m := range missions {
		out.Missions = append(out.Missions, missionUsageDTO{
			ID: uuidStr(m.PublicID), Key: m.Key, Name: m.Name,
			usageTotalsDTO: usageTotalsFromBudget(m.Runs, m.TotalTokens, m.CostMicros, parseSpendBudget(m.Metadata)),
		})
	}
	if err := s.appendUsageBreakdowns(ctx, fleet.ID, start, end, win, &out); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) usageForOperation(ctx context.Context, fleet db.Fleet, rawID, period string, win budgetPeriodWindow) (usageSummaryDTO, error) {
	pid, ok := parseUUID(rawID)
	if !ok {
		return usageSummaryDTO{}, errInvalidUsageScope
	}
	op, err := s.q.GetOperationByPublicID(ctx, pid)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	if op.FleetID != fleet.ID {
		return usageSummaryDTO{}, pgx.ErrNoRows
	}
	mission, err := s.q.GetMission(ctx, op.MissionID)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	out := emptyUsageSummary(period, win)
	runs, err := s.countTerminalRuns(ctx, budgetScopeOperation, op.ID, win)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	tokens, cost, err := s.sumUsage(ctx, budgetScopeOperation, op.ID, win)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	out.Operations = []operationUsageDTO{{
		ID: uuidStr(op.PublicID), Code: operationCode(mission.Key, op.Sequence), Title: op.Title, MissionID: uuidStr(mission.PublicID),
		usageTotalsDTO: usageTotalsFromBudget(runs, tokens, cost, parseSpendBudget(op.Metadata)),
	}}
	return out, nil
}

func (s *Server) usageForMission(ctx context.Context, fleet db.Fleet, rawID, period string, win budgetPeriodWindow) (usageSummaryDTO, error) {
	pid, ok := parseUUID(rawID)
	if !ok {
		return usageSummaryDTO{}, errInvalidUsageScope
	}
	mission, err := s.q.GetMissionByPublicID(ctx, pid)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	if mission.FleetID != fleet.ID {
		return usageSummaryDTO{}, pgx.ErrNoRows
	}
	out := emptyUsageSummary(period, win)
	runs, err := s.countTerminalRuns(ctx, budgetScopeMission, mission.ID, win)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	tokens, cost, err := s.sumUsage(ctx, budgetScopeMission, mission.ID, win)
	if err != nil {
		return usageSummaryDTO{}, err
	}
	out.Missions = []missionUsageDTO{{
		ID: uuidStr(mission.PublicID), Key: mission.Key, Name: mission.Name,
		usageTotalsDTO: usageTotalsFromBudget(runs, tokens, cost, parseSpendBudget(mission.Metadata)),
	}}
	start, end := timestamptz(win.Start), timestamptz(win.End)
	rows, err := s.q.ListMissionOperationUsageInRange(ctx, db.ListMissionOperationUsageInRangeParams{
		FleetID: fleet.ID, MissionID: mission.ID, StartAt: start, EndAt: end,
	})
	if err != nil {
		return usageSummaryDTO{}, err
	}
	for _, row := range rows {
		out.Operations = append(out.Operations, operationUsageDTO{
			ID: uuidStr(row.PublicID), Code: operationCode(row.MissionKey, row.Sequence), Title: row.Title, MissionID: uuidStr(row.MissionID),
			usageTotalsDTO: usageTotalsFromBudget(row.Runs, row.TotalTokens, row.CostMicros, parseSpendBudget(row.Metadata)),
		})
	}
	sort.Slice(out.Operations, func(i, j int) bool {
		if out.Operations[i].TotalTokens != out.Operations[j].TotalTokens {
			return out.Operations[i].TotalTokens > out.Operations[j].TotalTokens
		}
		if out.Operations[i].Code != out.Operations[j].Code {
			return out.Operations[i].Code < out.Operations[j].Code
		}
		return out.Operations[i].ID < out.Operations[j].ID
	})
	return out, nil
}

var errInvalidUsageScope = errors.New("invalid usage scope id")

func (s *Server) appendUsageBreakdowns(ctx context.Context, fleetID int64, start, end pgtype.Timestamptz, win budgetPeriodWindow, out *usageSummaryDTO) error {
	g, ctx := errgroup.WithContext(ctx)
	var (
		pilotRuns  []db.ListPilotTerminalRunsInRangeRow
		pilotUsage []db.ListPilotUsageFactsInRangeRow
		roverRuns  []db.ListRoverTerminalRunsInRangeRow
		roverUsage []db.ListRoverUsageFactsInRangeRow
		dayRuns    []db.ListDayTerminalRunsInRangeRow
		dayUsage   []db.ListDayUsageFactsInRangeRow
	)
	g.Go(func() error {
		rows, err := s.q.ListPilotTerminalRunsInRange(ctx, db.ListPilotTerminalRunsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		pilotRuns = rows
		return nil
	})
	g.Go(func() error {
		rows, err := s.q.ListPilotUsageFactsInRange(ctx, db.ListPilotUsageFactsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		pilotUsage = rows
		return nil
	})
	g.Go(func() error {
		rows, err := s.q.ListRoverTerminalRunsInRange(ctx, db.ListRoverTerminalRunsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		roverRuns = rows
		return nil
	})
	g.Go(func() error {
		rows, err := s.q.ListRoverUsageFactsInRange(ctx, db.ListRoverUsageFactsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		roverUsage = rows
		return nil
	})
	g.Go(func() error {
		rows, err := s.q.ListDayTerminalRunsInRange(ctx, db.ListDayTerminalRunsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		dayRuns = rows
		return nil
	})
	g.Go(func() error {
		rows, err := s.q.ListDayUsageFactsInRange(ctx, db.ListDayUsageFactsInRangeParams{
			FleetID: fleetID, StartAt: start, EndAt: end,
		})
		if err != nil {
			return err
		}
		dayUsage = rows
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}

	pilotFacts := map[string]*usageFact{}
	for _, row := range pilotRuns {
		if row.Pilot == "" {
			continue
		}
		addUsageRuns(pilotFacts, row.Pilot, row.Runs)
	}
	for _, row := range pilotUsage {
		if row.Pilot == "" {
			continue
		}
		addUsageTokens(pilotFacts, row.Pilot, row.TotalTokens, row.CostMicros)
	}
	for pilot, fact := range pilotFacts {
		if fact.runs == 0 && fact.tokens == 0 && fact.cost == 0 {
			continue
		}
		out.Pilots = append(out.Pilots, pilotUsageDTO{
			Pilot:          pilot,
			usageTotalsDTO: usageTotalsDTO{Runs: fact.runs, TotalTokens: fact.tokens, CostMicros: fact.cost},
		})
	}
	sort.Slice(out.Pilots, func(i, j int) bool {
		if out.Pilots[i].TotalTokens != out.Pilots[j].TotalTokens {
			return out.Pilots[i].TotalTokens > out.Pilots[j].TotalTokens
		}
		return out.Pilots[i].Pilot < out.Pilots[j].Pilot
	})

	type roverKey struct{ id, name string }
	roverFacts := map[string]*usageFact{}
	roverMeta := map[string]roverKey{}
	roverKeyOf := func(id pgtype.UUID, name string) string {
		k := uuidStr(id)
		if k == "" {
			return ""
		}
		roverMeta[k] = roverKey{id: k, name: name}
		return k
	}
	for _, row := range roverRuns {
		if key := roverKeyOf(row.PublicID, row.Name); key != "" {
			addUsageRuns(roverFacts, key, row.Runs)
		}
	}
	for _, row := range roverUsage {
		if key := roverKeyOf(row.PublicID, row.Name); key != "" {
			addUsageTokens(roverFacts, key, row.TotalTokens, row.CostMicros)
		}
	}
	for key, fact := range roverFacts {
		if fact.runs == 0 && fact.tokens == 0 && fact.cost == 0 {
			continue
		}
		meta := roverMeta[key]
		out.Rovers = append(out.Rovers, roverUsageDTO{
			ID:             meta.id,
			Name:           meta.name,
			usageTotalsDTO: usageTotalsDTO{Runs: fact.runs, TotalTokens: fact.tokens, CostMicros: fact.cost},
		})
	}
	sort.Slice(out.Rovers, func(i, j int) bool {
		if out.Rovers[i].TotalTokens != out.Rovers[j].TotalTokens {
			return out.Rovers[i].TotalTokens > out.Rovers[j].TotalTokens
		}
		if out.Rovers[i].Name != out.Rovers[j].Name {
			return out.Rovers[i].Name < out.Rovers[j].Name
		}
		return out.Rovers[i].ID < out.Rovers[j].ID
	})

	dayFacts := map[string]*usageFact{}
	for _, row := range dayRuns {
		addUsageRuns(dayFacts, row.Day, row.Runs)
	}
	for _, row := range dayUsage {
		addUsageTokens(dayFacts, row.Day, row.TotalTokens, row.CostMicros)
	}
	for d := win.Start; d.Before(win.End); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		fact := dayFacts[key]
		row := dayUsageDTO{Day: key}
		if fact != nil {
			row.Runs = fact.runs
			row.TotalTokens = fact.tokens
			row.CostMicros = fact.cost
		}
		out.Days = append(out.Days, row)
	}
	return nil
}

func normalizeRunUsage(u *runUsagePayload) runUsagePayload {
	if u == nil {
		return runUsagePayload{Metadata: json.RawMessage(`{}`)}
	}
	out := *u
	if out.InputTokens < 0 {
		out.InputTokens = 0
	}
	if out.OutputTokens < 0 {
		out.OutputTokens = 0
	}
	if out.CacheReadTokens < 0 {
		out.CacheReadTokens = 0
	}
	if out.CacheWriteTokens < 0 {
		out.CacheWriteTokens = 0
	}
	if out.ReasoningTokens < 0 {
		out.ReasoningTokens = 0
	}
	if out.TotalTokens <= 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens + out.CacheReadTokens + out.CacheWriteTokens + out.ReasoningTokens
	}
	if out.DurationMs != nil && *out.DurationMs < 0 {
		z := int64(0)
		out.DurationMs = &z
	}
	if out.CostMicros != nil && *out.CostMicros < 0 {
		z := int64(0)
		out.CostMicros = &z
	}
	switch strings.TrimSpace(out.Source) {
	case "pilot", "estimate", "run":
		out.Source = strings.TrimSpace(out.Source)
	default:
		if out.TotalTokens > 0 {
			out.Source = "pilot"
		} else {
			out.Source = ""
		}
	}
	out.Metadata = metadataBytes(metadataMap(out.Metadata))
	return out
}

func (s *Server) storeRunUsage(ctx context.Context, run db.Run, payload *runUsagePayload) {
	u := normalizeRunUsage(payload)
	if u.Source == "" && u.TotalTokens == 0 && (u.DurationMs == nil || *u.DurationMs <= 0) && u.CostMicros == nil && len(metadataMap(u.Metadata)) == 0 {
		return
	}
	var duration pgtype.Int8
	if u.DurationMs != nil {
		duration = pgtype.Int8{Int64: *u.DurationMs, Valid: true}
	}
	var cost pgtype.Int8
	if u.CostMicros != nil {
		cost = pgtype.Int8{Int64: *u.CostMicros, Valid: true}
	}
	_, err := s.q.UpsertRunUsage(ctx, db.UpsertRunUsageParams{
		RunID:            run.ID,
		FleetID:          run.FleetID,
		OperationID:      run.OperationID,
		RoverID:          run.RoverID,
		Pilot:            run.Pilot,
		Provider:         u.Provider,
		Model:            u.Model,
		Source:           optText(u.Source),
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		ReasoningTokens:  u.ReasoningTokens,
		TotalTokens:      u.TotalTokens,
		DurationMs:       duration,
		CostMicros:       cost,
		Metadata:         u.Metadata,
	})
	if err != nil {
		log.Printf("storeRunUsage run %d: %v", run.ID, err)
	}
}

func toRunUsageDTO(u db.RunUsage) *runUsageDTO {
	d := &runUsageDTO{
		Provider:         u.Provider,
		Model:            u.Model,
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		ReasoningTokens:  u.ReasoningTokens,
		TotalTokens:      u.TotalTokens,
		Metadata:         metadataJSON(u.Metadata),
		CreatedAt:        u.CreatedAt.Time.UTC(),
	}
	if u.Source.Valid {
		d.Source = u.Source.String
	}
	if u.DurationMs.Valid {
		v := u.DurationMs.Int64
		d.DurationMs = &v
	}
	if u.CostMicros.Valid {
		v := u.CostMicros.Int64
		d.CostMicros = &v
	}
	return d
}

func (s *Server) loadRunUsageDTO(ctx context.Context, runID int64) *runUsageDTO {
	u, err := s.q.GetRunUsage(ctx, runID)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil
		}
		log.Printf("GetRunUsage %d: %v", runID, err)
		return nil
	}
	return toRunUsageDTO(u)
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
