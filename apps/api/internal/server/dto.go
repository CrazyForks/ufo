package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"ufo/apps/api/internal/db"
)

// The wire never carries internal bigint ids. Each DTO emits the resource's
// public id as `id` and expands FK references to the referenced resource's
// public id (resolved via batch lookups — see the map* helpers below).

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func parseUUID(s string) (pgtype.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return pgtype.UUID{Bytes: id, Valid: true}, true
}

// ---- simple DTOs (own id only) ----

type userDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func toUserDTO(u db.User) userDTO {
	return userDTO{ID: uuidStr(u.PublicID), Email: u.Email, Name: u.Name}
}

type fleetDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func toFleetDTO(f db.Fleet) fleetDTO {
	return fleetDTO{ID: uuidStr(f.PublicID), Name: f.Name, Kind: f.Kind}
}

// pilotDTO is a pilot kind with the count of fleet rovers it can drive.
type pilotDTO struct {
	Kind   string `json:"kind"`
	Rovers int    `json:"rovers"` // rovers in the fleet this pilot can drive
	Online bool   `json:"online"` // at least one of those is online
}

type missionDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

func toMissionDTO(m db.Mission) missionDTO {
	return missionDTO{ID: uuidStr(m.PublicID), Name: m.Name, Key: m.Key}
}

type enrollmentCodeDTO struct {
	ID            string             `json:"id"`
	Code          string             `json:"code"`
	Name          string             `json:"name"`
	RemainingUses int32              `json:"remaining_uses"`
	CreatedAt     pgtype.Timestamptz `json:"created_at"`
	ExpiresAt     pgtype.Timestamptz `json:"expires_at"`
}

func toEnrollmentCodeDTO(t db.EnrollmentCode) enrollmentCodeDTO {
	return enrollmentCodeDTO{
		ID: uuidStr(t.PublicID), Code: "•••••", Name: t.Name, RemainingUses: t.RemainingUses,
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt,
	}
}

type memberDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func toMemberDTO(m db.ListMembersRow) memberDTO {
	return memberDTO{ID: uuidStr(m.ID), Email: m.Email, Name: m.Name, Role: m.Role}
}

type invitationDTO struct {
	ID           string             `json:"id"`
	InviteeEmail string             `json:"invitee_email"`
	Role         string             `json:"role"`
	Status       string             `json:"status"`
	ExpiresAt    pgtype.Timestamptz `json:"expires_at"`
}

func toInvitationDTO(i db.Invitation) invitationDTO {
	return invitationDTO{ID: uuidStr(i.PublicID), InviteeEmail: i.InviteeEmail, Role: i.Role, Status: i.Status, ExpiresAt: i.ExpiresAt}
}

type myInviteDTO struct {
	ID           string `json:"id"`
	FleetID      string `json:"fleet_id"`
	FleetName    string `json:"fleet_name"`
	Role         string `json:"role"`
	InviteeEmail string `json:"invitee_email"`
}

func toMyInviteDTO(i db.InvitationsForEmailRow) myInviteDTO {
	return myInviteDTO{
		ID: uuidStr(i.PublicID), FleetID: uuidStr(i.FleetPublicID),
		FleetName: i.FleetName, Role: i.Role, InviteeEmail: i.InviteeEmail,
	}
}

// runEventDTO / artifactDTO carry no own id (never referenced) — just content.
type runEventDTO struct {
	Kind      string             `json:"kind"`
	Message   string             `json:"message"`
	CreatedAt pgtype.Timestamptz `json:"created_at"`
}

func toRunEventDTO(e db.RunEvent) runEventDTO {
	return runEventDTO{Kind: e.Kind, Message: e.Message, CreatedAt: e.CreatedAt}
}

type artifactDTO struct {
	Kind      string             `json:"kind"`
	Name      string             `json:"name"`
	Content   string             `json:"content"`
	CreatedAt pgtype.Timestamptz `json:"created_at"`
}

func toArtifactDTO(a db.Artifact) artifactDTO {
	return artifactDTO{Kind: a.Kind, Name: a.Name, Content: a.Content, CreatedAt: a.CreatedAt}
}

// ---- DTOs with FK references (need public id expansion) ----

