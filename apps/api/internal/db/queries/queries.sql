-- ============================ auth ============================

-- name: CreateUser :one
INSERT INTO users (email, password_hash, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: SetUserPasswordHash :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- Resolve a session cookie to its user (only if unexpired).
-- name: GetSessionUser :one
SELECT u.* FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- ========================== tenancy ==========================

-- name: CreateFleet :one
INSERT INTO fleets (name, kind)
VALUES ($1, $2)
RETURNING *;

-- Resolve a fleet's public id to its internal id, asserting membership.
-- name: ResolveFleetForMember :one
SELECT f.id FROM fleets f
JOIN memberships m ON m.fleet_id = f.id
WHERE f.public_id = $1 AND m.user_id = $2;

-- name: GetFleetByPublicID :one
SELECT * FROM fleets WHERE public_id = $1;

-- name: GetFleetByID :one
SELECT * FROM fleets WHERE id = $1;

-- name: GetFleetKind :one
SELECT kind FROM fleets WHERE id = $1;

-- name: UpdateFleetName :one
UPDATE fleets SET name = $2 WHERE id = $1 RETURNING *;

-- name: DeleteFleet :exec
DELETE FROM fleets WHERE id = $1;

-- name: GetUserIDByPublicID :one
SELECT id FROM users WHERE public_id = $1;

-- name: GetMemberUserIDByPublicID :one
SELECT u.id FROM users u
JOIN memberships m ON m.user_id = u.id
WHERE u.public_id = $1 AND m.fleet_id = $2;

-- name: CreateMembership :exec
INSERT INTO memberships (user_id, fleet_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, fleet_id) DO NOTHING;

-- name: ListFleetsForUser :many
SELECT w.* FROM fleets w
JOIN memberships m ON m.fleet_id = w.id
WHERE m.user_id = $1
ORDER BY w.id;

-- name: IsMember :one
SELECT EXISTS(
    SELECT 1 FROM memberships WHERE user_id = $1 AND fleet_id = $2
);

-- name: ListFleetMemberIDs :many
SELECT user_id FROM memberships WHERE fleet_id = $1;

-- name: GetMemberRole :one
SELECT role FROM memberships WHERE user_id = $1 AND fleet_id = $2;

-- name: ListMembers :many
SELECT u.public_id AS id, u.email, u.name, m.role
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.fleet_id = $1
ORDER BY m.created_at;

-- name: CountFleetOwners :one
SELECT COUNT(*) FROM memberships WHERE fleet_id = $1 AND role = 'owner';

-- name: UpdateMemberRole :execrows
UPDATE memberships SET role = $3 WHERE user_id = $1 AND fleet_id = $2;

-- name: LockFleet :exec
SELECT id FROM fleets WHERE id = $1 FOR UPDATE;

-- name: RemoveMember :execrows
DELETE FROM memberships WHERE user_id = $1 AND fleet_id = $2;

-- ===================== invitations ===========================

-- name: CreateInvitation :one
INSERT INTO invitations (fleet_id, inviter_id, invitee_email, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListInvitations :many
SELECT * FROM invitations WHERE fleet_id = $1 AND status = 'pending' ORDER BY id DESC;

-- Pending invitations addressed to an email (across fleets), with fleet name.
-- name: InvitationsForEmail :many
SELECT i.*, f.name AS fleet_name, f.public_id AS fleet_public_id
FROM invitations i JOIN fleets f ON f.id = i.fleet_id
WHERE i.invitee_email = $1 AND i.status = 'pending' AND i.expires_at > now()
ORDER BY i.id DESC;

-- name: GetInvitation :one
SELECT * FROM invitations WHERE id = $1;

-- name: GetInvitationByPublicID :one
SELECT * FROM invitations WHERE public_id = $1;

-- name: SetInvitationStatus :exec
UPDATE invitations SET status = $2 WHERE id = $1;

-- Fleets whose rovers just crossed the offline threshold (so the sweeper can push
-- a presence update — absence of heartbeat isn't itself an event).
-- name: FleetsWithNewlyOfflineRovers :many
SELECT DISTINCT fleet_id FROM rovers
WHERE last_seen_at IS NOT NULL
  AND last_seen_at <  now() - make_interval(secs => $1::float8)
  AND last_seen_at >= now() - make_interval(secs => $2::float8);

-- name: NotifyFleetChanged :exec
SELECT pg_notify('ufo_changed', json_build_object('t', 'rover', 'fleet', $1::bigint)::text);

-- ---- enrollment codes (enrollment) ----

-- name: CreateEnrollmentCode :one
INSERT INTO enrollment_codes (fleet_id, code_hash, name, remaining_uses, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListEnrollmentCodes :many
SELECT * FROM enrollment_codes WHERE fleet_id = $1 ORDER BY id DESC;

-- name: GetEnrollmentCodeForUpdate :one
SELECT * FROM enrollment_codes WHERE code_hash = $1 FOR UPDATE;

-- name: DeleteEnrollmentCode :exec
DELETE FROM enrollment_codes WHERE id = $1 AND fleet_id = $2;

-- name: DecrementEnrollmentCodeUses :exec
UPDATE enrollment_codes SET remaining_uses = remaining_uses - 1 WHERE id = $1 AND fleet_id = $2;

-- ---- rovers (per-rover identity + connection token) ----

-- name: CreateRover :one
INSERT INTO rovers (fleet_id, name, enrollment_code_id, token_hash, tags, auto_tags)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: SetRoverTags :exec
UPDATE rovers SET tags = $3 WHERE id = $1 AND fleet_id = $2;

-- name: SetRoverName :exec
UPDATE rovers SET name = $3 WHERE id = $1 AND fleet_id = $2;

-- name: SetRoverUnits :exec
UPDATE rovers SET units = $2 WHERE id = $1;

-- name: SetRoverAutoTags :exec
UPDATE rovers SET auto_tags = $2 WHERE id = $1;

-- name: GetRoverByTokenHash :one
SELECT * FROM rovers WHERE token_hash = $1;

-- name: TouchRover :exec
UPDATE rovers SET last_seen_at = now() WHERE id = $1;

-- name: DeleteRover :exec
DELETE FROM rovers WHERE id = $1 AND fleet_id = $2;

-- List rovers with active run count.
-- name: ListRoversWithStatus :many
SELECT r.*,
       (
           SELECT COUNT(*)::bigint FROM runs x
           WHERE x.rover_id = r.id AND x.state IN ('claimed', 'starting', 'running')
       ) AS busy_units
FROM rovers r
WHERE r.fleet_id = $1
ORDER BY r.id;

-- ========================== operations ===========================

-- name: CreateOperation :one
INSERT INTO operations (fleet_id, title, body, mission_id, assignee_type, assignee_id, status, sequence, required_tags, excluded_tags, priority, main_operation_id, start_date, due_date, created_by, assignee_pilot_kind, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, CASE WHEN $7 = 'in_progress' THEN now() ELSE NULL END)
RETURNING *;

-- name: UpdateOperationTags :exec
UPDATE operations SET required_tags = $3, excluded_tags = $4, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: SetOperationPriority :exec
UPDATE operations SET priority = $3, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: SetOperationDates :exec
UPDATE operations SET start_date = $3, due_date = $4, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: SetMainOperation :exec
UPDATE operations SET main_operation_id = $3, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: TouchOperation :exec
UPDATE operations SET updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: ListSubOperations :many
SELECT * FROM operations WHERE main_operation_id = $1 ORDER BY id;

-- name: SetOperationOrchestrating :exec
UPDATE operations SET orchestrating = $3, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- name: CountActiveOrUnsettledSubOperations :one
SELECT COUNT(*)::bigint FROM operations o
WHERE o.main_operation_id = $1
  AND (o.status IN ('backlog', 'todo', 'in_progress')
       OR EXISTS (SELECT 1 FROM runs r WHERE r.operation_id = o.id AND r.state IN ('queued','claimed','starting','running')));

-- name: LatestDiffForOperation :one
SELECT a.content FROM artifacts a JOIN runs r ON a.run_id = r.id
WHERE r.operation_id = $1 AND a.kind = 'diff' ORDER BY a.id DESC LIMIT 1;

-- name: OperationHasActiveRun :one
SELECT EXISTS(SELECT 1 FROM runs WHERE operation_id = $1 AND state IN ('queued','claimed','starting','running'));

-- Active run state per operation, batched for board/detail DTOs.
-- name: ActiveRunStatesForOperations :many
SELECT operation_id, state FROM runs
WHERE operation_id = ANY($1::bigint[]) AND state IN ('queued','claimed','starting','running');

-- Active run counts split by queue/work state.
-- name: CountActiveRunsByState :many
SELECT state, COUNT(DISTINCT operation_id)::bigint AS n FROM runs
WHERE fleet_id = $1 AND state IN ('queued','claimed','starting','running')
GROUP BY state;

-- Sub-operation progress per main operation (total + done), batched for the board.
-- name: SubOperationProgress :many
SELECT main_operation_id, COUNT(*)::bigint AS total, COUNT(*) FILTER (WHERE status = 'done')::bigint AS done
FROM operations WHERE main_operation_id = ANY($1::bigint[]) GROUP BY main_operation_id;

-- name: GetOperation :one
SELECT * FROM operations WHERE id = $1 AND fleet_id = $2;

-- name: ListOperations :many
SELECT * FROM operations WHERE fleet_id = $1 ORDER BY id DESC;

-- Board: one status column, keyset-paginated. mission = 0 → all missions;
-- before = 0 → first page (newest). Index: operations_board_idx.
-- Board column, keyset-paginated, with optional filters (0/'' = unset). $6 priority
-- (-1=any), $7 assignee_kind (''|user|pilot|crew), $8 assignee_id, $9 creator, $10 label.
-- name: ListOperationsByStatus :many
SELECT * FROM operations
WHERE fleet_id = $1 AND status = $2
  AND ($3::bigint = 0 OR mission_id = $3)
  AND ($4::bigint = 0 OR id < $4)
  AND ($6::smallint = -1 OR priority = $6)
  AND ($7::text = '' OR assignee_type = $7)
  AND ($8::bigint = 0 OR assignee_id = $8)
  AND ($9::bigint = 0 OR created_by = $9)
  AND ($10::bigint = 0 OR EXISTS(SELECT 1 FROM operation_labels ol WHERE ol.operation_id = operations.id AND ol.label_id = $10))
  AND ($11::bool OR archived = FALSE)
  AND ($12::text = '' OR assignee_pilot_kind = $12)
ORDER BY id DESC
LIMIT $5;

-- Board column counts (optionally scoped to one mission). mission = 0 → all.
-- name: CountOperationsByStatus :many
SELECT status, COUNT(*)::bigint AS n FROM operations
WHERE fleet_id = $1 AND ($2::bigint = 0 OR mission_id = $2)
  AND ($3::smallint = -1 OR priority = $3)
  AND ($4::text = '' OR assignee_type = $4)
  AND ($5::bigint = 0 OR assignee_id = $5)
  AND ($6::bigint = 0 OR created_by = $6)
  AND ($7::bigint = 0 OR EXISTS(SELECT 1 FROM operation_labels ol WHERE ol.operation_id = operations.id AND ol.label_id = $7))
  AND ($8::bool OR archived = FALSE)
  AND ($9::text = '' OR assignee_pilot_kind = $9)
GROUP BY status;

-- Per-mission operation counts (for the Missions view), keyed by mission public id.
-- name: CountOperationsByMission :many
SELECT m.public_id AS mission_id, COUNT(*)::bigint AS n
FROM operations o JOIN missions m ON m.id = o.mission_id
WHERE o.fleet_id = $1
GROUP BY m.public_id;

-- name: AssignOperation :one
UPDATE operations SET assignee_type = $3, assignee_id = $4, assignee_pilot_kind = $5, updated_at = now()
WHERE id = $1 AND fleet_id = $2
RETURNING *;

-- name: SetOperationStatus :exec
UPDATE operations
SET status = $3,
    started_at = CASE WHEN $3 = 'in_progress' AND started_at IS NULL THEN now() ELSE started_at END,
    finished_at = CASE
        WHEN $3 IN ('done', 'cancelled') THEN coalesce(finished_at, now())
        WHEN $3 NOT IN ('done', 'cancelled') THEN NULL
        ELSE finished_at
    END,
    updated_at = now()
WHERE id = $1 AND fleet_id = $2;

-- ========================= pilots ============================

-- Pilot kinds and the fleet rovers each can drive, derived from rovers' pilot:* tags. $2 = the
-- online window (seconds); a kind is online if any rover advertising it is fresh.
-- name: FleetPilotCapabilities :many
SELECT substr(t, 7)::text AS kind,
       COUNT(*)::bigint AS rovers,
       coalesce(bool_or(now() - last_seen_at < make_interval(secs => $2::float8)), FALSE)::bool AS online
FROM rovers, unnest(tags || auto_tags) AS t
WHERE fleet_id = $1 AND t like 'pilot:%'
GROUP BY 1
ORDER BY 1;

-- name: FleetPilotKindFree :many
-- Per pilot kind in the fleet, whether any capable rover is online AND idle.
-- Only kinds with >=1 capable rover appear (presence => hasRover).
SELECT substr(t, 7)::text AS kind,
       coalesce(bool_or(
         now() - r.last_seen_at < make_interval(secs => $2::float8)
         AND NOT EXISTS (SELECT 1 FROM runs x
                         WHERE x.rover_id = r.id AND x.state IN ('claimed','starting','running'))
       ), FALSE)::bool AS has_free
FROM rovers r, unnest(r.tags || r.auto_tags) AS t
WHERE r.fleet_id = $1 AND t LIKE 'pilot:%'
GROUP BY 1;

-- name: FailedPilotKindsForOperation :many
SELECT DISTINCT pilot FROM runs WHERE operation_id = $1 AND state IN ('blocked','failed');

-- ========================== crews ============================

-- name: CreateCrew :one
INSERT INTO crews (fleet_id, name) VALUES ($1, $2) RETURNING *;

-- name: ListCrews :many
SELECT * FROM crews WHERE fleet_id = $1 ORDER BY id;

-- name: GetCrew :one
SELECT * FROM crews WHERE id = $1 AND fleet_id = $2;

-- name: SetCrewName :exec
UPDATE crews SET name = $3 WHERE id = $1 AND fleet_id = $2;

-- name: DeleteCrew :exec
DELETE FROM crews WHERE id = $1 AND fleet_id = $2;

-- name: AddCrewUser :exec
INSERT INTO crew_members (crew_id, member_type, user_id, role)
VALUES ($1, 'user', $2, $3)
ON CONFLICT (crew_id, user_id) WHERE user_id IS NOT NULL DO UPDATE SET role = excluded.role;

-- name: AddCrewPilot :exec
INSERT INTO crew_members (crew_id, member_type, pilot_kind, role)
VALUES ($1, 'pilot', $2, $3)
ON CONFLICT (crew_id, pilot_kind) WHERE pilot_kind IS NOT NULL DO UPDATE SET role = excluded.role;

-- name: RemoveCrewUser :exec
DELETE FROM crew_members WHERE crew_id = $1 AND member_type = 'user' AND user_id = $2;

-- name: RemoveCrewPilot :exec
DELETE FROM crew_members WHERE crew_id = $1 AND member_type = 'pilot' AND pilot_kind = $2;

-- name: DemoteCrewCaptains :exec
UPDATE crew_members SET role = 'member' WHERE crew_id = $1 AND role = 'captain';

-- name: ListCrewMembers :many
SELECT * FROM crew_members WHERE crew_id = $1;

-- ========================= comments ==========================

-- name: CreateComment :one
INSERT INTO comments (operation_id, author_type, author_id, body, author_pilot_kind)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListComments :many
SELECT * FROM comments WHERE operation_id = $1 ORDER BY id;

-- ========================== signals ==========================

-- name: CreateSignal :one
INSERT INTO signals (fleet_id, recipient_user_id, operation_id, type, severity, title, body)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListSignals :many
SELECT * FROM signals
WHERE fleet_id = $1 AND recipient_user_id = $2 AND archived = FALSE
ORDER BY read, id DESC;

-- name: MarkSignalRead :exec
UPDATE signals SET read = TRUE
WHERE id = $1 AND fleet_id = $2 AND recipient_user_id = $3;

-- name: ArchiveSignal :exec
UPDATE signals SET archived = TRUE, read = TRUE
WHERE id = $1 AND fleet_id = $2 AND recipient_user_id = $3;

-- Self-heal: archive open action-required signals once an operation leaves that state.
-- name: ArchiveActionRequiredForOperation :exec
UPDATE signals SET archived = TRUE
WHERE operation_id = $1 AND severity = 'action_required' AND archived = FALSE;

-- ========================= missions ==========================
-- A mission is a user-created objective: a grouping of operations within a
-- fleet. Its key prefixes operation codes; runs execute in per-operation
-- isolated directories managed by the rover.

-- name: CreateMission :one
INSERT INTO missions (fleet_id, name, key)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateMission :one
UPDATE missions SET name = $3, key = $4
WHERE id = $1 AND fleet_id = $2
RETURNING *;

-- Atomically allocate the next per-mission operation number.
-- name: BumpMissionSequence :one
UPDATE missions SET next_sequence = next_sequence + 1
WHERE id = $1 AND fleet_id = $2
RETURNING next_sequence;

-- name: ListMissions :many
SELECT * FROM missions WHERE fleet_id = $1 ORDER BY id;

-- name: GetMission :one
SELECT * FROM missions WHERE id = $1;

-- =========================== runs ============================

-- name: CreateRun :one
INSERT INTO runs (fleet_id, operation_id, mission_id, command, pilot, session_id, required_rover_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SetRunSession :exec
UPDATE runs SET session_id = $2 WHERE id = $1 AND fleet_id = $3;

-- name: SetRunNeedsInput :exec
UPDATE runs SET needs_input = TRUE WHERE id = $1 AND fleet_id = $2;

-- name: SetRunRequestedStatus :exec
UPDATE runs SET requested_status = $3 WHERE id = $1 AND fleet_id = $2;

-- name: SetOperationSession :exec
UPDATE operations SET pilot_session_id = $2, pilot_session_kind = $3, pilot_session_rover_id = $5 WHERE id = $1 AND fleet_id = $4;

-- name: RoverLastSeen :one
SELECT last_seen_at FROM rovers WHERE id = $1;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1 AND fleet_id = $2;

-- name: ListRuns :many
SELECT * FROM runs WHERE fleet_id = $1 ORDER BY id DESC;

-- name: ListRunsByOperation :many
SELECT * FROM runs WHERE operation_id = $1 ORDER BY id DESC;

-- Atomically grab the oldest queued run in a fleet and attribute it to the
-- claiming rover.
-- Claim the oldest queued run the rover is allowed and able to run: the rover
-- must advertise the run's pilot kind, the operation deny list must not overlap
-- its tags (checked first), and its allow list must be a subset.
-- $3 = the rover's tag union (tags || auto_tags).
-- name: ClaimNextRun :one
UPDATE runs
SET state = 'claimed', updated_at = now(), heartbeat_at = now(), rover_id = $2
WHERE id = (
    SELECT r.id FROM runs r
    JOIN operations o ON o.id = r.operation_id
    WHERE r.state = 'queued' AND r.fleet_id = $1
      AND (r.required_rover_id IS NULL OR r.required_rover_id = $2) -- session affinity pin
      AND ('pilot:' || r.pilot) = ANY($3::text[]) -- pilot capability tag
      AND NOT (o.excluded_tags && $3::text[])      -- deny boundary
      AND o.required_tags <@ $3::text[]            -- allow list
    ORDER BY r.id
    FOR UPDATE of r skip locked
    LIMIT 1
)
RETURNING *;

-- name: SetRunState :one
UPDATE runs
SET state = $2, updated_at = now()
WHERE id = $1 AND fleet_id = $3 AND state IN ('claimed', 'starting', 'running')
RETURNING *;

-- name: CancelRun :one
UPDATE runs
SET state = 'canceled', updated_at = now()
WHERE id = $1 AND fleet_id = $2 AND state IN ('queued', 'claimed', 'starting', 'running')
RETURNING *;

-- name: Heartbeat :one
UPDATE runs SET heartbeat_at = now()
WHERE id = $1 AND fleet_id = $2 AND state IN ('claimed', 'starting', 'running')
RETURNING id;

-- Requeue runs whose rover went silent (heartbeat older than the lease).
-- name: RequeueExpiredRuns :many
UPDATE runs
SET state = 'queued', heartbeat_at = NULL, rover_id = NULL, updated_at = now()
WHERE state IN ('claimed', 'starting', 'running')
  AND heartbeat_at IS NOT NULL
  AND heartbeat_at < now() - make_interval(secs => $1::float8)
RETURNING id;

-- ===================== events & artifacts ====================

-- name: AppendRunEvent :one
INSERT INTO run_events (run_id, kind, message)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListRunEvents :many
SELECT * FROM run_events WHERE run_id = $1 ORDER BY id;

-- name: AppendArtifact :one
INSERT INTO artifacts (run_id, kind, name, content)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListRunArtifacts :many
SELECT * FROM artifacts WHERE run_id = $1 ORDER BY id;

-- ===================== transcript (run messages) =============

-- name: AppendRunMessage :one
INSERT INTO run_messages (run_id, sequence, type, tool, content, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListRunMessages :many
SELECT * FROM run_messages WHERE run_id = $1 ORDER BY sequence, id;

-- ================ public-id resolvers (public id -> internal id) ================
-- Each resolves a public id (from a URL path or request body) to the internal
-- bigint, scoped to the fleet so cross-tenant ids can't be addressed.

-- name: GetOperationIDByPublicID :one
SELECT id FROM operations WHERE public_id = $1 AND fleet_id = $2;

-- name: GetRunIDByPublicID :one
SELECT id FROM runs WHERE public_id = $1 AND fleet_id = $2;

-- name: GetRunIDForRover :one
-- Resolve a run owned by the calling rover (claimed by it), so one rover can't
-- mutate another rover's run.
SELECT id FROM runs WHERE public_id = $1 AND fleet_id = $2 AND rover_id = $3;

-- name: GetCrewIDByPublicID :one
SELECT id FROM crews WHERE public_id = $1 AND fleet_id = $2;

-- name: GetMissionIDByPublicID :one
SELECT id FROM missions WHERE public_id = $1 AND fleet_id = $2;

-- name: GetRoverIDByPublicID :one
SELECT id FROM rovers WHERE public_id = $1 AND fleet_id = $2;

-- name: GetEnrollmentCodeIDByPublicID :one
SELECT id FROM enrollment_codes WHERE public_id = $1 AND fleet_id = $2;

-- name: GetSignalIDByPublicID :one
SELECT id FROM signals WHERE public_id = $1 AND fleet_id = $2;

-- ============ batch id -> public_id maps (API response reference expansion) ==========
-- Batch-resolve internal ids for API response reference expansion.

-- name: PublicIDsForUsers :many
SELECT id, public_id FROM users WHERE id = ANY($1::bigint[]);

-- name: PublicIDsForCrews :many
SELECT id, public_id FROM crews WHERE id = ANY($1::bigint[]);

-- name: PublicIDsForMissions :many
SELECT id, public_id FROM missions WHERE id = ANY($1::bigint[]);

-- name: PublicIDsForOperations :many
SELECT id, public_id FROM operations WHERE id = ANY($1::bigint[]);

-- ============================ labels =============================

-- name: CreateLabel :one
INSERT INTO labels (fleet_id, name, color) VALUES ($1, $2, $3) RETURNING *;

-- name: ListLabels :many
SELECT * FROM labels WHERE fleet_id = $1 ORDER BY name;

-- name: GetLabelIDByPublicID :one
SELECT id FROM labels WHERE public_id = $1 AND fleet_id = $2;

-- name: DeleteLabel :exec
DELETE FROM labels WHERE id = $1 AND fleet_id = $2;

-- name: AddOperationLabel :exec
INSERT INTO operation_labels (operation_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: RemoveOperationLabel :exec
DELETE FROM operation_labels WHERE operation_id = $1 AND label_id = $2;

-- Labels for a set of operations.
-- name: LabelsForOperations :many
SELECT ol.operation_id, l.public_id, l.name, l.color
FROM operation_labels ol JOIN labels l ON l.id = ol.label_id
WHERE ol.operation_id = ANY($1::bigint[])
ORDER BY l.name;

-- ========================= pull requests =========================

-- name: CreatePullRequest :one
INSERT INTO pull_requests (operation_id, url, title, number) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListPullRequestsForOperation :many
SELECT * FROM pull_requests WHERE operation_id = $1 ORDER BY id;

-- name: DeletePullRequest :exec
DELETE FROM pull_requests p USING operations o
WHERE p.public_id = $1 AND p.operation_id = o.id AND o.fleet_id = $2;

-- ====================== operation relations ========================

-- name: CreateRelation :one
INSERT INTO operation_relations (fleet_id, source_id, target_id, kind)
VALUES ($1, $2, $3, $4)
ON CONFLICT (source_id, target_id, kind) DO UPDATE SET kind = excluded.kind
RETURNING *;

-- name: DeleteRelation :exec
DELETE FROM operation_relations WHERE public_id = $1 AND fleet_id = $2;

-- Both directions for one operation, joined to the *other* operation. `outgoing`
-- = the queried operation is the source (so the row's kind applies as-is; otherwise inverse).
-- name: ListRelationsForOperation :many
SELECT r.public_id AS relation_id, r.kind, (r.source_id = $1) AS outgoing,
       o.public_id AS operation_public_id, o.title, o.status, o.sequence, m.public_id AS mission_id
FROM operation_relations r
JOIN operations o ON o.id = CASE WHEN r.source_id = $1 THEN r.target_id ELSE r.source_id END
JOIN missions m ON m.id = o.mission_id
WHERE r.source_id = $1 OR r.target_id = $1
ORDER BY r.id;

-- Typeahead for linking operations: match title or numeric sequence, newest first.
-- name: SearchOperations :many
SELECT o.*, m.public_id AS mission_public_id, m.key AS mission_key
FROM operations o JOIN missions m ON m.id = o.mission_id
WHERE o.fleet_id = $1
  AND (o.title ILIKE '%' || $2 || '%' OR cast(o.sequence AS text) = $2)
ORDER BY o.id DESC
LIMIT 20;

-- name: SetOperationArchived :exec
UPDATE operations SET archived = $3, updated_at = now() WHERE id = $1 AND fleet_id = $2;

-- ====================== reactions ========================

-- name: GetCommentIDByPublicID :one
SELECT c.id FROM comments c JOIN operations o ON o.id = c.operation_id
WHERE c.public_id = $1 AND o.fleet_id = $2;

-- One generic reaction API over (target_type, target_id) — serves operations + comments.

-- name: ReactionExists :one
SELECT EXISTS(SELECT 1 FROM reactions WHERE target_type = $1 AND target_id = $2 AND user_id = $3 AND emoji = $4);

-- name: AddReaction :exec
INSERT INTO reactions (target_type, target_id, user_id, emoji) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING;

-- name: RemoveReaction :exec
DELETE FROM reactions WHERE target_type = $1 AND target_id = $2 AND user_id = $3 AND emoji = $4;

-- Reactions for a set of targets of one type: count, whether the caller ($3) reacted, and
-- reactors (oldest first, for the hover tooltip). Emoji groups ordered by first use.
-- name: ReactionsForTargets :many
SELECT r.target_id, r.emoji, COUNT(*)::bigint AS n, bool_or(r.user_id = $3) AS mine,
       array_agg(coalesce(nullif(u.name, ''), u.email) ORDER BY r.created_at)::text[] AS users
FROM reactions r JOIN users u ON u.id = r.user_id
WHERE r.target_type = $1 AND r.target_id = ANY($2::bigint[])
GROUP BY r.target_id, r.emoji
ORDER BY min(r.created_at);