type operationDTO struct {
	ID                   string               `json:"id"`
	Title                string               `json:"title"`
	Body                 string               `json:"body"`
	Status               string               `json:"status"`
	ActiveRunState       string               `json:"active_run_state"`
	MissionID            string               `json:"mission_id"`
	Sequence             int32                `json:"sequence"`
	Priority             int16                `json:"priority"`
	AssigneeType         *string              `json:"assignee_type"`
	AssigneeID           *string              `json:"assignee_id"`         // user/crew public id
	AssigneePilotKind    *string              `json:"assignee_pilot_kind"` // when assignee_type=pilot
	RequiredTags         []string             `json:"required_tags"`
	ExcludedTags         []string             `json:"excluded_tags"`
	Labels               []labelDTO           `json:"labels"`
	Reactions            []reactionDTO        `json:"reactions"`
	SubOperationProgress subOperationProgress `json:"sub_operation_progress"`
	StartDate            *string              `json:"start_date"`
	DueDate              *string              `json:"due_date"`
	MainOperationID      *string              `json:"main_operation_id"`
	Orchestrating        bool                 `json:"orchestrating"`
	Archived             bool                 `json:"archived"`
	StartedAt            pgtype.Timestamptz   `json:"started_at"`
	FinishedAt           pgtype.Timestamptz   `json:"finished_at"`
	CreatedBy            *string              `json:"created_by"`
	CreatedAt            pgtype.Timestamptz   `json:"created_at"`
	UpdatedAt            pgtype.Timestamptz   `json:"updated_at"`
}

type subOperationProgress struct {
	Total int64 `json:"total"`
	Done  int64 `json:"done"`
}

type labelDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func toLabelDTO(l db.Label) labelDTO {
	return labelDTO{ID: uuidStr(l.PublicID), Name: l.Name, Color: l.Color}
}

type pullRequestDTO struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Number *int32 `json:"number"`
}

func toPullRequestDTO(p db.PullRequest) pullRequestDTO {
	d := pullRequestDTO{ID: uuidStr(p.PublicID), URL: p.Url, Title: p.Title, State: p.State}
	if p.Number.Valid {
		n := p.Number.Int32
		d.Number = &n
	}
	return d
}

// operationReferenceDTO is a compact operation reference (relations, search) — enough for the
// web to render the code (mission_id + sequence) and a status icon.
type operationReferenceDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Sequence  int32  `json:"sequence"`
	MissionID string `json:"mission_id"`
}

type relationDTO struct {
	ID        string                `json:"id"`
	Kind      string                `json:"kind"` // blocks | blocked_by | relates | duplicate | duplicated_by
	Operation operationReferenceDTO `json:"operation"`
}

// relationKind maps the stored (kind, direction) to the display-facing kind.
func relationKind(kind string, outgoing bool) string {
	switch kind {
	case "blocks":
		if outgoing {
			return "blocks"
		}
		return "blocked_by"
	case "duplicate":
		if outgoing {
			return "duplicate"
		}
		return "duplicated_by"
	default:
		return "relates"
	}
}

func toRelationDTOs(rows []db.ListRelationsForOperationRow) []relationDTO {
	out := make([]relationDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, relationDTO{
			ID:   uuidStr(r.RelationID),
			Kind: relationKind(r.Kind, r.Outgoing),
			Operation: operationReferenceDTO{
				ID:        uuidStr(r.OperationPublicID),
				Title:     r.Title,
				Status:    r.Status,
				Sequence:  r.Sequence,
				MissionID: uuidStr(r.MissionID),
			},
		})
	}
	return out
}

type reactionDTO struct {
	Emoji string   `json:"emoji"`
	Count int64    `json:"count"`
	Mine  bool     `json:"mine"`
	Users []string `json:"users"` // reactors, oldest first (hover tooltip)
}

type commentDTO struct {
	ID              string             `json:"id"`
	AuthorType      string             `json:"author_type"`
	AuthorID        *string            `json:"author_id"`         // user public id
	AuthorPilotKind *string            `json:"author_pilot_kind"` // when author_type=pilot
	Body            string             `json:"body"`
	Reactions       []reactionDTO      `json:"reactions"`
	CreatedAt       pgtype.Timestamptz `json:"created_at"`
}

func dateStr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

type runDTO struct {
	ID          string             `json:"id"`
	OperationID string             `json:"operation_id"`
	Pilot       string             `json:"pilot"`
	State       string             `json:"state"`
	NeedsInput  bool               `json:"needs_input"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
	UpdatedAt   pgtype.Timestamptz `json:"updated_at"`
}

type crewMemberDTO struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"` // user public id, or pilot kind
	Role       string `json:"role"`
}

type crewDTO struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Members []crewMemberDTO `json:"members"`
}

type signalDTO struct {
	ID          string             `json:"id"`
	OperationID *string            `json:"operation_id"`
	Type        string             `json:"type"`
	Severity    string             `json:"severity"`
	Title       string             `json:"title"`
	Body        string             `json:"body"`
	Read        bool               `json:"read"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
}

// runMessageDTO serializes a transcript message with input as raw JSON (the db
// row stores jsonb as []byte, which would otherwise marshal as base64). No own
// id — the client orders by sequence.
type runMessageDTO struct {
	Sequence  int32           `json:"sequence"`
	Type      string          `json:"type"`
	Tool      string          `json:"tool,omitempty"`
	Content   string          `json:"content,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func toRunMessageDTO(m db.RunMessage) runMessageDTO {
	d := runMessageDTO{Sequence: m.Sequence, Type: m.Type, CreatedAt: m.CreatedAt.Time}
	if m.Tool.Valid {
		d.Tool = m.Tool.String
	}
	if m.Content.Valid {
		d.Content = m.Content.String
	}
	if m.Output.Valid {
		d.Output = m.Output.String
	}
	if len(m.Input) > 0 {
		d.Input = json.RawMessage(m.Input)
	}
	return d
}

// ---- batch internal id -> public id maps (reference expansion) ----

func dedupeIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return ids
	}
	seen := make(map[int64]struct{}, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Server) mapUsers(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.PublicIDsForUsers(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = uuidStr(r.PublicID)
	}
	return out
}

func (s *Server) mapCrews(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.PublicIDsForCrews(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = uuidStr(r.PublicID)
	}
	return out
}

func (s *Server) mapMissions(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.PublicIDsForMissions(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = uuidStr(r.PublicID)
	}
	return out
}

func (s *Server) mapOperations(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.PublicIDsForOperations(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = uuidStr(r.PublicID)
	}
	return out
}

// polyUUID resolves a polymorphic (type, id) reference to its public id. Pilots
// are referenced by kind, not id, so they're handled separately.
func polyUUID(typ string, id pgtype.Int8, users, crews map[int64]string) string {
	if !id.Valid {
		return ""
	}
	switch typ {
	case "user":
		return users[id.Int64]
	case "crew":
		return crews[id.Int64]
	}
	return ""
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- list builders ----

func (s *Server) operationDTOs(ctx context.Context, ops []db.Operation) []operationDTO {
	var mIDs, uIDs, cIDs, mainOperationIDs, creatorIDs, opIDs []int64
	for _, o := range ops {
		mIDs = append(mIDs, o.MissionID)
		opIDs = append(opIDs, o.ID)
		if o.MainOperationID.Valid {
			mainOperationIDs = append(mainOperationIDs, o.MainOperationID.Int64)
		}
		if o.CreatedBy.Valid {
			creatorIDs = append(creatorIDs, o.CreatedBy.Int64)
		}
		if o.AssigneeID.Valid && o.AssigneeType.Valid {
			switch o.AssigneeType.String {
			case "user":
				uIDs = append(uIDs, o.AssigneeID.Int64)
			case "crew":
				cIDs = append(cIDs, o.AssigneeID.Int64)
			}
		}
	}
	mMap := s.mapMissions(ctx, mIDs)
	uMap := s.mapUsers(ctx, uIDs)
	cMap := s.mapCrews(ctx, cIDs)
	mainOperationMap := s.mapOperations(ctx, mainOperationIDs)
	creatorMap := s.mapUsers(ctx, creatorIDs)
	labelMap := s.labelsForOperations(ctx, opIDs)
	subOperationProgressMap := s.subOperationProgress(ctx, opIDs)
	activeRunStateMap := s.activeRunStates(ctx, opIDs)
	out := make([]operationDTO, 0, len(ops))
	for _, o := range ops {
		d := operationDTO{
			ID: uuidStr(o.PublicID), Title: o.Title, Body: o.Body, Status: o.Status, ActiveRunState: activeRunStateMap[o.ID],
			MissionID: mMap[o.MissionID], Sequence: o.Sequence, Priority: o.Priority, Orchestrating: o.Orchestrating, Archived: o.Archived, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, StartedAt: o.StartedAt, FinishedAt: o.FinishedAt,
			RequiredTags: o.RequiredTags, ExcludedTags: o.ExcludedTags,
			Labels: labelMap[o.ID], SubOperationProgress: subOperationProgressMap[o.ID],
			StartDate: dateStr(o.StartDate), DueDate: dateStr(o.DueDate),
		}
		if d.Labels == nil {
			d.Labels = []labelDTO{}
		}
		d.Reactions = []reactionDTO{} // populated only in the detail view
		if o.AssigneeType.Valid {
			d.AssigneeType = strPtr(o.AssigneeType.String)
			d.AssigneeID = strPtr(polyUUID(o.AssigneeType.String, o.AssigneeID, uMap, cMap))
		}
		if o.AssigneePilotKind.Valid {
			d.AssigneePilotKind = strPtr(o.AssigneePilotKind.String)
		}
		if o.MainOperationID.Valid {
			d.MainOperationID = strPtr(mainOperationMap[o.MainOperationID.Int64])
		}
		if o.CreatedBy.Valid {
			d.CreatedBy = strPtr(creatorMap[o.CreatedBy.Int64])
		}
		out = append(out, d)
	}
	return out
}

func (s *Server) labelsForOperations(ctx context.Context, ids []int64) map[int64][]labelDTO {
	out := map[int64][]labelDTO{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.LabelsForOperations(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.OperationID] = append(out[r.OperationID], labelDTO{ID: uuidStr(r.PublicID), Name: r.Name, Color: r.Color})
	}
	return out
}

func (s *Server) subOperationProgress(ctx context.Context, ids []int64) map[int64]subOperationProgress {
	out := map[int64]subOperationProgress{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.SubOperationProgress(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.MainOperationID.Valid {
			out[r.MainOperationID.Int64] = subOperationProgress{Total: r.Total, Done: r.Done}
		}
	}
	return out
}

func (s *Server) activeRunStates(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.ActiveRunStatesForOperations(ctx, ids)
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.OperationID] = r.State
	}
	return out
}

func (s *Server) operationDTO(ctx context.Context, o db.Operation) operationDTO {
	return s.operationDTOs(ctx, []db.Operation{o})[0]
}

func (s *Server) commentDTOs(ctx context.Context, cs []db.Comment, userID int64) []commentDTO {
	var uIDs, cIDs []int64
	for _, c := range cs {
		cIDs = append(cIDs, c.ID)
		if c.AuthorID.Valid && c.AuthorType == "user" {
			uIDs = append(uIDs, c.AuthorID.Int64)
		}
	}
	uMap := s.mapUsers(ctx, uIDs)
	reMap := s.reactionsForTargets(ctx, "comment", cIDs, userID)
	out := make([]commentDTO, 0, len(cs))
	for _, c := range cs {
		d := commentDTO{ID: uuidStr(c.PublicID), AuthorType: c.AuthorType, Body: c.Body, CreatedAt: c.CreatedAt, Reactions: reMap[c.ID]}
		if d.Reactions == nil {
			d.Reactions = []reactionDTO{}
		}
		d.AuthorID = strPtr(polyUUID(c.AuthorType, c.AuthorID, uMap, nil))
		if c.AuthorPilotKind.Valid {
			d.AuthorPilotKind = strPtr(c.AuthorPilotKind.String)
		}
		out = append(out, d)
	}
	return out
}

// reactionsForTargets batch-loads reactions for a set of targets of one type
// ("operation"|"comment") → map[targetID][]reactionDTO. One query for either kind.
func (s *Server) reactionsForTargets(ctx context.Context, targetType string, ids []int64, userID int64) map[int64][]reactionDTO {
	out := map[int64][]reactionDTO{}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return out
	}
	rows, err := s.q.ReactionsForTargets(ctx, db.ReactionsForTargetsParams{TargetType: targetType, Column2: ids, UserID: userID})
	if err != nil {
		return out
	}
	for _, r := range rows {
		out[r.TargetID] = append(out[r.TargetID], reactionDTO{Emoji: r.Emoji, Count: r.N, Mine: r.Mine, Users: r.Users})
	}
	return out
}

func (s *Server) runDTOs(ctx context.Context, rs []db.Run) []runDTO {
	var opIDs []int64
	for _, r := range rs {
		opIDs = append(opIDs, r.OperationID)
	}
	opMap := s.mapOperations(ctx, opIDs)
	out := make([]runDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, runDTO{
			ID: uuidStr(r.PublicID), OperationID: opMap[r.OperationID], State: r.State,
			Pilot: r.Pilot, NeedsInput: r.NeedsInput, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return out
}

func (s *Server) crewMemberDTOs(ctx context.Context, ms []db.CrewMember) []crewMemberDTO {
	var uIDs []int64
	for _, m := range ms {
		if m.MemberType == "user" && m.UserID.Valid {
			uIDs = append(uIDs, m.UserID.Int64)
		}
	}
	uMap := s.mapUsers(ctx, uIDs)
	out := make([]crewMemberDTO, 0, len(ms))
	for _, m := range ms {
		ref := m.PilotKind.String // pilot member: the kind is the ref
		if m.MemberType == "user" {
			ref = polyUUID("user", m.UserID, uMap, nil)
		}
		out = append(out, crewMemberDTO{MemberType: m.MemberType, MemberID: ref, Role: m.Role})
	}
	return out
}

func (s *Server) signalDTOs(ctx context.Context, ss []db.Signal) []signalDTO {
	var opIDs []int64
	for _, sg := range ss {
		if sg.OperationID.Valid {
			opIDs = append(opIDs, sg.OperationID.Int64)
		}
	}
	opMap := s.mapOperations(ctx, opIDs)
	out := make([]signalDTO, 0, len(ss))
	for _, sg := range ss {
		d := signalDTO{
			ID: uuidStr(sg.PublicID), Type: sg.Type, Severity: sg.Severity,
			Title: sg.Title, Body: sg.Body, Read: sg.Read, CreatedAt: sg.CreatedAt,
		}
		if sg.OperationID.Valid {
			d.OperationID = strPtr(opMap[sg.OperationID.Int64])
		}
		out = append(out, d)
	}
	return out
}
