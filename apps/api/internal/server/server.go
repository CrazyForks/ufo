// Package server holds the UFO Hub HTTP handlers: accounts/auth, the
// tenant (fleet) surface for the web board, and the rover surface (claim/
// state/events/artifacts/missions) authenticated by per-rover connection tokens.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"ufo/apps/api/internal/auth"
	"ufo/apps/api/internal/db"
	"ufo/apps/api/internal/spec"
)

const (
	sessionCookie = "ufo_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type testHooks struct {
	afterEnrollmentCodeLocked func()
	afterRoleFleetLocked      func()
}

var serverTestHooks atomic.Value

func runTestHook(selectHook func(testHooks) func()) {
	h, _ := serverTestHooks.Load().(testHooks)
	if hook := selectHook(h); hook != nil {
		hook()
	}
}

type ctxKey int

const (
	userKey ctxKey = iota
	roverKey
)

// Server wires the pgx pool, generated queries, long-poll duration, and notifier.
type Server struct {
	pool     *pgxpool.Pool
	q        *db.Queries
	longPoll time.Duration
	notifier *Notifier
	hub      *wsHub

	secureCookies    bool     // UFO_HUB_SECURE_COOKIES: mark the session cookie Secure (HTTPS)
	allowedOrigins   []string // UFO_HUB_ORIGINS: CORS + WebSocket cross-origin allowlist
	maxSubOperations int      // UFO_HUB_MAX_SUB_OPERATIONS: cap on a captain's single split
}

func New(pool *pgxpool.Pool, longPoll time.Duration, notifier *Notifier) *Server {
	return &Server{
		pool: pool, q: db.New(pool), longPoll: longPoll, notifier: notifier, hub: newWSHub(),
		secureCookies:    envBool("UFO_HUB_SECURE_COOKIES"),
		allowedOrigins:   splitOrigins(os.Getenv("UFO_HUB_ORIGINS")),
		maxSubOperations: envInt("UFO_HUB_MAX_SUB_OPERATIONS", 8),
	}
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitOrigins(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// originAllowed gates browser cross-origin requests: a missing Origin (curl, the
// rover) has no CSRF surface; otherwise it must be in the allowlist, or same-origin
// when no allowlist is set.
func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if origin == "" {
		return true
	}
	for _, o := range s.allowedOrigins {
		if origin == o {
			return true
		}
	}
	if len(s.allowedOrigins) == 0 {
		if u, err := url.Parse(origin); err == nil && u.Host == r.Host {
			return true
		}
	}
	return false
}

// StartHub runs the WebSocket fan-out loop (typed change events per fleet).
func (s *Server) StartHub(ctx context.Context) { go s.hub.run(ctx, s.notifier) }

// Handler returns the routed, CORS-wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.discovery)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /openapi.yaml", s.serveOpenAPI)
	mux.HandleFunc("GET /.well-known/api-catalog", s.apiCatalog)
	api := http.NewServeMux()
	mux.Handle("/v1/", http.StripPrefix("/v1", api))

	// Auth (public).
	api.HandleFunc("POST /auth/signup", s.signup)
	api.HandleFunc("POST /auth/login", s.login)
	api.HandleFunc("POST /auth/logout", s.logout)

	// UI surface (requires a session; fleet via ?fleet=).
	api.HandleFunc("GET /me", s.requireUser(s.me))
	api.HandleFunc("GET /fleets", s.requireUser(s.listFleets))
	api.HandleFunc("POST /fleets", s.requireUser(s.createFleet))
	api.HandleFunc("PATCH /fleets/{id}", s.requireUser(s.updateFleet))
	api.HandleFunc("DELETE /fleets/{id}", s.requireUser(s.deleteFleet))
	api.HandleFunc("GET /rovers", s.requireUser(s.listRovers))
	api.HandleFunc("PATCH /rovers/{id}", s.requireUser(s.patchRover))
	api.HandleFunc("DELETE /rovers/{id}", s.requireUser(s.deleteRover))
	api.HandleFunc("GET /enrollment-codes", s.requireUser(s.listEnrollmentCodes))
	api.HandleFunc("POST /enrollment-codes", s.requireUser(s.createEnrollmentCode))
	api.HandleFunc("DELETE /enrollment-codes/{id}", s.requireUser(s.deleteEnrollmentCode))
	api.HandleFunc("POST /operations", s.requireUser(s.createOperation))
	api.HandleFunc("GET /operations", s.requireUser(s.listOperations))
	api.HandleFunc("GET /operations/{id}", s.requireUser(s.getOperation))
	api.HandleFunc("PATCH /operations/{id}", s.requireUser(s.patchOperation))
	api.HandleFunc("POST /operations/{id}/run", s.requireUser(s.runOperation))
	api.HandleFunc("PUT /operations/{id}/labels/{label_id}", s.requireUser(s.attachLabel))
	api.HandleFunc("DELETE /operations/{id}/labels/{label_id}", s.requireUser(s.detachLabel))
	api.HandleFunc("POST /operations/{id}/pull-requests", s.requireUser(s.addPullRequest))
	api.HandleFunc("DELETE /pull-requests/{id}", s.requireUser(s.deletePullRequest))
	api.HandleFunc("POST /operations/{id}/relations", s.requireUser(s.addRelation))
	api.HandleFunc("DELETE /relations/{id}", s.requireUser(s.deleteRelation))
	api.HandleFunc("GET /labels", s.requireUser(s.listLabels))
	api.HandleFunc("POST /labels", s.requireUser(s.createLabel))
	api.HandleFunc("DELETE /labels/{id}", s.requireUser(s.deleteLabel))
	api.HandleFunc("PUT /comments/{id}/reactions/{emoji}", s.requireUser(s.addReaction))
	api.HandleFunc("DELETE /comments/{id}/reactions/{emoji}", s.requireUser(s.removeReaction))
	api.HandleFunc("PUT /operations/{id}/reactions/{emoji}", s.requireUser(s.addOperationReaction))
	api.HandleFunc("DELETE /operations/{id}/reactions/{emoji}", s.requireUser(s.removeOperationReaction))
	api.HandleFunc("GET /operations/{id}/comments", s.requireUser(s.listComments))
	api.HandleFunc("POST /operations/{id}/comments", s.requireUser(s.postComment))
	api.HandleFunc("GET /pilots", s.requireUser(s.listPilotCapabilities))
	api.HandleFunc("GET /crews", s.requireUser(s.listCrews))
	api.HandleFunc("POST /crews", s.requireUser(s.createCrew))
	api.HandleFunc("PATCH /crews/{id}", s.requireUser(s.patchCrew))
	api.HandleFunc("DELETE /crews/{id}", s.requireUser(s.deleteCrew))
	api.HandleFunc("PUT /crews/{id}/members/{member_type}/{member_id}", s.requireUser(s.addCrewMember))
	api.HandleFunc("DELETE /crews/{id}/members/{member_type}/{member_id}", s.requireUser(s.removeCrewMember))
	api.HandleFunc("GET /runs", s.requireUser(s.listRuns))
	api.HandleFunc("GET /runs/{id}", s.requireUser(s.getRun))
	api.HandleFunc("POST /runs/{id}/cancel", s.requireUser(s.cancelRun))
	api.HandleFunc("GET /members", s.requireUser(s.listMembers))
	api.HandleFunc("PATCH /members/{id}", s.requireUser(s.updateMemberRole))
	api.HandleFunc("DELETE /members/{id}", s.requireUser(s.removeMember))
	api.HandleFunc("GET /invitations", s.requireUser(s.listInvitations))
	api.HandleFunc("POST /invitations", s.requireUser(s.createInvitation))
	api.HandleFunc("GET /invitations/mine", s.requireUser(s.myInvitations))
	api.HandleFunc("DELETE /invitations/{id}", s.requireUser(s.revokeInvitation))
	api.HandleFunc("POST /invitations/{id}/accept", s.requireUser(s.acceptInvitation))
	api.HandleFunc("POST /invitations/{id}/decline", s.requireUser(s.declineInvitation))
	api.HandleFunc("GET /operations/counts", s.requireUser(s.countOperations))
	api.HandleFunc("GET /operations/working", s.requireUser(s.workingCount))
	api.HandleFunc("GET /operations/search", s.requireUser(s.searchOperations))
	api.HandleFunc("GET /missions", s.requireUser(s.listMissions))
	api.HandleFunc("GET /missions/counts", s.requireUser(s.missionCounts))
	api.HandleFunc("POST /missions", s.requireUser(s.createMission))
	api.HandleFunc("PATCH /missions/{id}", s.requireUser(s.updateMission))
	api.HandleFunc("GET /signals", s.requireUser(s.listSignals))
	api.HandleFunc("PATCH /signals/{id}", s.requireUser(s.patchSignal))
	api.HandleFunc("GET /ws", s.requireUser(s.wsConnect))

	// Rover enrollment (enrollment code authentication, handled inside).
	api.HandleFunc("POST /rover/enroll", s.enroll)

	// Rover surface (per-rover connection-token auth).
	api.HandleFunc("DELETE /rover/me", s.roverAuth(s.removeRoverEnrollment))
	api.HandleFunc("PATCH /rover/me", s.roverAuth(s.roverRefreshTags))
	api.HandleFunc("POST /rover/runs/claim", s.roverAuth(s.claimRun))
	api.HandleFunc("PATCH /rover/runs/{id}", s.roverAuth(s.setRunState))
	api.HandleFunc("PUT /rover/runs/{id}/heartbeat", s.roverAuth(s.heartbeat))
	api.HandleFunc("POST /rover/runs/{id}/events", s.roverAuth(s.appendEvent))
	api.HandleFunc("POST /rover/runs/{id}/artifacts", s.roverAuth(s.appendArtifact))
	api.HandleFunc("POST /rover/runs/{id}/messages", s.roverAuth(s.appendRunMessage))
	api.HandleFunc("POST /rover/runs/{id}/result", s.roverAuth(s.runResult))

	return s.cors(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// discovery points a client that holds only the uplink origin at the RFC 9727
// API catalog.
func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     "ufo-hub",
		"api_catalog": requestBase(r) + "/.well-known/api-catalog",
	})
}

// apiCatalog implements RFC 9727: an application/linkset+json document listing
// the hub's API(s) and, per version, links to its OpenAPI spec and health.
func (s *Server) apiCatalog(w http.ResponseWriter, r *http.Request) {
	base := requestBase(r)
	doc := map[string]any{"linkset": []map[string]any{
		{
			"anchor": base + "/.well-known/api-catalog",
			"item":   []map[string]any{{"href": base + "/v1", "title": "UFO Hub API v1"}},
		},
		{
			"anchor":       base + "/v1",
			"service-desc": []map[string]any{{"href": base + "/openapi.yaml", "type": "application/yaml"}},
			"status":       []map[string]any{{"href": base + "/healthz"}},
		},
	}}
	w.Header().Set("Content-Type", "application/linkset+json")
	_ = json.NewEncoder(w).Encode(doc)
}

// serveOpenAPI serves the embedded spec so the catalog's service-desc resolves
// on the same origin (RFC 9727 §4.1 keeps the description with the publisher).
func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(spec.Spec)
}

func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

// ---- auth ----------------------------------------------------------------

type signupReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// signup creates a user, a default fleet + owner membership, and a session —
// all in one transaction.
func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Password) < 8 {
		httpError(w, http.StatusBadRequest, "email and a password of 8+ chars are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, err)
		return
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	user, err := qtx.CreateUser(ctx, db.CreateUserParams{Email: req.Email, PasswordHash: hash, Name: req.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "email already registered")
			return
		}
		serverError(w, err)
		return
	}

	fleetName := req.Name
	if fleetName == "" {
		fleetName = strings.SplitN(req.Email, "@", 2)[0]
	}
	// The fleet created at signup is the user's immutable personal fleet
	// (no invites, no transfer/delete). Group fleets are created later.
	fleet, err := qtx.CreateFleet(ctx, db.CreateFleetParams{
		Name: fleetName + "'s fleet",
		Kind: "personal",
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if err := qtx.CreateMembership(ctx, db.CreateMembershipParams{UserID: user.ID, FleetID: fleet.ID, Role: "owner"}); err != nil {
		serverError(w, err)
		return
	}
	if err := s.startSessionTx(ctx, qtx, w, user.ID); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUserDTO(user))
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	ctx := r.Context()
	user, err := s.q.GetUserByEmail(ctx, req.Email)
	if err != nil || !auth.CheckPassword(user.PasswordHash, req.Password) {
		httpError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if auth.PasswordNeedsRehash(user.PasswordHash) {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			serverError(w, err)
			return
		}
		if err := s.q.SetUserPasswordHash(ctx, db.SetUserPasswordHashParams{ID: user.ID, PasswordHash: hash}); err != nil {
			serverError(w, err)
			return
		}
	}
	if err := s.startSessionTx(ctx, s.q, w, user.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(user))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.q.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserDTO(currentUser(r)))
}

// sessionWriter is satisfied by *db.Queries (and its tx variant).
type sessionWriter interface {
	CreateSession(ctx context.Context, arg db.CreateSessionParams) error
}

func (s *Server) startSessionTx(ctx context.Context, q sessionWriter, w http.ResponseWriter, userID int64) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	exp := time.Now().Add(sessionTTL)
	if err := q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: auth.HashToken(token), UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: exp, Valid: true},
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode, Expires: exp,
	})
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookies, MaxAge: -1})
}

// ---- tenant (UI) handlers ------------------------------------------------

func (s *Server) listFleets(w http.ResponseWriter, r *http.Request) {
	fleets, err := s.q.ListFleetsForUser(r.Context(), currentUser(r).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]fleetDTO, 0, len(fleets))
	for _, f := range fleets {
		out = append(out, toFleetDTO(f))
	}
	writeJSON(w, http.StatusOK, out)
}

type createFleetReq struct {
	Name string `json:"name"`
}

type updateFleetReq struct {
	Name string `json:"name"`
}

// createFleet makes a group fleet (invitable/manageable) owned by the creator.
func (s *Server) createFleet(w http.ResponseWriter, r *http.Request) {
	var req createFleetReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	f, err := qtx.CreateFleet(ctx, db.CreateFleetParams{Name: name, Kind: "group"})
	if err != nil {
		serverError(w, err)
		return
	}
	if err := qtx.CreateMembership(ctx, db.CreateMembershipParams{UserID: currentUser(r).ID, FleetID: f.ID, Role: "owner"}); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toFleetDTO(f))
}

func (s *Server) updateFleet(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var req updateFleetReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	ctx := r.Context()
	wid, err := s.q.ResolveFleetForMember(ctx, db.ResolveFleetForMemberParams{PublicID: pid, UserID: currentUser(r).ID})
	if err != nil {
		httpError(w, http.StatusNotFound, "fleet not found")
		return
	}
	if s.memberRole(r, wid) != "owner" {
		httpError(w, http.StatusForbidden, "only the owner can rename a fleet")
		return
	}
	f, err := s.q.UpdateFleetName(ctx, db.UpdateFleetNameParams{ID: wid, Name: name})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toFleetDTO(f))
}

// deleteFleet removes a group fleet (owner only). Personal fleets can't be deleted.
func (s *Server) deleteFleet(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	wid, err := s.q.ResolveFleetForMember(ctx, db.ResolveFleetForMemberParams{PublicID: pid, UserID: currentUser(r).ID})
	if err != nil {
		httpError(w, http.StatusNotFound, "fleet not found")
		return
	}
	if s.memberRole(r, wid) != "owner" {
		httpError(w, http.StatusForbidden, "only the owner can delete a fleet")
		return
	}
	if s.isPersonalFleet(ctx, wid) {
		httpError(w, http.StatusBadRequest, "your personal fleet can't be deleted")
		return
	}
	if err := s.q.DeleteFleet(ctx, wid); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const roverOnlineWindow = 60 * time.Second

type roverDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"` // online | busy | offline
	Units      int      `json:"units"`  // concurrent operations this rover can process
	BusyUnits  int      `json:"busy_units"`
	Tags       []string `json:"tags"`      // user-set
	AutoTags   []string `json:"auto_tags"` // rover-detected
	CreatedAt  string   `json:"created_at"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`
}

func (s *Server) listRovers(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	rows, err := s.q.ListRoversWithStatus(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]roverDTO, 0, len(rows))
	for _, rv := range rows {
		status := "offline"
		if rv.LastSeenAt.Valid && time.Since(rv.LastSeenAt.Time) < roverOnlineWindow {
			status = "online"
		}
		if rv.BusyUnits > 0 {
			status = "busy"
		}
		d := roverDTO{ID: uuidStr(rv.PublicID), Name: rv.Name, Status: status, Units: int(rv.Units), BusyUnits: int(rv.BusyUnits), Tags: rv.Tags, AutoTags: rv.AutoTags, CreatedAt: rv.CreatedAt.Time.Format(time.RFC3339)}
		if rv.LastSeenAt.Valid {
			d.LastSeenAt = rv.LastSeenAt.Time.Format(time.RFC3339)
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// patchRover edits a rover's user-managed fields (owner/admin only). auto_tags are
// owned by the rover itself via PATCH /rover/me.
func (s *Server) patchRover(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !isOwnerOrAdmin(s.memberRole(r, wid)) {
		httpError(w, http.StatusForbidden, "only owners/admins can tag rovers")
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	id, err := s.q.GetRoverIDByPublicID(r.Context(), db.GetRoverIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "rover not found")
		return
	}
	var patch map[string]json.RawMessage
	if !readJSON(w, r, &patch) {
		return
	}
	if raw, ok := patch["name"]; ok {
		namePtr, ok := jsonNullableStringValue(w, raw, "name")
		if !ok {
			return
		}
		if namePtr == nil || strings.TrimSpace(*namePtr) == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := s.q.SetRoverName(r.Context(), db.SetRoverNameParams{ID: id, FleetID: wid, Name: strings.TrimSpace(*namePtr)}); err != nil {
			serverError(w, err)
			return
		}
	}
	if raw, ok := patch["tags"]; ok {
		var tags []string
		if err := json.Unmarshal(raw, &tags); err != nil {
			httpError(w, http.StatusBadRequest, "tags must be an array")
			return
		}
		if err := s.q.SetRoverTags(r.Context(), db.SetRoverTagsParams{ID: id, FleetID: wid, Tags: normTags(tags)}); err != nil {
			serverError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteRover(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	id, err := s.q.GetRoverIDByPublicID(r.Context(), db.GetRoverIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "rover not found")
		return
	}
	if err := s.q.DeleteRover(r.Context(), db.DeleteRoverParams{ID: id, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listEnrollmentCodes(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	toks, err := s.q.ListEnrollmentCodes(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]enrollmentCodeDTO, 0, len(toks))
	for _, t := range toks {
		out = append(out, toEnrollmentCodeDTO(t))
	}
	writeJSON(w, http.StatusOK, out)
}

type createEnrollmentCodeReq struct {
	Name      string     `json:"name"`
	Uses      *int32     `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"`
}

const maxEnrollmentCodeUses int32 = 1000

func (s *Server) createEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	var req createEnrollmentCodeReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	expires := pgtype.Timestamptz{}
	if req.Uses != nil && *req.Uses < 1 {
		httpError(w, http.StatusBadRequest, "uses must be at least 1")
		return
	}
	uses := int32(1)
	if req.Uses != nil {
		uses = *req.Uses
	}
	if uses > maxEnrollmentCodeUses {
		httpError(w, http.StatusBadRequest, "uses must be at most 1000")
		return
	}
	if uses > 1 {
		if req.Name == "" {
			httpError(w, http.StatusBadRequest, "multi-use enrollment codes require name")
			return
		}
	} else {
		req.Name = ""
	}
	if req.ExpiresAt != nil {
		if req.ExpiresAt.After(time.Now().Add(365 * 24 * time.Hour)) {
			httpError(w, http.StatusBadRequest, "expires_at must be within 1 year")
			return
		}
		expires = pgtype.Timestamptz{Time: *req.ExpiresAt, Valid: true}
	}
	code, err := auth.NewToken()
	if err != nil {
		serverError(w, err)
		return
	}
	at, err := s.q.CreateEnrollmentCode(r.Context(), db.CreateEnrollmentCodeParams{
		FleetID: wid, CodeHash: auth.HashToken(code), Name: req.Name, RemainingUses: uses, ExpiresAt: expires,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	d := toEnrollmentCodeDTO(at)
	d.Code = code
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) deleteEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	id, err := s.q.GetEnrollmentCodeIDByPublicID(r.Context(), db.GetEnrollmentCodeIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "enrollment code not found")
		return
	}
	if err := s.q.DeleteEnrollmentCode(r.Context(), db.DeleteEnrollmentCodeParams{ID: id, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// enroll exchanges an enrollment code for a per-rover connection token.
type enrollReq struct {
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`      // user tags supplied at enroll
	AutoTags []string `json:"auto_tags"` // rover-detected (pilot:*, os:*, arch:*)
}
type enrollResp struct {
	Token string `json:"token"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	code := bearerToken(r)
	if code == "" {
		httpError(w, http.StatusUnauthorized, "missing enrollment code")
		return
	}
	var req enrollReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "rover"
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	at, err := qtx.GetEnrollmentCodeForUpdate(ctx, auth.HashToken(code))
	if err != nil {
		httpError(w, http.StatusUnauthorized, "invalid enrollment code")
		return
	}
	if at.ExpiresAt.Valid && at.ExpiresAt.Time.Before(time.Now()) {
		httpError(w, http.StatusUnauthorized, "enrollment code expired")
		return
	}
	runTestHook(func(h testHooks) func() { return h.afterEnrollmentCodeLocked })

	connToken, err := auth.NewToken()
	if err != nil {
		serverError(w, err)
		return
	}
	rover, err := qtx.CreateRover(ctx, db.CreateRoverParams{
		FleetID:          at.FleetID,
		Name:             name,
		EnrollmentCodeID: pgtype.Int8{Int64: at.ID, Valid: true},
		TokenHash:        auth.HashToken(connToken),
		Tags:             normTags(req.Tags),
		AutoTags:         normTags(req.AutoTags),
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if at.RemainingUses <= 1 {
		if err := qtx.DeleteEnrollmentCode(ctx, db.DeleteEnrollmentCodeParams{ID: at.ID, FleetID: at.FleetID}); err != nil {
			serverError(w, err)
			return
		}
	} else if err := qtx.DecrementEnrollmentCodeUses(ctx, db.DecrementEnrollmentCodeUsesParams{ID: at.ID, FleetID: at.FleetID}); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, enrollResp{Token: connToken, ID: uuidStr(rover.PublicID), Name: rover.Name})
}

type createOperationReq struct {
	Title           string   `json:"title"`
	Body            string   `json:"body"`
	MissionID       *string  `json:"mission_id"`    // mission public id
	AssigneeType    string   `json:"assignee_type"` // pilot | user | crew
	AssigneeID      *string  `json:"assignee_id"`   // referenced resource public id
	StartNow        *bool    `json:"start_immediately"`
	RequiredTags    []string `json:"required_tags"`     // dispatch allow list
	ExcludedTags    []string `json:"excluded_tags"`     // dispatch deny list
	Priority        int16    `json:"priority"`          // 0 none → 4 urgent
	MainOperationID *string  `json:"main_operation_id"` // main operation public id
	StartDate       *string  `json:"start_date"`        // YYYY-MM-DD
	DueDate         *string  `json:"due_date"`
}

// parseDate maps an optional YYYY-MM-DD string to pgtype.Date.
func parseDate(s *string) (pgtype.Date, bool) {
	if s == nil || *s == "" {
		return pgtype.Date{}, true
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return pgtype.Date{}, false
	}
	return pgtype.Date{Time: t, Valid: true}, true
}

func pgDateToStringPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

func patchHas(patch map[string]json.RawMessage, field string) bool {
	_, ok := patch[field]
	return ok
}

func jsonNullableStringValue(w http.ResponseWriter, raw json.RawMessage, field string) (*string, bool) {
	if string(raw) == "null" {
		return nil, true
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		httpError(w, http.StatusBadRequest, field+" must be a string")
		return nil, false
	}
	return &v, true
}

func jsonStringValue(w http.ResponseWriter, raw json.RawMessage, field string) (string, bool) {
	v, ok := jsonNullableStringValue(w, raw, field)
	if !ok {
		return "", false
	}
	if v == nil {
		httpError(w, http.StatusBadRequest, field+" must be a string")
		return "", false
	}
	return *v, true
}

func jsonStringSlice(w http.ResponseWriter, patch map[string]json.RawMessage, field string, fallback []string) ([]string, bool) {
	raw, ok := patch[field]
	if !ok {
		return fallback, true
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		httpError(w, http.StatusBadRequest, field+" must be an array")
		return nil, false
	}
	return v, true
}

func jsonInt16Value(w http.ResponseWriter, raw json.RawMessage, field string) (int16, bool) {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		httpError(w, http.StatusBadRequest, field+" must be a number")
		return 0, false
	}
	if v < -32768 || v > 32767 {
		httpError(w, http.StatusBadRequest, field+" is out of range")
		return 0, false
	}
	return int16(v), true
}

func jsonBoolValue(w http.ResponseWriter, raw json.RawMessage, field string) (bool, bool) {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		httpError(w, http.StatusBadRequest, field+" must be a boolean")
		return false, false
	}
	return v, true
}

// resolveAssignee maps an assignee ref to stored columns: a bigint id for
// user/crew, a kind string for pilot. Empty ref = unassigned.
func (s *Server) resolveAssignee(ctx context.Context, fleet int64, atype string, aid *string) (pgtype.Int8, pgtype.Text, bool) {
	if aid == nil || *aid == "" {
		return pgtype.Int8{}, pgtype.Text{}, atype == ""
	}
	if atype == "pilot" {
		if !validPilotKind(*aid) {
			return pgtype.Int8{}, pgtype.Text{}, false
		}
		return pgtype.Int8{}, pgtype.Text{String: *aid, Valid: true}, true
	}
	pid, ok := parseUUID(*aid)
	if !ok {
		return pgtype.Int8{}, pgtype.Text{}, false
	}
	var id int64
	var err error
	switch atype {
	case "user":
		id, err = s.q.GetMemberUserIDByPublicID(ctx, db.GetMemberUserIDByPublicIDParams{PublicID: pid, FleetID: fleet})
	case "crew":
		id, err = s.q.GetCrewIDByPublicID(ctx, db.GetCrewIDByPublicIDParams{PublicID: pid, FleetID: fleet})
	default:
		return pgtype.Int8{}, pgtype.Text{}, false
	}
	if err != nil {
		return pgtype.Int8{}, pgtype.Text{}, false
	}
	return pgtype.Int8{Int64: id, Valid: true}, pgtype.Text{}, true
}

// resolvePilotKind returns the kind that drives an assignment, or "" if
// human-only. Crews pick the captain-if-pilot, else the first pilot member.
func (s *Server) resolvePilotKind(ctx context.Context, q *db.Queries, atype string, pilotKind pgtype.Text, crewID pgtype.Int8) string {
	switch atype {
	case "pilot":
		if pilotKind.Valid && validPilotKind(pilotKind.String) {
			return pilotKind.String
		}
		return ""
	case "crew":
		if !crewID.Valid {
			return ""
		}
		members, err := q.ListCrewMembers(ctx, crewID.Int64)
		if err != nil {
			return ""
		}
		if kinds := crewPilotKinds(members); len(kinds) > 0 {
			return kinds[0]
		}
		return ""
	default: // user or unassigned
		return ""
	}
}

// crewPilotKinds lists a crew's pilot kinds, captain first, deduped.
func crewPilotKinds(members []db.CrewMember) []string {
	var captain string
	var rest []string
	seen := map[string]bool{}
	for _, m := range members {
		if m.MemberType != "pilot" || !m.PilotKind.Valid || seen[m.PilotKind.String] {
			continue
		}
		seen[m.PilotKind.String] = true
		if m.Role == "captain" && captain == "" {
			captain = m.PilotKind.String
		} else {
			rest = append(rest, m.PilotKind.String)
		}
	}
	if captain != "" {
		return append([]string{captain}, rest...)
	}
	return rest
}

// crewPickKind returns the first usable crew pilot, preferring idle rovers when requested.
func (s *Server) crewPickKind(ctx context.Context, q *db.Queries, fleetID, crewID int64, exclude map[string]bool, preferFree bool) string {
	members, err := q.ListCrewMembers(ctx, crewID)
	if err != nil {
		return ""
	}
	rows, err := q.FleetPilotKindFree(ctx, db.FleetPilotKindFreeParams{FleetID: fleetID, Column2: roverOnlineWindow.Seconds()})
	if err != nil {
		return ""
	}
	free := map[string]bool{}
	hasRover := map[string]bool{}
	for _, r := range rows {
		hasRover[r.Kind] = true
		free[r.Kind] = r.HasFree
	}
	var fallback string
	for _, k := range crewPilotKinds(members) {
		if exclude[k] || !hasRover[k] {
			continue
		}
		if preferFree && free[k] {
			return k
		}
		if fallback == "" {
			fallback = k
		}
	}
	return fallback
}

func (s *Server) resolveDispatchKind(ctx context.Context, q *db.Queries, fleetID int64, atype string, pilotKind pgtype.Text, crewID pgtype.Int8) string {
	if atype == "crew" {
		if !crewID.Valid {
			return ""
		}
		return s.crewPickKind(ctx, q, fleetID, crewID.Int64, nil, true)
	}
	return s.resolvePilotKind(ctx, q, atype, pilotKind, crewID)
}

// crewFailover tries the next eligible crew pilot after a run failure.
func (s *Server) crewFailover(ctx context.Context, op db.Operation, run db.Run, runState string) bool {
	if op.AssigneeType.String != "crew" || !op.AssigneeID.Valid {
		return false
	}
	failed, err := s.q.FailedPilotKindsForOperation(ctx, op.ID)
	if err != nil {
		return false
	}
	exclude := map[string]bool{run.Pilot: true}
	for _, k := range failed {
		exclude[k] = true
	}
	pick := s.crewPickKind(ctx, s.q, op.FleetID, op.AssigneeID.Int64, exclude, true)
	if pick == "" {
		return false
	}
	if err := s.dispatchRun(ctx, s.q, op, pick, "failover"); err != nil {
		return false
	}
	_ = s.setOperationStatus(ctx, s.q, op, "in_progress")
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "system",
		Body: fmt.Sprintf("Crew failover: reassigned to %s after run #%d %s", pick, run.ID, runState),
	})
	return true
}

// fleetHasRoverFor reports whether the fleet has a rover the kind can drive
// (online or not; an offline one claims when it returns).
func (s *Server) fleetHasRoverFor(ctx context.Context, q *db.Queries, fleetID int64, kind string) bool {
	caps, err := q.FleetPilotCapabilities(ctx, db.FleetPilotCapabilitiesParams{FleetID: fleetID, Column2: roverOnlineWindow.Seconds()})
	if err != nil {
		return false
	}
	for _, c := range caps {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// dispatchOrBlock queues work or blocks when the fleet has no capable rover.
func (s *Server) dispatchOrBlock(ctx context.Context, q *db.Queries, op db.Operation, kind, prompt string) (string, error) {
	if !s.fleetHasRoverFor(ctx, q, op.FleetID, kind) {
		return "blocked", s.blockNoRover(ctx, q, op, kind)
	}
	if err := s.dispatchRun(ctx, q, op, kind, prompt); err != nil {
		return "", err
	}
	return "in_progress", nil
}

// blockNoRover records that this pilot has no fleet rover to drive.
func (s *Server) blockNoRover(ctx context.Context, q *db.Queries, op db.Operation, kind string) error {
	if err := s.setOperationStatus(ctx, q, op, "blocked"); err != nil {
		return err
	}
	msg := fmt.Sprintf("The %s pilot has no rover to drive in this fleet. Enroll a rover it can drive.", kind)
	_, _ = q.CreateComment(ctx, db.CreateCommentParams{OperationID: op.ID, AuthorType: "system", Body: msg})
	if ids, err := q.ListFleetMemberIDs(ctx, op.FleetID); err == nil {
		for _, uid := range ids {
			_, _ = q.CreateSignal(ctx, db.CreateSignalParams{
				FleetID: op.FleetID, RecipientUserID: uid,
				OperationID: pgtype.Int8{Int64: op.ID, Valid: true},
				Type:        "no_rover", Severity: "action_required", Title: "No capable rover", Body: msg,
			})
		}
	}
	return nil
}

// dispatchRun queues a run for an operation. A non-empty `prompt` is a human
// reply driving a continuation.
func (s *Server) dispatchRun(ctx context.Context, q *db.Queries, op db.Operation, kind, prompt string) error {
	if !validPilotKind(kind) {
		return fmt.Errorf("invalid pilot kind %q", kind)
	}
	// Resume only on the rover that owns the matching pilot session.
	canResume := op.PilotSessionID.Valid &&
		op.PilotSessionKind.Valid && op.PilotSessionKind.String == kind &&
		op.PilotSessionRoverID.Valid && s.roverOnline(ctx, op.PilotSessionRoverID.Int64)

	session := pgtype.Text{}
	requiredRover := pgtype.Int8{}
	command := ""
	switch {
	case canResume:
		session = op.PilotSessionID            // native resume; rover sends `prompt` into the session
		requiredRover = op.PilotSessionRoverID // pin to the rover that holds it
		command = prompt
	case prompt != "":
		command = s.contextPrompt(ctx, q, op)
	}
	// First run: command stays empty, and the rover derives title + body.

	run, err := q.CreateRun(ctx, db.CreateRunParams{
		FleetID: op.FleetID, OperationID: op.ID, MissionID: pgtype.Int8{Int64: op.MissionID, Valid: true}, Command: command, Pilot: kind,
		SessionID: session, RequiredRoverID: requiredRover,
	})
	if err != nil {
		return err
	}
	_, err = q.AppendRunEvent(ctx, db.AppendRunEventParams{RunID: run.ID, Kind: "status", Message: "queued"})
	return err
}

func activeRunConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "runs_one_active_per_operation_idx"
}

// contextPrompt gives a fresh session the operation and conversation so far.
func (s *Server) contextPrompt(ctx context.Context, q *db.Queries, op db.Operation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n\n--- Conversation so far ---\n", op.Title, op.Body)
	if comments, err := q.ListComments(ctx, op.ID); err == nil {
		for _, c := range comments {
			who := c.AuthorType
			if who == "user" {
				who = "Human"
			} else if who == "pilot" {
				who = "Pilot"
			}
			fmt.Fprintf(&b, "%s: %s\n", who, c.Body)
		}
	}
	b.WriteString("\nContinue the work, taking the conversation above into account.")
	return b.String()
}

func (s *Server) createOperation(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	var req createOperationReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Title == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Priority < 0 || req.Priority > 4 {
		httpError(w, http.StatusBadRequest, "priority must be 0–4")
		return
	}
	startDate, startOK := parseDate(req.StartDate)
	dueDate, dueOK := parseDate(req.DueDate)
	if !startOK || !dueOK {
		httpError(w, http.StatusBadRequest, "dates must use YYYY-MM-DD")
		return
	}
	ctx := r.Context()
	// Every operation belongs to a mission; a fleet with no mission can't take operations.
	if req.MissionID == nil || *req.MissionID == "" {
		httpError(w, http.StatusBadRequest, "create a mission first")
		return
	}
	mpid, ok := parseUUID(*req.MissionID)
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid mission")
		return
	}
	missionID, err := s.q.GetMissionIDByPublicID(ctx, db.GetMissionIDByPublicIDParams{PublicID: mpid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusBadRequest, "mission not found")
		return
	}
	assigneeID, pilotKind, ok := s.resolveAssignee(ctx, wid, req.AssigneeType, req.AssigneeID)
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid assignee")
		return
	}
	assigneeType := optText(req.AssigneeType)
	mainOperationID := pgtype.Int8{}
	if req.MainOperationID != nil && *req.MainOperationID != "" {
		ppid, ok := parseUUID(*req.MainOperationID)
		if !ok {
			httpError(w, http.StatusBadRequest, "invalid main operation")
			return
		}
		pid, err := s.q.GetOperationIDByPublicID(ctx, db.GetOperationIDByPublicIDParams{PublicID: ppid, FleetID: wid})
		if err != nil {
			httpError(w, http.StatusBadRequest, "main operation not found")
			return
		}
		mainOperationID = pgtype.Int8{Int64: pid, Valid: true}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	// Allocate the per-mission operation number. The displayed id is <key>-<sequence>.
	sequence, err := qtx.BumpMissionSequence(ctx, db.BumpMissionSequenceParams{ID: missionID, FleetID: wid})
	if err != nil {
		serverError(w, err)
		return
	}

	// Auto-exec policy: a pilot assignment dispatches; human-only work stays backlog.
	kind := s.resolveDispatchKind(ctx, qtx, wid, req.AssigneeType, pilotKind, assigneeID)
	startNow := req.StartNow == nil || *req.StartNow
	status := "backlog"
	if kind != "" {
		status = "todo"
		if startNow {
			status = "in_progress"
		}
	}
	op, err := qtx.CreateOperation(ctx, db.CreateOperationParams{
		FleetID: wid, Title: req.Title, Body: req.Body, MissionID: missionID,
		AssigneeType: assigneeType, AssigneeID: assigneeID, AssigneePilotKind: pilotKind, Status: status, Sequence: sequence,
		RequiredTags: normTags(req.RequiredTags), ExcludedTags: normTags(req.ExcludedTags),
		Priority: req.Priority, MainOperationID: mainOperationID, StartDate: startDate, DueDate: dueDate,
		CreatedBy: pgtype.Int8{Int64: currentUser(r).ID, Valid: true},
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if kind != "" && startNow {
		st, err := s.dispatchOrBlock(ctx, qtx, op, kind, "")
		if err != nil {
			serverError(w, err)
			return
		}
		op.Status = st
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.operationDTO(ctx, op))
}

// listOperations serves one board column, keyset-paginated:
// ?status=&mission=&before=&limit= (mission/before 0 = all/newest). Without a
// status it returns the newest page across statuses (small, bounded).
// boardFilters holds the optional board filters, resolved to internal ids
// (0/”/-1 = unset). mission/before are resolved separately.
type boardFilters struct {
	priority        int16  // -1 = any
	assigneeKind    string // "" | user | pilot | crew
	assigneeID      int64  // 0 = any (specific user/crew)
	pilotKind       string // "" = any (specific pilot kind)
	creator         int64  // 0 = any
	label           int64  // 0 = any
	includeArchived bool   // false = hide archived operations
}

// parseBoardFilters reads + resolves the board filter query params for a fleet.
func (s *Server) parseBoardFilters(ctx context.Context, q url.Values, fleet int64) boardFilters {
	f := boardFilters{priority: -1}
	if v := q.Get("priority"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.priority = int16(n)
		}
	}
	if k := q.Get("assignee_kind"); k == "user" || k == "pilot" || k == "crew" {
		f.assigneeKind = k
	}
	// A specific pilot is filtered by kind; a specific user/crew by public id.
	if v := q.Get("pilot"); validPilotKind(v) {
		f.pilotKind, f.assigneeKind = v, "pilot"
	}
	if v := q.Get("assignee"); v != "" {
		if pid, ok := parseUUID(v); ok {
			if id, err := s.q.GetUserIDByPublicID(ctx, pid); err == nil {
				f.assigneeID, f.assigneeKind = id, "user"
			} else if id, err := s.q.GetCrewIDByPublicID(ctx, db.GetCrewIDByPublicIDParams{PublicID: pid, FleetID: fleet}); err == nil {
				f.assigneeID, f.assigneeKind = id, "crew"
			}
		}
	}
	if v := q.Get("creator"); v != "" {
		if pid, ok := parseUUID(v); ok {
			if id, err := s.q.GetUserIDByPublicID(ctx, pid); err == nil {
				f.creator = id
			}
		}
	}
	if v := q.Get("label"); v != "" {
		if pid, ok := parseUUID(v); ok {
			if id, err := s.q.GetLabelIDByPublicID(ctx, db.GetLabelIDByPublicIDParams{PublicID: pid, FleetID: fleet}); err == nil {
				f.label = id
			}
		}
	}
	f.includeArchived = q.Get("archived") == "1"
	return f
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	status := q.Get("status")
	limit := queryInt(q, "limit", 50)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	mission := s.resolveMissionParam(ctx, q.Get("mission"), wid)
	before := int64(0)
	if v := q.Get("before"); v != "" {
		if pid, ok := parseUUID(v); ok {
			if id, err := s.q.GetOperationIDByPublicID(ctx, db.GetOperationIDByPublicIDParams{PublicID: pid, FleetID: wid}); err == nil {
				before = id
			}
		}
	}
	if status == "" {
		// Bounded fallback for non-board callers.
		ops, err := s.q.ListOperations(ctx, wid)
		if err != nil {
			serverError(w, err)
			return
		}
		if int64(len(ops)) > limit {
			ops = ops[:limit]
		}
		writeJSON(w, http.StatusOK, s.operationDTOs(ctx, ops))
		return
	}
	f := s.parseBoardFilters(ctx, q, wid)
	ops, err := s.q.ListOperationsByStatus(ctx, db.ListOperationsByStatusParams{
		FleetID: wid, Status: status, Column3: mission, Column4: before, Limit: int32(limit),
		Column6: f.priority, Column7: f.assigneeKind, Column8: f.assigneeID, Column9: f.creator, Column10: f.label,
		Column11: f.includeArchived, Column12: f.pilotKind,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.operationDTOs(ctx, ops))
}

// resolveMissionParam maps a mission public id query value to its internal id (0 = all).
func (s *Server) resolveMissionParam(ctx context.Context, v string, fleet int64) int64 {
	if v == "" {
		return 0
	}
	pid, ok := parseUUID(v)
	if !ok {
		return 0
	}
	id, err := s.q.GetMissionIDByPublicID(ctx, db.GetMissionIDByPublicIDParams{PublicID: pid, FleetID: fleet})
	if err != nil {
		return 0
	}
	return id
}

// workingCount reports queued vs claimed/running operations (board pills).
func (s *Server) workingCount(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	rows, err := s.q.CountActiveRunsByState(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := map[string]int64{"count": 0, "queued": 0, "working": 0}
	for _, row := range rows {
		if row.State == "queued" {
			out["queued"] += row.N
		} else {
			out["working"] += row.N
		}
		out["count"] += row.N
	}
	writeJSON(w, http.StatusOK, out)
}

// missionCounts returns per-mission operation counts (keyed by mission id).
func (s *Server) missionCounts(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	rows, err := s.q.CountOperationsByMission(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[uuidStr(row.MissionID)] = row.N
	}
	writeJSON(w, http.StatusOK, counts)
}

// countOperations returns per-status counts (optionally scoped to one mission).
func (s *Server) countOperations(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	mission := s.resolveMissionParam(ctx, q.Get("mission"), wid)
	f := s.parseBoardFilters(ctx, q, wid)
	rows, err := s.q.CountOperationsByStatus(ctx, db.CountOperationsByStatusParams{
		FleetID: wid, Column2: mission,
		Column3: f.priority, Column4: f.assigneeKind, Column5: f.assigneeID, Column6: f.creator, Column7: f.label,
		Column8: f.includeArchived, Column9: f.pilotKind,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.Status] = row.N
	}
	writeJSON(w, http.StatusOK, counts)
}

type operationDetail struct {
	Operation     operationDTO     `json:"operation"`
	Comments      []commentDTO     `json:"comments"`
	Runs          []runDTO         `json:"runs"`
	SubOperations []operationDTO   `json:"sub_operations"`
	PullRequests  []pullRequestDTO `json:"pull_requests"`
	Relations     []relationDTO    `json:"relations"`
}

// operationInFleet loads an operation by its public id, scoped to the request's
// fleet, or writes 404.
func (s *Server) operationInFleet(w http.ResponseWriter, r *http.Request) (db.Operation, int64, bool) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return db.Operation{}, 0, false
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return db.Operation{}, 0, false
	}
	id, err := s.q.GetOperationIDByPublicID(r.Context(), db.GetOperationIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "operation not found")
		return db.Operation{}, 0, false
	}
	op, err := s.q.GetOperation(r.Context(), db.GetOperationParams{ID: id, FleetID: wid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "operation not found")
		} else {
			serverError(w, err)
		}
		return db.Operation{}, 0, false
	}
	return op, wid, true
}

func (s *Server) getOperation(w http.ResponseWriter, r *http.Request) {
	op, _, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	comments, err := s.q.ListComments(ctx, op.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	runs, err := s.q.ListRunsByOperation(ctx, op.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	subOperations, _ := s.q.ListSubOperations(ctx, pgtype.Int8{Int64: op.ID, Valid: true})
	relations, _ := s.q.ListRelationsForOperation(ctx, op.ID)
	pullRequests, _ := s.q.ListPullRequestsForOperation(ctx, op.ID)
	pullRequestDTOs := make([]pullRequestDTO, 0, len(pullRequests))
	for _, p := range pullRequests {
		pullRequestDTOs = append(pullRequestDTOs, toPullRequestDTO(p))
	}
	opDTO := s.operationDTO(ctx, op)
	opDTO.Reactions = s.reactionsForTargets(ctx, "operation", []int64{op.ID}, currentUser(r).ID)[op.ID]
	if opDTO.Reactions == nil {
		opDTO.Reactions = []reactionDTO{}
	}
	writeJSON(w, http.StatusOK, operationDetail{
		Operation:     opDTO,
		Comments:      s.commentDTOs(ctx, comments, currentUser(r).ID),
		Runs:          s.runDTOs(ctx, runs),
		SubOperations: s.operationDTOs(ctx, subOperations),
		PullRequests:  pullRequestDTOs,
		Relations:     toRelationDTOs(relations),
	})
}

var validOperationStatus = map[string]bool{
	"backlog": true, "todo": true, "in_progress": true,
	"in_review": true, "done": true, "blocked": true, "cancelled": true,
}

// Statuses a pilot may request after a run finishes.
var pilotSettableStatus = map[string]bool{
	"in_review": true, "done": true, "blocked": true, "cancelled": true,
}

func (s *Server) setOperationStatus(ctx context.Context, q *db.Queries, op db.Operation, status string) error {
	return q.SetOperationStatus(ctx, db.SetOperationStatusParams{ID: op.ID, FleetID: op.FleetID, Status: status})
}

func applyStatusToDTO(op *db.Operation, status string) {
	op.Status = status
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if status == "in_progress" && !op.StartedAt.Valid {
		op.StartedAt = now
	}
	if status == "done" || status == "cancelled" {
		if !op.FinishedAt.Valid {
			op.FinishedAt = now
		}
	} else {
		op.FinishedAt = pgtype.Timestamptz{}
	}
}

type runOperationReq struct {
	Message string `json:"message"`
}

func (s *Server) runOperation(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	var req runOperationReq
	if r.Body != http.NoBody && r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	prompt := strings.TrimSpace(req.Message)
	atype := ""
	if op.AssigneeType.Valid {
		atype = op.AssigneeType.String
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	kind := s.resolveDispatchKind(ctx, qtx, wid, atype, op.AssigneePilotKind, op.AssigneeID)
	if kind == "" {
		httpError(w, http.StatusBadRequest, "operation has no pilot assigned")
		return
	}
	if prompt != "" {
		if _, err := qtx.CreateComment(ctx, db.CreateCommentParams{
			OperationID: op.ID, AuthorType: "user", AuthorID: pgtype.Int8{Int64: currentUser(r).ID, Valid: true}, Body: prompt,
		}); err != nil {
			serverError(w, err)
			return
		}
	}
	st, err := s.dispatchOrBlock(ctx, qtx, op, kind, prompt)
	if err != nil {
		if activeRunConflict(err) {
			httpError(w, http.StatusConflict, "operation already has an active run")
			return
		}
		serverError(w, err)
		return
	}
	if err := s.setOperationStatus(ctx, qtx, op, st); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) patchOperation(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	var patch map[string]json.RawMessage
	if !readJSON(w, r, &patch) {
		return
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	if _, ok := patch["assignee_type"]; ok {
		assigneeTypePtr, ok := jsonNullableStringValue(w, patch["assignee_type"], "assignee_type")
		if !ok {
			return
		}
		assigneeType := ""
		if assigneeTypePtr != nil {
			assigneeType = *assigneeTypePtr
		}
		var assigneeID *string
		if raw, ok := patch["assignee_id"]; ok {
			assigneeID, ok = jsonNullableStringValue(w, raw, "assignee_id")
			if !ok {
				return
			}
		}
		resolvedID, pilotKind, ok := s.resolveAssignee(ctx, wid, assigneeType, assigneeID)
		if !ok {
			httpError(w, http.StatusBadRequest, "invalid assignee")
			return
		}
		updated, err := qtx.AssignOperation(ctx, db.AssignOperationParams{
			ID: op.ID, FleetID: wid, AssigneeType: optText(assigneeType), AssigneeID: resolvedID, AssigneePilotKind: pilotKind,
		})
		if err != nil {
			serverError(w, err)
			return
		}
		op = updated
		status := "backlog"
		if kind := s.resolveDispatchKind(ctx, qtx, wid, assigneeType, pilotKind, resolvedID); kind != "" {
			st, err := s.dispatchOrBlock(ctx, qtx, updated, kind, "")
			if err != nil {
				if activeRunConflict(err) {
					httpError(w, http.StatusConflict, "operation already has an active run")
					return
				}
				serverError(w, err)
				return
			}
			status = st
		}
		if err := s.setOperationStatus(ctx, qtx, op, status); err != nil {
			serverError(w, err)
			return
		}
		applyStatusToDTO(&op, status)
	} else if _, ok := patch["assignee_id"]; ok {
		httpError(w, http.StatusBadRequest, "assignee_type is required with assignee_id")
		return
	}

	if raw, ok := patch["status"]; ok {
		status, ok := jsonStringValue(w, raw, "status")
		if !ok {
			return
		}
		if !validOperationStatus[status] {
			httpError(w, http.StatusBadRequest, "invalid status")
			return
		}
		if err := s.setOperationStatus(ctx, qtx, op, status); err != nil {
			serverError(w, err)
			return
		}
		if status != "in_review" && status != "blocked" {
			_ = qtx.ArchiveActionRequiredForOperation(ctx, pgtype.Int8{Int64: op.ID, Valid: true})
		}
		applyStatusToDTO(&op, status)
	}

	if _, rok := patch["required_tags"]; rok {
		required, ok := jsonStringSlice(w, patch, "required_tags", op.RequiredTags)
		if !ok {
			return
		}
		excluded, ok := jsonStringSlice(w, patch, "excluded_tags", op.ExcludedTags)
		if !ok {
			return
		}
		if err := qtx.UpdateOperationTags(ctx, db.UpdateOperationTagsParams{
			ID: op.ID, FleetID: wid, RequiredTags: normTags(required), ExcludedTags: normTags(excluded),
		}); err != nil {
			serverError(w, err)
			return
		}
		op.RequiredTags, op.ExcludedTags = required, excluded
	} else if _, eok := patch["excluded_tags"]; eok {
		required, ok := jsonStringSlice(w, patch, "required_tags", op.RequiredTags)
		if !ok {
			return
		}
		excluded, ok := jsonStringSlice(w, patch, "excluded_tags", op.ExcludedTags)
		if !ok {
			return
		}
		if err := qtx.UpdateOperationTags(ctx, db.UpdateOperationTagsParams{
			ID: op.ID, FleetID: wid, RequiredTags: normTags(required), ExcludedTags: normTags(excluded),
		}); err != nil {
			serverError(w, err)
			return
		}
		op.RequiredTags, op.ExcludedTags = required, excluded
	}

	if raw, ok := patch["priority"]; ok {
		priority, ok := jsonInt16Value(w, raw, "priority")
		if !ok {
			return
		}
		if priority < 0 || priority > 4 {
			httpError(w, http.StatusBadRequest, "priority must be 0–4")
			return
		}
		if err := qtx.SetOperationPriority(ctx, db.SetOperationPriorityParams{ID: op.ID, FleetID: wid, Priority: priority}); err != nil {
			serverError(w, err)
			return
		}
		op.Priority = priority
	}

	if _, sok := patch["start_date"]; sok || patchHas(patch, "due_date") {
		start := pgDateToStringPtr(op.StartDate)
		if raw, ok := patch["start_date"]; ok {
			start, ok = jsonNullableStringValue(w, raw, "start_date")
			if !ok {
				return
			}
		}
		due := pgDateToStringPtr(op.DueDate)
		if raw, ok := patch["due_date"]; ok {
			due, ok = jsonNullableStringValue(w, raw, "due_date")
			if !ok {
				return
			}
		}
		startDate, startOK := parseDate(start)
		dueDate, dueOK := parseDate(due)
		if !startOK || !dueOK {
			httpError(w, http.StatusBadRequest, "dates must use YYYY-MM-DD")
			return
		}
		if err := qtx.SetOperationDates(ctx, db.SetOperationDatesParams{
			ID: op.ID, FleetID: wid, StartDate: startDate, DueDate: dueDate,
		}); err != nil {
			serverError(w, err)
			return
		}
		op.StartDate, op.DueDate = startDate, dueDate
	}

	if raw, ok := patch["main_operation_id"]; ok {
		mainOperationID, ok := jsonNullableStringValue(w, raw, "main_operation_id")
		if !ok {
			return
		}
		mainOperation := pgtype.Int8{}
		if mainOperationID != nil && *mainOperationID != "" {
			ppid, ok := parseUUID(*mainOperationID)
			if !ok {
				httpError(w, http.StatusBadRequest, "invalid main operation")
				return
			}
			pid, err := qtx.GetOperationIDByPublicID(ctx, db.GetOperationIDByPublicIDParams{PublicID: ppid, FleetID: wid})
			if err != nil || pid == op.ID {
				httpError(w, http.StatusBadRequest, "invalid main operation")
				return
			}
			mainOperation = pgtype.Int8{Int64: pid, Valid: true}
		}
		if err := qtx.SetMainOperation(ctx, db.SetMainOperationParams{ID: op.ID, FleetID: wid, MainOperationID: mainOperation}); err != nil {
			serverError(w, err)
			return
		}
		op.MainOperationID = mainOperation
	}

	if raw, ok := patch["archived"]; ok {
		archived, ok := jsonBoolValue(w, raw, "archived")
		if !ok {
			return
		}
		if err := qtx.SetOperationArchived(ctx, db.SetOperationArchivedParams{ID: op.ID, FleetID: wid, Archived: archived}); err != nil {
			serverError(w, err)
			return
		}
		op.Archived = archived
	}

	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	updated, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: op.ID, FleetID: wid})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.operationDTO(ctx, updated))
}

// ---- labels ----

func (s *Server) listLabels(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	labels, err := s.q.ListLabels(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]labelDTO, 0, len(labels))
	for _, l := range labels {
		out = append(out, toLabelDTO(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createLabel(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Color == "" {
		req.Color = "gray"
	}
	l, err := s.q.CreateLabel(r.Context(), db.CreateLabelParams{FleetID: wid, Name: req.Name, Color: req.Color})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "that label already exists")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLabelDTO(l))
}

func (s *Server) deleteLabel(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	id, ok := s.labelIDByPath(w, r, wid)
	if !ok {
		return
	}
	if err := s.q.DeleteLabel(r.Context(), db.DeleteLabelParams{ID: id, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) labelIDByPath(w http.ResponseWriter, r *http.Request, fleetID int64) (int64, bool) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return 0, false
	}
	id, err := s.q.GetLabelIDByPublicID(r.Context(), db.GetLabelIDByPublicIDParams{PublicID: pid, FleetID: fleetID})
	if err != nil {
		httpError(w, http.StatusNotFound, "label not found")
		return 0, false
	}
	return id, true
}

func (s *Server) attachLabel(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	lpid, ok := parseUUID(r.PathValue("label_id"))
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid label")
		return
	}
	lid, err := s.q.GetLabelIDByPublicID(r.Context(), db.GetLabelIDByPublicIDParams{PublicID: lpid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "label not found")
		return
	}
	if err := s.q.AddOperationLabel(r.Context(), db.AddOperationLabelParams{OperationID: op.ID, LabelID: lid}); err != nil {
		serverError(w, err)
		return
	}
	_ = s.q.TouchOperation(r.Context(), db.TouchOperationParams{ID: op.ID, FleetID: wid})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) detachLabel(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	lpid, ok := parseUUID(r.PathValue("label_id"))
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid label")
		return
	}
	lid, err := s.q.GetLabelIDByPublicID(r.Context(), db.GetLabelIDByPublicIDParams{PublicID: lpid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "label not found")
		return
	}
	if err := s.q.RemoveOperationLabel(r.Context(), db.RemoveOperationLabelParams{OperationID: op.ID, LabelID: lid}); err != nil {
		serverError(w, err)
		return
	}
	_ = s.q.TouchOperation(r.Context(), db.TouchOperationParams{ID: op.ID, FleetID: wid})
	w.WriteHeader(http.StatusNoContent)
}

// ---- pull requests (manual linking; GitHub auto-link not yet supported) ----

func (s *Server) addPullRequest(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	var req struct {
		URL    string `json:"url"`
		Title  string `json:"title"`
		Number *int32 `json:"number"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		httpError(w, http.StatusBadRequest, "url is required")
		return
	}
	num := pgtype.Int4{}
	if req.Number != nil {
		num = pgtype.Int4{Int32: *req.Number, Valid: true}
	}
	pullRequest, err := s.q.CreatePullRequest(r.Context(), db.CreatePullRequestParams{OperationID: op.ID, Url: req.URL, Title: req.Title, Number: num})
	if err != nil {
		serverError(w, err)
		return
	}
	_ = s.q.TouchOperation(r.Context(), db.TouchOperationParams{ID: op.ID, FleetID: wid})
	writeJSON(w, http.StatusCreated, toPullRequestDTO(pullRequest))
}

func (s *Server) deletePullRequest(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if err := s.q.DeletePullRequest(r.Context(), db.DeletePullRequestParams{PublicID: pid, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- relations ----

func (s *Server) addRelation(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind   string `json:"kind"`   // blocks | blocked_by | relates | duplicate | duplicated_by
		Target string `json:"target"` // other operation public id
	}
	if !readJSON(w, r, &req) {
		return
	}
	tpid, ok := parseUUID(req.Target)
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid target")
		return
	}
	tid, err := s.q.GetOperationIDByPublicID(r.Context(), db.GetOperationIDByPublicIDParams{PublicID: tpid, FleetID: wid})
	if err != nil || tid == op.ID {
		httpError(w, http.StatusBadRequest, "invalid target operation")
		return
	}
	// Normalize the display-facing kind to a stored directed (source, target, kind).
	source, target, kind := op.ID, tid, ""
	switch req.Kind {
	case "blocks":
		kind = "blocks"
	case "blocked_by":
		source, target, kind = tid, op.ID, "blocks"
	case "duplicate":
		kind = "duplicate"
	case "duplicated_by":
		source, target, kind = tid, op.ID, "duplicate"
	case "relates":
		kind = "relates" // symmetric: store lowest id as source so it isn't duplicated
		if source > target {
			source, target = target, source
		}
	default:
		httpError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if _, err := s.q.CreateRelation(r.Context(), db.CreateRelationParams{FleetID: wid, SourceID: source, TargetID: target, Kind: kind}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteRelation(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if err := s.q.DeleteRelation(r.Context(), db.DeleteRelationParams{PublicID: pid, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) searchOperations(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.q.SearchOperations(r.Context(), db.SearchOperationsParams{FleetID: wid, Column2: pgtype.Text{String: q, Valid: true}})
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]operationReferenceDTO, 0, len(rows))
	for _, o := range rows {
		out = append(out, operationReferenceDTO{ID: uuidStr(o.PublicID), Title: o.Title, Status: o.Status, Sequence: o.Sequence, MissionID: uuidStr(o.MissionPublicID)})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- reactions (one current-user reaction resource per target+emoji) ----

func (s *Server) setReactionFor(w http.ResponseWriter, r *http.Request, targetType string, targetID int64, emoji string, on bool) {
	if strings.TrimSpace(emoji) == "" {
		httpError(w, http.StatusBadRequest, "emoji is required")
		return
	}
	ctx := r.Context()
	var err error
	uid := currentUser(r).ID
	if on {
		err = s.q.AddReaction(ctx, db.AddReactionParams{TargetType: targetType, TargetID: targetID, UserID: uid, Emoji: emoji})
	} else {
		err = s.q.RemoveReaction(ctx, db.RemoveReactionParams{TargetType: targetType, TargetID: targetID, UserID: uid, Emoji: emoji})
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addOperationReaction(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	_ = s.q.TouchOperation(r.Context(), db.TouchOperationParams{ID: op.ID, FleetID: wid})
	s.setReactionFor(w, r, "operation", op.ID, r.PathValue("emoji"), true)
}

func (s *Server) removeOperationReaction(w http.ResponseWriter, r *http.Request) {
	op, wid, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	_ = s.q.TouchOperation(r.Context(), db.TouchOperationParams{ID: op.ID, FleetID: wid})
	s.setReactionFor(w, r, "operation", op.ID, r.PathValue("emoji"), false)
}

func (s *Server) addReaction(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	cid, err := s.q.GetCommentIDByPublicID(r.Context(), db.GetCommentIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "comment not found")
		return
	}
	s.setReactionFor(w, r, "comment", cid, r.PathValue("emoji"), true)
}

func (s *Server) removeReaction(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	cid, err := s.q.GetCommentIDByPublicID(r.Context(), db.GetCommentIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "comment not found")
		return
	}
	s.setReactionFor(w, r, "comment", cid, r.PathValue("emoji"), false)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	op, _, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	comments, err := s.q.ListComments(r.Context(), op.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.commentDTOs(r.Context(), comments, currentUser(r).ID))
}

type postCommentReq struct {
	Body string `json:"body"`
}

func (s *Server) postComment(w http.ResponseWriter, r *http.Request) {
	op, _, ok := s.operationInFleet(w, r)
	if !ok {
		return
	}
	var req postCommentReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Body == "" {
		httpError(w, http.StatusBadRequest, "body is required")
		return
	}
	ctx := r.Context()
	uid := currentUser(r).ID
	c, err := s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: op.ID, AuthorType: "user", AuthorID: pgtype.Int8{Int64: uid, Valid: true}, Body: req.Body,
	})
	if err != nil {
		serverError(w, err)
		return
	}

	// Auto-resume: a human reply to an AI-assigned operation resumes its session with the
	// reply as the prompt — unless a run is already in flight.
	atype := ""
	if op.AssigneeType.Valid {
		atype = op.AssigneeType.String
	}
	if kind := s.resolvePilotKind(ctx, s.q, atype, op.AssigneePilotKind, op.AssigneeID); kind != "" {
		s.resumeAfterComment(ctx, op, kind, req.Body)
	}
	writeJSON(w, http.StatusCreated, s.commentDTOs(ctx, []db.Comment{c}, currentUser(r).ID)[0])
}

// resumeAfterComment queues a continuation after a human reply.
func (s *Server) resumeAfterComment(ctx context.Context, op db.Operation, kind, prompt string) bool {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	st, err := s.dispatchOrBlock(ctx, qtx, op, kind, prompt)
	if err != nil {
		return false
	}
	if err := s.setOperationStatus(ctx, qtx, op, st); err != nil {
		return false
	}
	_ = qtx.ArchiveActionRequiredForOperation(ctx, pgtype.Int8{Int64: op.ID, Valid: true})
	return tx.Commit(ctx) == nil
}

func latestUserCommentAfter(comments []db.Comment, since pgtype.Timestamptz) string {
	var body string
	if !since.Valid {
		return body
	}
	for _, c := range comments {
		if c.AuthorType == "user" && c.CreatedAt.Valid && c.CreatedAt.Time.After(since.Time) {
			body = c.Body
		}
	}
	return body
}

func (s *Server) resumePendingUserComment(ctx context.Context, op db.Operation, run db.Run) bool {
	atype := ""
	if op.AssigneeType.Valid {
		atype = op.AssigneeType.String
	}
	kind := s.resolvePilotKind(ctx, s.q, atype, op.AssigneePilotKind, op.AssigneeID)
	if kind == "" {
		return false
	}
	comments, err := s.q.ListComments(ctx, op.ID)
	if err != nil {
		return false
	}
	if prompt := latestUserCommentAfter(comments, run.CreatedAt); prompt != "" {
		return s.resumeAfterComment(ctx, op, kind, prompt)
	}
	return false
}

// ---- pilots ----

// Built-in pilot kinds shown before any fleet-specific custom pilot tags.
var builtinPilotKinds = []string{
	"claude",
	"codex",
	"antigravity",
	"cursor",
	"copilot",
	"amp",
	"opencode",
	"openclaw",
	"hermes",
	"pi",
	"kimi",
	"kiro",
}

func validPilotKind(kind string) bool {
	if len(kind) == 0 || len(kind) > 32 {
		return false
	}
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		if i == 0 {
			if c < 'a' || c > 'z' {
				return false
			}
			continue
		}
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// listPilotCapabilities reports, for each pilot kind, how many of the fleet's
// rovers it can drive and whether any is online. Drives the assign picker.
func (s *Server) listPilotCapabilities(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	rows, err := s.q.FleetPilotCapabilities(r.Context(), db.FleetPilotCapabilitiesParams{FleetID: wid, Column2: roverOnlineWindow.Seconds()})
	if err != nil {
		serverError(w, err)
		return
	}
	byKind := make(map[string]db.FleetPilotCapabilitiesRow, len(rows))
	for _, c := range rows {
		if validPilotKind(c.Kind) {
			byKind[c.Kind] = c
		}
	}
	seen := map[string]bool{}
	out := make([]pilotDTO, 0, len(builtinPilotKinds)+len(rows))
	for _, kind := range builtinPilotKinds {
		c := byKind[kind]
		out = append(out, pilotDTO{Kind: kind, Rovers: int(c.Rovers), Online: c.Online})
		seen[kind] = true
	}
	for _, c := range rows {
		if validPilotKind(c.Kind) && !seen[c.Kind] {
			out = append(out, pilotDTO{Kind: c.Kind, Rovers: int(c.Rovers), Online: c.Online})
			seen[c.Kind] = true
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- crews ----

type createCrewReq struct {
	Name string `json:"name"`
}

func (s *Server) listCrews(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	crews, err := s.q.ListCrews(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	ctx := r.Context()
	out := make([]crewDTO, 0, len(crews))
	for _, c := range crews {
		m, _ := s.q.ListCrewMembers(ctx, c.ID)
		out = append(out, crewDTO{ID: uuidStr(c.PublicID), Name: c.Name, Members: s.crewMemberDTOs(ctx, m)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCrew(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	var req createCrewReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	c, err := s.q.CreateCrew(r.Context(), db.CreateCrewParams{FleetID: wid, Name: req.Name})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, crewDTO{ID: uuidStr(c.PublicID), Name: c.Name, Members: []crewMemberDTO{}})
}

func (s *Server) patchCrew(w http.ResponseWriter, r *http.Request) {
	crewID, fleetID, ok := s.crewInFleet(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, fleetID) {
		return
	}
	var req createCrewReq
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.q.SetCrewName(r.Context(), db.SetCrewNameParams{ID: crewID, FleetID: fleetID, Name: name}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCrew(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, wid) {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	id, err := s.q.GetCrewIDByPublicID(r.Context(), db.GetCrewIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "crew not found")
		return
	}
	if err := s.q.DeleteCrew(r.Context(), db.DeleteCrewParams{ID: id, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type crewMemberReq struct {
	Role string `json:"role"`
}

func validCrewRole(role string) bool { return role == "member" || role == "captain" }

// crewInFleet verifies the {id} crew belongs to the request's fleet, returning the
// internal crew id and the fleet id.
func (s *Server) crewInFleet(w http.ResponseWriter, r *http.Request) (crewID, fleetID int64, ok bool) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return 0, 0, false
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return 0, 0, false
	}
	id, err := s.q.GetCrewIDByPublicID(r.Context(), db.GetCrewIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "crew not found")
		return 0, 0, false
	}
	return id, wid, true
}

// resolveCrewUser maps a member public id to a fleet user's internal id.
func (s *Server) resolveCrewUser(ctx context.Context, fleet int64, mid string) (int64, bool) {
	pid, ok := parseUUID(mid)
	if !ok {
		return 0, false
	}
	id, err := s.q.GetMemberUserIDByPublicID(ctx, db.GetMemberUserIDByPublicIDParams{PublicID: pid, FleetID: fleet})
	return id, err == nil
}

func (s *Server) addCrewMember(w http.ResponseWriter, r *http.Request) {
	crewID, fleetID, ok := s.crewInFleet(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, fleetID) {
		return
	}
	var req crewMemberReq
	if !readJSON(w, r, &req) {
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "member"
	}
	if !validCrewRole(role) {
		httpError(w, http.StatusBadRequest, "role must be captain or member")
		return
	}
	ctx := r.Context()
	memberType := r.PathValue("member_type")
	memberID := r.PathValue("member_id")

	// Resolve + validate before opening the tx.
	var addUser func(*db.Queries) error
	switch memberType {
	case "user":
		uid, ok := s.resolveCrewUser(ctx, fleetID, memberID)
		if !ok {
			httpError(w, http.StatusBadRequest, "member not found")
			return
		}
		addUser = func(q *db.Queries) error {
			return q.AddCrewUser(ctx, db.AddCrewUserParams{CrewID: crewID, UserID: pgtype.Int8{Int64: uid, Valid: true}, Role: role})
		}
	case "pilot":
		if !validPilotKind(memberID) {
			httpError(w, http.StatusBadRequest, "invalid pilot kind")
			return
		}
		addUser = func(q *db.Queries) error {
			return q.AddCrewPilot(ctx, db.AddCrewPilotParams{CrewID: crewID, PilotKind: pgtype.Text{String: memberID, Valid: true}, Role: role})
		}
	default:
		httpError(w, http.StatusBadRequest, "member_type must be pilot or user")
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if role == "captain" { // one captain per crew, demote + promote atomically
		if err := qtx.DemoteCrewCaptains(ctx, crewID); err != nil {
			serverError(w, err)
			return
		}
	}
	if err := addUser(qtx); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeCrewMember(w http.ResponseWriter, r *http.Request) {
	crewID, fleetID, ok := s.crewInFleet(w, r)
	if !ok {
		return
	}
	if !s.requireOwnerOrAdmin(w, r, fleetID) {
		return
	}
	mid := r.PathValue("member_id")
	switch r.PathValue("member_type") {
	case "user":
		uid, ok := s.resolveCrewUser(r.Context(), fleetID, mid)
		if !ok {
			httpError(w, http.StatusBadRequest, "member_id is required")
			return
		}
		if err := s.q.RemoveCrewUser(r.Context(), db.RemoveCrewUserParams{CrewID: crewID, UserID: pgtype.Int8{Int64: uid, Valid: true}}); err != nil {
			serverError(w, err)
			return
		}
	case "pilot":
		if err := s.q.RemoveCrewPilot(r.Context(), db.RemoveCrewPilotParams{CrewID: crewID, PilotKind: pgtype.Text{String: mid, Valid: true}}); err != nil {
			serverError(w, err)
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "member_type must be pilot or user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	runs, err := s.q.ListRuns(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.runDTOs(r.Context(), runs))
}

func (s *Server) listMissions(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	missions, err := s.q.ListMissions(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]missionDTO, 0, len(missions))
	for _, m := range missions {
		out = append(out, toMissionDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- members & invitations ----

func (s *Server) memberRole(r *http.Request, fleetID int64) string {
	role, _ := s.q.GetMemberRole(r.Context(), db.GetMemberRoleParams{UserID: currentUser(r).ID, FleetID: fleetID})
	return role
}
func isOwnerOrAdmin(role string) bool { return role == "owner" || role == "admin" }

// requireOwnerOrAdmin writes 403 and returns false unless the caller is an owner/admin
// of the fleet. Used to gate infrastructure/credential operations.
func (s *Server) requireOwnerOrAdmin(w http.ResponseWriter, r *http.Request, fleetID int64) bool {
	if !isOwnerOrAdmin(s.memberRole(r, fleetID)) {
		httpError(w, http.StatusForbidden, "owners/admins only")
		return false
	}
	return true
}

// roverOnline reports whether a rover has heartbeated within the presence window.
func (s *Server) roverOnline(ctx context.Context, roverID int64) bool {
	ls, err := s.q.RoverLastSeen(ctx, roverID)
	if err != nil || !ls.Valid {
		return false
	}
	return time.Since(ls.Time) < roverOnlineWindow
}

// isPersonalFleet reports whether a fleet is a user's immutable personal fleet.
func (s *Server) isPersonalFleet(ctx context.Context, fleetID int64) bool {
	kind, _ := s.q.GetFleetKind(ctx, fleetID)
	return kind == "personal"
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	members, err := s.q.ListMembers(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]memberDTO, 0, len(members))
	for _, m := range members {
		out = append(out, toMemberDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

type roleReq struct {
	Role string `json:"role"`
}

func (s *Server) updateMemberRole(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	uid, ok := s.pathUserID(w, r)
	if !ok {
		return
	}
	var req roleReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Role != "owner" && req.Role != "admin" && req.Role != "member" {
		httpError(w, http.StatusBadRequest, "role must be owner, admin, or member")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if err := qtx.LockFleet(ctx, wid); err != nil {
		serverError(w, err)
		return
	}
	runTestHook(func(h testHooks) func() { return h.afterRoleFleetLocked })
	if role, _ := qtx.GetMemberRole(ctx, db.GetMemberRoleParams{UserID: currentUser(r).ID, FleetID: wid}); role != "owner" {
		httpError(w, http.StatusForbidden, "only the owner can change roles")
		return
	}
	// A fleet must keep at least one owner, or it becomes unmanageable.
	if cur, _ := qtx.GetMemberRole(ctx, db.GetMemberRoleParams{UserID: uid, FleetID: wid}); cur == "owner" && req.Role != "owner" {
		if n, err := qtx.CountFleetOwners(ctx, wid); err != nil || n <= 1 {
			httpError(w, http.StatusBadRequest, "a fleet must keep at least one owner")
			return
		}
	}
	if rows, err := qtx.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{UserID: uid, FleetID: wid, Role: req.Role}); err != nil {
		serverError(w, err)
		return
	} else if rows == 0 {
		httpError(w, http.StatusNotFound, "member not found")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !isOwnerOrAdmin(s.memberRole(r, wid)) {
		httpError(w, http.StatusForbidden, "only owners/admins can remove members")
		return
	}
	uid, ok := s.pathUserID(w, r)
	if !ok {
		return
	}
	if role, _ := s.q.GetMemberRole(r.Context(), db.GetMemberRoleParams{UserID: uid, FleetID: wid}); role == "owner" {
		httpError(w, http.StatusBadRequest, "can't remove an owner")
		return
	}
	if rows, err := s.q.RemoveMember(r.Context(), db.RemoveMemberParams{UserID: uid, FleetID: wid}); err != nil {
		serverError(w, err)
		return
	} else if rows == 0 {
		httpError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !isOwnerOrAdmin(s.memberRole(r, wid)) {
		httpError(w, http.StatusForbidden, "only owners/admins can invite")
		return
	}
	if s.isPersonalFleet(r.Context(), wid) {
		httpError(w, http.StatusForbidden, "can't invite to a personal fleet")
		return
	}
	var req inviteReq
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		httpError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	role := "member"
	if req.Role == "admin" {
		role = "admin"
	}
	if u, err := s.q.GetUserByEmail(r.Context(), email); err == nil {
		if member, _ := s.q.IsMember(r.Context(), db.IsMemberParams{UserID: u.ID, FleetID: wid}); member {
			httpError(w, http.StatusConflict, "that person is already a member")
			return
		}
	}
	inv, err := s.q.CreateInvitation(r.Context(), db.CreateInvitationParams{
		FleetID: wid, InviterID: currentUser(r).ID, InviteeEmail: email, Role: role,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "already invited")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toInvitationDTO(inv))
}

func (s *Server) listInvitations(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !isOwnerOrAdmin(s.memberRole(r, wid)) {
		httpError(w, http.StatusForbidden, "only owners/admins can view invitations")
		return
	}
	inv, err := s.q.ListInvitations(r.Context(), wid)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]invitationDTO, 0, len(inv))
	for _, i := range inv {
		out = append(out, toInvitationDTO(i))
	}
	writeJSON(w, http.StatusOK, out)
}

// invitationByPath resolves the {id} invitation public id, or writes 404.
func (s *Server) invitationByPath(w http.ResponseWriter, r *http.Request) (db.Invitation, bool) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return db.Invitation{}, false
	}
	inv, err := s.q.GetInvitationByPublicID(r.Context(), pid)
	if err != nil {
		httpError(w, http.StatusNotFound, "invitation not found")
		return db.Invitation{}, false
	}
	return inv, true
}

func (s *Server) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	if !isOwnerOrAdmin(s.memberRole(r, wid)) {
		httpError(w, http.StatusForbidden, "only owners/admins can revoke invitations")
		return
	}
	inv, ok := s.invitationByPath(w, r)
	if !ok {
		return
	}
	if inv.FleetID != wid {
		httpError(w, http.StatusNotFound, "invitation not found")
		return
	}
	_ = s.q.SetInvitationStatus(r.Context(), db.SetInvitationStatusParams{ID: inv.ID, Status: "declined"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) myInvitations(w http.ResponseWriter, r *http.Request) {
	inv, err := s.q.InvitationsForEmail(r.Context(), strings.ToLower(currentUser(r).Email))
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]myInviteDTO, 0, len(inv))
	for _, i := range inv {
		out = append(out, toMyInviteDTO(i))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.invitationByPath(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if !strings.EqualFold(inv.InviteeEmail, currentUser(r).Email) {
		httpError(w, http.StatusForbidden, "this invitation isn't for you")
		return
	}
	if inv.Status != "pending" || inv.ExpiresAt.Time.Before(time.Now()) {
		httpError(w, http.StatusBadRequest, "invitation is no longer valid")
		return
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if err := qtx.CreateMembership(ctx, db.CreateMembershipParams{UserID: currentUser(r).ID, FleetID: inv.FleetID, Role: inv.Role}); err != nil {
		serverError(w, err)
		return
	}
	if err := qtx.SetInvitationStatus(ctx, db.SetInvitationStatusParams{ID: inv.ID, Status: "accepted"}); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		serverError(w, err)
		return
	}
	f, _ := s.q.GetFleetByID(ctx, inv.FleetID)
	writeJSON(w, http.StatusOK, map[string]string{"fleet_id": uuidStr(f.PublicID)})
}

func (s *Server) declineInvitation(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.invitationByPath(w, r)
	if !ok {
		return
	}
	if !strings.EqualFold(inv.InviteeEmail, currentUser(r).Email) {
		httpError(w, http.StatusForbidden, "this invitation isn't for you")
		return
	}
	_ = s.q.SetInvitationStatus(r.Context(), db.SetInvitationStatusParams{ID: inv.ID, Status: "declined"})
	w.WriteHeader(http.StatusNoContent)
}

// ---- signals ----

func (s *Server) listSignals(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	items, err := s.q.ListSignals(r.Context(), db.ListSignalsParams{FleetID: wid, RecipientUserID: currentUser(r).ID})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.signalDTOs(r.Context(), items))
}

// signalIDByPath resolves the {id} signal public id to its internal id in the fleet.
func (s *Server) signalIDByPath(w http.ResponseWriter, r *http.Request, fleetID int64) (int64, bool) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return 0, false
	}
	id, err := s.q.GetSignalIDByPublicID(r.Context(), db.GetSignalIDByPublicIDParams{PublicID: pid, FleetID: fleetID})
	if err != nil {
		httpError(w, http.StatusNotFound, "signal not found")
		return 0, false
	}
	return id, true
}

func (s *Server) patchSignal(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	id, ok := s.signalIDByPath(w, r, wid)
	if !ok {
		return
	}
	var patch map[string]json.RawMessage
	if !readJSON(w, r, &patch) {
		return
	}
	if raw, ok := patch["read"]; ok {
		read, ok := jsonBoolValue(w, raw, "read")
		if !ok {
			return
		}
		if read {
			if err := s.q.MarkSignalRead(r.Context(), db.MarkSignalReadParams{ID: id, FleetID: wid, RecipientUserID: currentUser(r).ID}); err != nil {
				serverError(w, err)
				return
			}
		}
	}
	if raw, ok := patch["archived"]; ok {
		archived, ok := jsonBoolValue(w, raw, "archived")
		if !ok {
			return
		}
		if archived {
			if err := s.q.ArchiveSignal(r.Context(), db.ArchiveSignalParams{ID: id, FleetID: wid, RecipientUserID: currentUser(r).ID}); err != nil {
				serverError(w, err)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type runDetail struct {
	Run       runDTO          `json:"run"`
	Events    []runEventDTO   `json:"events"`
	Artifacts []artifactDTO   `json:"artifacts"`
	Messages  []runMessageDTO `json:"messages"`
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, err := s.q.GetRunIDByPublicID(ctx, db.GetRunIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "run not found")
		return
	}
	run, err := s.q.GetRun(ctx, db.GetRunParams{ID: id, FleetID: wid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "run not found")
			return
		}
		serverError(w, err)
		return
	}
	events, err := s.q.ListRunEvents(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	artifacts, err := s.q.ListRunArtifacts(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	msgs, err := s.q.ListRunMessages(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	eventDTOs := make([]runEventDTO, len(events))
	for i, e := range events {
		eventDTOs[i] = toRunEventDTO(e)
	}
	artifactDTOs := make([]artifactDTO, len(artifacts))
	for i, a := range artifacts {
		artifactDTOs[i] = toArtifactDTO(a)
	}
	telemetry := make([]runMessageDTO, len(msgs))
	for i, m := range msgs {
		telemetry[i] = toRunMessageDTO(m)
	}
	writeJSON(w, http.StatusOK, runDetail{Run: s.runDTOs(ctx, []db.Run{run})[0], Events: eventDTOs, Artifacts: artifactDTOs, Messages: telemetry})
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id, err := s.q.GetRunIDByPublicID(ctx, db.GetRunIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "run not found")
		return
	}
	run, err := s.q.CancelRun(ctx, db.CancelRunParams{ID: id, FleetID: wid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusConflict, "run is not active")
			return
		}
		serverError(w, err)
		return
	}
	_, _ = s.q.AppendRunEvent(ctx, db.AppendRunEventParams{RunID: id, Kind: "status", Message: "canceled by user"})
	if op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: run.OperationID, FleetID: wid}); err == nil && op.Status == "in_progress" {
		_ = s.setOperationStatus(ctx, s.q, op, "in_review")
	}
	writeJSON(w, http.StatusOK, s.runDTOs(ctx, []db.Run{run})[0])
}

// ---- rover handlers ------------------------------------------------------

type claimedRun struct {
	ID                      string `json:"id"`           // run public id
	OperationID             string `json:"operation_id"` // operation public id
	State                   string `json:"state"`
	Pilot                   string `json:"pilot"`
	Command                 string `json:"command"`
	Prompt                  string `json:"prompt"`
	SessionID               string `json:"session_id"`
	CanProposeSubOperations bool   `json:"can_propose_sub_operations"` // captain may propose sub-operations
}

// removeRoverEnrollment lets a rover delete itself (connection-token auth) — used by `rover remove`.
func (s *Server) removeRoverEnrollment(w http.ResponseWriter, r *http.Request) {
	rv := currentRover(r)
	if err := s.q.DeleteRover(r.Context(), db.DeleteRoverParams{ID: rv.ID, FleetID: rv.FleetID}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// roverRefreshTags lets a rover update its self-reported metadata on start,
// without touching the hub-managed display name or user-set tags.
func (s *Server) roverRefreshTags(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if !readJSON(w, r, &patch) {
		return
	}
	if raw, ok := patch["units"]; ok {
		var units int
		if err := json.Unmarshal(raw, &units); err != nil {
			httpError(w, http.StatusBadRequest, "units must be a number")
			return
		}
		if units < 1 {
			httpError(w, http.StatusBadRequest, "units must be positive")
			return
		}
		if err := s.q.SetRoverUnits(r.Context(), db.SetRoverUnitsParams{ID: currentRover(r).ID, Units: int32(units)}); err != nil {
			serverError(w, err)
			return
		}
	}
	if raw, ok := patch["auto_tags"]; ok {
		var autoTags []string
		if err := json.Unmarshal(raw, &autoTags); err != nil {
			httpError(w, http.StatusBadRequest, "auto_tags must be an array")
			return
		}
		if err := s.q.SetRoverAutoTags(r.Context(), db.SetRoverAutoTagsParams{ID: currentRover(r).ID, AutoTags: normTags(autoTags)}); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": currentRover(r).Name})
}

// roverRunID resolves the {id} run public id to its internal id, scoped to the
// run the calling rover actually claimed — a rover cannot touch another rover's run.
func (s *Server) roverRunID(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	rv := currentRover(r)
	pid, ok := pathUUID(w, r)
	if !ok {
		return 0, 0, false
	}
	id, err := s.q.GetRunIDForRover(r.Context(), db.GetRunIDForRoverParams{
		PublicID: pid, FleetID: rv.FleetID, RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
	})
	if err != nil {
		httpError(w, http.StatusNotFound, "run not found")
		return 0, 0, false
	}
	return id, rv.FleetID, true
}

func (s *Server) claimRun(w http.ResponseWriter, r *http.Request) {
	rv := currentRover(r)
	ctx := r.Context()
	sub, unsubscribe := s.notifier.Subscribe(runQueuedChannel)
	defer unsubscribe()
	deadline := time.Now().Add(s.longPoll)

	for {
		run, err := s.q.ClaimNextRun(ctx, db.ClaimNextRunParams{
			FleetID: rv.FleetID,
			RoverID: pgtype.Int8{Int64: rv.ID, Valid: true},
			Column3: rv.Tags, // tag match: deny-first, then required ⊆ rover tags
		})
		if err == nil {
			s.respondClaimed(ctx, w, run)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			serverError(w, err)
			return
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		select {
		case <-sub:
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) respondClaimed(ctx context.Context, w http.ResponseWriter, run db.Run) {
	_, _ = s.q.AppendRunEvent(ctx, db.AppendRunEventParams{RunID: run.ID, Kind: "status", Message: "claimed"})
	opUUID := s.mapOperations(ctx, []int64{run.OperationID})[run.OperationID]
	resp := claimedRun{ID: uuidStr(run.PublicID), OperationID: opUUID, State: run.State, Pilot: run.Pilot, Command: run.Command}
	if run.SessionID.Valid {
		resp.SessionID = run.SessionID.String
	}
	op, opErr := s.q.GetOperation(ctx, db.GetOperationParams{ID: run.OperationID, FleetID: run.FleetID})
	if opErr == nil && op.AssigneeType.String == "crew" && !op.MainOperationID.Valid {
		resp.CanProposeSubOperations = true // top-level crew operation: captain may propose sub-operations
	}
	// A resume run carries its prompt (the human reply) in command; a first run
	// derives it from the operation.
	if run.Command != "" {
		resp.Prompt = run.Command
	} else if opErr == nil {
		resp.Prompt = op.Title
		if op.Body != "" {
			resp.Prompt += "\n\n" + op.Body
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

var validRunStates = map[string]bool{
	"starting": true, "running": true, "blocked": true,
	"succeeded": true, "failed": true, "canceled": true,
}

type setStateReq struct {
	State string `json:"state"`
}

func (s *Server) setRunState(w http.ResponseWriter, r *http.Request) {
	id, wid, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	var req setStateReq
	if !readJSON(w, r, &req) {
		return
	}
	if !validRunStates[req.State] {
		httpError(w, http.StatusBadRequest, "invalid state: "+req.State)
		return
	}
	ctx := r.Context()
	run, err := s.q.SetRunState(ctx, db.SetRunStateParams{ID: id, State: req.State, FleetID: wid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "run not found")
			return
		}
		serverError(w, err)
		return
	}
	_, _ = s.q.AppendRunEvent(ctx, db.AppendRunEventParams{RunID: id, Kind: "status", Message: req.State})

	// A planning run waits for sub-operation completion before the captain reconciles.
	if op, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: run.OperationID, FleetID: wid}); err == nil && op.Orchestrating {
		s.maybeReconvene(ctx, wid, op)
		writeJSON(w, http.StatusOK, s.runDTOs(ctx, []db.Run{run})[0])
		return
	}

	// A pilot-requested status wins; otherwise success -> in_review, failure -> blocked.
	operationStatus, ok := operationStatusForRun(req.State)
	pilotSet := run.RequestedStatus != "" && pilotSettableStatus[run.RequestedStatus]
	if pilotSet {
		operationStatus, ok = run.RequestedStatus, true
	}
	if ok {
		op, _ := s.q.GetOperation(ctx, db.GetOperationParams{ID: run.OperationID, FleetID: wid})
		var mainOperation db.Operation
		orchestratedSubOperation := false
		if op.MainOperationID.Valid {
			if p, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: op.MainOperationID.Int64, FleetID: wid}); err == nil {
				mainOperation, orchestratedSubOperation = p, p.Orchestrating
			}
		}
		_ = s.setOperationStatus(ctx, s.q, op, operationStatus)
		if pilotSet && operationStatus == "done" && !op.MainOperationID.Valid {
			s.markReviewedSubOperationsDone(ctx, wid, op.ID)
		}
		if pilotSet {
			_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
				OperationID: run.OperationID, AuthorType: "system",
				Body: "Pilot set status: " + operationStatus,
			})
		}
		settled := true // failover re-dispatch leaves the sub-operation unsettled
		switch operationStatus {
		case "in_review":
			if s.resumePendingUserComment(ctx, op, run) {
				settled = false
				break
			}
			if orchestratedSubOperation {
				break
			}
			if run.NeedsInput {
				s.notifyMembers(ctx, wid, run.OperationID, "input_requested", "action_required",
					"Needs input: "+op.Title, "A pilot is waiting for your answer to continue.")
			} else {
				s.notifyMembers(ctx, wid, run.OperationID, "review_requested", "action_required",
					"Review: "+op.Title, "A pilot finished work and needs your review.")
			}
		case "blocked":
			runFailed := req.State == "failed" || req.State == "blocked"
			if runFailed && s.crewFailover(ctx, op, run, req.State) {
				settled = false
				break
			}
			if s.resumePendingUserComment(ctx, op, run) {
				settled = false
				break
			}
			_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
				OperationID: run.OperationID, AuthorType: "system",
				Body: fmt.Sprintf("run #%d %s", run.ID, req.State),
			})
			if !orchestratedSubOperation {
				s.notifyMembers(ctx, wid, run.OperationID, "task_failed", "action_required",
					"Blocked: "+op.Title, fmt.Sprintf("run #%d %s — needs your attention.", run.ID, req.State))
			}
		default:
			if s.resumePendingUserComment(ctx, op, run) {
				settled = false
			}
		}
		if orchestratedSubOperation && settled {
			s.maybeReconvene(ctx, wid, mainOperation)
		}
	}
	writeJSON(w, http.StatusOK, s.runDTOs(ctx, []db.Run{run})[0])
}

func (s *Server) markReviewedSubOperationsDone(ctx context.Context, wid, mainOperationID int64) {
	subOperations, err := s.q.ListSubOperations(ctx, pgtype.Int8{Int64: mainOperationID, Valid: true})
	if err != nil {
		return
	}
	for _, subOperation := range subOperations {
		if subOperation.Status == "in_review" {
			_ = s.setOperationStatus(ctx, s.q, subOperation, "done")
		}
	}
}

type subOperationReq struct {
	Title    string  `json:"title"`
	Body     string  `json:"body"`
	Assignee *string `json:"assignee"` // optional crew-member pilot kind; default = the crew
}

type runResultReq struct {
	SessionID       string            `json:"session_id"`
	Message         string            `json:"message"`
	NeedsInput      bool              `json:"needs_input"`      // pilot is stuck awaiting a human answer
	OperationStatus string            `json:"operation_status"` // pilot-requested operation status (overrides default)
	SubOperations   []subOperationReq `json:"sub_operations"`   // captain splits the operation into parallel sub-operations
}

// runResult records the pilot session and posts the pilot's final message.
func (s *Server) runResult(w http.ResponseWriter, r *http.Request) {
	id, wid, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	var req runResultReq
	if !readJSONLimit(w, r, &req, maxLargeBody) {
		return
	}
	ctx := r.Context()
	run, err := s.q.GetRun(ctx, db.GetRunParams{ID: id, FleetID: wid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "run not found")
		} else {
			serverError(w, err)
		}
		return
	}
	if req.NeedsInput {
		_ = s.q.SetRunNeedsInput(ctx, db.SetRunNeedsInputParams{ID: id, FleetID: wid})
	}
	if req.OperationStatus != "" && pilotSettableStatus[req.OperationStatus] {
		_ = s.q.SetRunRequestedStatus(ctx, db.SetRunRequestedStatusParams{ID: id, FleetID: wid, RequestedStatus: req.OperationStatus})
	}
	if req.SessionID != "" {
		_ = s.q.SetRunSession(ctx, db.SetRunSessionParams{ID: id, FleetID: wid, SessionID: optText(req.SessionID)})
		_ = s.q.SetOperationSession(ctx, db.SetOperationSessionParams{
			ID: run.OperationID, FleetID: wid, PilotSessionID: optText(req.SessionID),
			PilotSessionKind: optText(run.Pilot), PilotSessionRoverID: run.RoverID,
		})
	}
	if strings.TrimSpace(req.Message) != "" {
		_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
			OperationID: run.OperationID, AuthorType: "pilot",
			AuthorPilotKind: pgtype.Text{String: run.Pilot, Valid: true}, Body: req.Message,
		})
	}
	if len(req.SubOperations) > 0 {
		s.spawnSubOperations(ctx, wid, run, req.SubOperations)
	}
	w.WriteHeader(http.StatusNoContent)
}

// spawnSubOperations creates the captain's sub-operations.
func (s *Server) spawnSubOperations(ctx context.Context, wid int64, splitRun db.Run, subOperations []subOperationReq) {
	mainOperation, err := s.q.GetOperation(ctx, db.GetOperationParams{ID: splitRun.OperationID, FleetID: wid})
	if err != nil || mainOperation.AssigneeType.String != "crew" || !mainOperation.AssigneeID.Valid || mainOperation.MainOperationID.Valid {
		return
	}
	if len(subOperations) > s.maxSubOperations {
		log.Printf("spawnSubOperations: operation %d capped %d sub-operations to %d", mainOperation.ID, len(subOperations), s.maxSubOperations)
		subOperations = subOperations[:s.maxSubOperations]
	}
	crewKinds := map[string]bool{}
	if members, err := s.q.ListCrewMembers(ctx, mainOperation.AssigneeID.Int64); err == nil {
		for _, k := range crewPilotKinds(members) {
			crewKinds[k] = true
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	created := 0
	for _, so := range subOperations {
		if strings.TrimSpace(so.Title) == "" {
			continue
		}
		atype, aid, akind := "crew", mainOperation.AssigneeID, pgtype.Text{}
		if so.Assignee != nil && crewKinds[*so.Assignee] {
			atype, aid, akind = "pilot", pgtype.Int8{}, pgtype.Text{String: *so.Assignee, Valid: true}
		}
		if _, err := s.createSubOperation(ctx, qtx, wid, mainOperation, so.Title, so.Body, atype, aid, akind); err != nil {
			return
		}
		created++
	}
	if created == 0 {
		return
	}
	if err := qtx.SetOperationOrchestrating(ctx, db.SetOperationOrchestratingParams{ID: mainOperation.ID, FleetID: wid, Orchestrating: true}); err != nil {
		return
	}
	_ = s.setOperationStatus(ctx, qtx, mainOperation, "in_progress")
	_, _ = qtx.CreateComment(ctx, db.CreateCommentParams{
		OperationID: mainOperation.ID, AuthorType: "system",
		Body: fmt.Sprintf("Captain split into %d sub-operations", created),
	})
	_ = tx.Commit(ctx)
}

// maybeReconvene queues the captain once every sub-operation has settled.
func (s *Server) maybeReconvene(ctx context.Context, wid int64, mainOperation db.Operation) {
	if !mainOperation.Orchestrating || mainOperation.AssigneeType.String != "crew" || !mainOperation.AssigneeID.Valid {
		return
	}
	n, err := s.q.CountActiveOrUnsettledSubOperations(ctx, pgtype.Int8{Int64: mainOperation.ID, Valid: true})
	if err != nil || n > 0 {
		return
	}
	if busy, err := s.q.OperationHasActiveRun(ctx, mainOperation.ID); err != nil || busy {
		return
	}
	if err := s.q.SetOperationOrchestrating(ctx, db.SetOperationOrchestratingParams{ID: mainOperation.ID, FleetID: wid, Orchestrating: false}); err != nil {
		return
	}
	captainKind := s.crewPickKind(ctx, s.q, wid, mainOperation.AssigneeID.Int64, nil, true)
	if captainKind == "" {
		_ = s.setOperationStatus(ctx, s.q, mainOperation, "blocked")
		s.notifyMembers(ctx, wid, mainOperation.ID, "no_rover", "action_required",
			"No capable rover", "Sub-operations finished but no crew rover is available to reconcile them.")
		return
	}
	run, err := s.q.CreateRun(ctx, db.CreateRunParams{
		FleetID: wid, OperationID: mainOperation.ID, MissionID: pgtype.Int8{Int64: mainOperation.MissionID, Valid: true},
		Command: s.reconcilePrompt(ctx, mainOperation), Pilot: captainKind,
	})
	if err != nil {
		return
	}
	_, _ = s.q.AppendRunEvent(ctx, db.AppendRunEventParams{RunID: run.ID, Kind: "status", Message: "queued"})
	_ = s.setOperationStatus(ctx, s.q, mainOperation, "in_progress")
	_, _ = s.q.CreateComment(ctx, db.CreateCommentParams{
		OperationID: mainOperation.ID, AuthorType: "system", Body: "Captain reconvening to reconcile sub-operations",
	})
}

// reconcilePrompt gives the captain each sub-operation result and diff.
func (s *Server) reconcilePrompt(ctx context.Context, mainOperation db.Operation) string {
	var b strings.Builder
	b.WriteString(s.contextPrompt(ctx, s.q, mainOperation))
	b.WriteString("\n--- Sub-operation results to reconcile ---\n")
	subOperations, _ := s.q.ListSubOperations(ctx, pgtype.Int8{Int64: mainOperation.ID, Valid: true})
	for _, subOperation := range subOperations {
		fmt.Fprintf(&b, "\n## %s [%s]\nSub-operation: %s\nMain operation: %s\n", subOperation.Title, subOperation.Status, uuidStr(subOperation.PublicID), uuidStr(mainOperation.PublicID))
		if comments, err := s.q.ListComments(ctx, subOperation.ID); err == nil {
			for i := len(comments) - 1; i >= 0; i-- {
				if comments[i].AuthorType == "pilot" {
					b.WriteString(comments[i].Body + "\n")
					break
				}
			}
		}
		if diff, err := s.q.LatestDiffForOperation(ctx, subOperation.ID); err == nil {
			if d := strings.TrimSpace(diff); d != "" && d != "(no changes)" {
				fmt.Fprintf(&b, "```diff\n%s\n```\n", d)
			}
		}
	}
	b.WriteString("\nReview each sub-operation above. Apply the non-overlapping change sets into this " +
		"operation's working directory, resolve any conflicts, verify, and finish. If rework is " +
		"needed, split again; if you need a human decision, ask.")
	return b.String()
}

// createSubOperation creates a sub-operation under the same mission.
func (s *Server) createSubOperation(ctx context.Context, q *db.Queries, wid int64, mainOperation db.Operation, title, body, atype string, aid pgtype.Int8, akind pgtype.Text) (db.Operation, error) {
	sequence, err := q.BumpMissionSequence(ctx, db.BumpMissionSequenceParams{ID: mainOperation.MissionID, FleetID: wid})
	if err != nil {
		return db.Operation{}, err
	}
	kind := s.resolveDispatchKind(ctx, q, wid, atype, akind, aid)
	status := "backlog"
	if kind != "" {
		status = "in_progress"
	}
	body = strings.TrimSpace(body)
	if mainOperationID := uuidStr(mainOperation.PublicID); mainOperationID != "" {
		if body != "" {
			body = fmt.Sprintf("Main operation: %s\n\n%s", mainOperationID, body)
		} else {
			body = "Main operation: " + mainOperationID
		}
	}
	subOperation, err := q.CreateOperation(ctx, db.CreateOperationParams{
		FleetID: wid, Title: title, Body: body, MissionID: mainOperation.MissionID,
		AssigneeType: optText(atype), AssigneeID: aid, AssigneePilotKind: akind, Status: status, Sequence: sequence,
		RequiredTags: []string{}, ExcludedTags: []string{},
		MainOperationID: pgtype.Int8{Int64: mainOperation.ID, Valid: true}, CreatedBy: mainOperation.CreatedBy,
	})
	if err != nil {
		return db.Operation{}, err
	}
	if kind != "" {
		if _, err := s.dispatchOrBlock(ctx, q, subOperation, kind, ""); err != nil {
			return db.Operation{}, err
		}
	}
	return subOperation, nil
}

// operationStatusForRun maps a terminal run state to an operation status. A
// successful run hands off to the human for review rather than auto-closing.
func operationStatusForRun(runState string) (string, bool) {
	switch runState {
	case "succeeded":
		return "in_review", true
	case "blocked", "failed":
		return "blocked", true
	default:
		return "", false
	}
}

// notifyMembers drops a signal for every human member of the fleet.
func (s *Server) notifyMembers(ctx context.Context, fleetID, opID int64, typ, severity, title, body string) {
	ids, err := s.q.ListFleetMemberIDs(ctx, fleetID)
	if err != nil {
		return
	}
	for _, uid := range ids {
		_, _ = s.q.CreateSignal(ctx, db.CreateSignalParams{
			FleetID: fleetID, RecipientUserID: uid,
			OperationID: pgtype.Int8{Int64: opID, Valid: true},
			Type:        typ, Severity: severity, Title: title, Body: body,
		})
	}
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	id, wid, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	if _, err := s.q.Heartbeat(r.Context(), db.HeartbeatParams{ID: id, FleetID: wid}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "run not active")
			return
		}
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type appendEventReq struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (s *Server) appendEvent(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	var req appendEventReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "log"
	}
	event, err := s.q.AppendRunEvent(r.Context(), db.AppendRunEventParams{RunID: id, Kind: req.Kind, Message: req.Message})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRunEventDTO(event))
}

type appendArtifactReq struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (s *Server) appendArtifact(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	var req appendArtifactReq
	if !readJSONLimit(w, r, &req, maxLargeBody) {
		return
	}
	if req.Kind == "" {
		req.Kind = "artifact"
	}
	artifact, err := s.q.AppendArtifact(r.Context(), db.AppendArtifactParams{RunID: id, Kind: req.Kind, Name: req.Name, Content: req.Content})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toArtifactDTO(artifact))
}

type appendRunMessageReq struct {
	Sequence int32           `json:"sequence"`
	Type     string          `json:"type"`
	Tool     string          `json:"tool"`
	Content  string          `json:"content"`
	Input    json.RawMessage `json:"input"`
	Output   string          `json:"output"`
}

// appendRunMessage records one typed transcript entry (the rover's telemetry of
// what the pilot did) for a run.
func (s *Server) appendRunMessage(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.roverRunID(w, r)
	if !ok {
		return
	}
	var req appendRunMessageReq
	if !readJSONLimit(w, r, &req, maxLargeBody) {
		return
	}
	if req.Type == "" {
		httpError(w, http.StatusBadRequest, "type is required")
		return
	}
	var input []byte
	if len(req.Input) > 0 {
		input = []byte(req.Input)
	}
	msg, err := s.q.AppendRunMessage(r.Context(), db.AppendRunMessageParams{
		RunID: id, Sequence: req.Sequence, Type: req.Type,
		Tool: optText(req.Tool), Content: optText(req.Content), Input: input, Output: optText(req.Output),
	})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRunMessageDTO(msg))
}

type missionReq struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func normalizeKey(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// createMission makes a mission: a fleet-scoped operation grouping whose key
// prefixes operation codes.
func (s *Server) createMission(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	var req missionReq
	if !readJSON(w, r, &req) {
		return
	}
	key := normalizeKey(req.Key)
	if strings.TrimSpace(req.Name) == "" || key == "" {
		httpError(w, http.StatusBadRequest, "name and an alphanumeric key are required")
		return
	}
	m, err := s.q.CreateMission(r.Context(), db.CreateMissionParams{FleetID: wid, Name: req.Name, Key: key})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "that key is already used in this fleet")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMissionDTO(m))
}

// updateMission renames a mission and/or its key. Renaming the key relabels every
// operation's displayed id with no re-indexing (display is key + per-operation sequence).
func (s *Server) updateMission(w http.ResponseWriter, r *http.Request) {
	wid, ok := s.fleetID(w, r)
	if !ok {
		return
	}
	pid, ok := pathUUID(w, r)
	if !ok {
		return
	}
	id, err := s.q.GetMissionIDByPublicID(r.Context(), db.GetMissionIDByPublicIDParams{PublicID: pid, FleetID: wid})
	if err != nil {
		httpError(w, http.StatusNotFound, "mission not found")
		return
	}
	var req missionReq
	if !readJSON(w, r, &req) {
		return
	}
	key := normalizeKey(req.Key)
	if strings.TrimSpace(req.Name) == "" || key == "" {
		httpError(w, http.StatusBadRequest, "name and an alphanumeric key are required")
		return
	}
	m, err := s.q.UpdateMission(r.Context(), db.UpdateMissionParams{ID: id, FleetID: wid, Name: req.Name, Key: key})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpError(w, http.StatusConflict, "that key is already used in this fleet")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusNotFound, "mission not found")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMissionDTO(m))
}

// StartLeaseSweeper periodically requeues runs whose rover went silent.
func (s *Server) StartLeaseSweeper(ctx context.Context, leaseSeconds float64, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ids, err := s.q.RequeueExpiredRuns(ctx, leaseSeconds)
				if err != nil {
					log.Printf("lease sweeper: %v", err)
					continue
				}
				for _, id := range ids {
					log.Printf("lease expired: requeued run %d", id)
					_, _ = s.q.AppendRunEvent(ctx, db.AppendRunEventParams{
						RunID: id, Kind: "status", Message: "requeued: lease expired (rover lost)",
					})
				}
				// A rover going silent (offline) isn't an event — detect the
				// crossing here and push a presence update so boards reflect it
				// without client polling.
				win := roverOnlineWindow.Seconds()
				if fleets, err := s.q.FleetsWithNewlyOfflineRovers(ctx, db.FleetsWithNewlyOfflineRoversParams{
					Column1: win, Column2: win + interval.Seconds() + 2,
				}); err == nil {
					for _, fid := range fleets {
						_ = s.q.NotifyFleetChanged(ctx, fid)
					}
				}
			}
		}
	}()
}

// ---- middleware ----------------------------------------------------------

// requireUser resolves the session cookie to a user, or 401.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			httpError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		user, err := s.q.GetSessionUser(r.Context(), auth.HashToken(c.Value))
		if err != nil {
			httpError(w, http.StatusUnauthorized, "session expired")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}

func currentUser(r *http.Request) db.User { return r.Context().Value(userKey).(db.User) }

// fleetID reads ?fleet=<fleet id>, resolves it to the internal fleet id, and verifies
// the current user is a member (one indexed query). The internal bigint never
// leaves the server.
func (s *Server) fleetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	pid, ok := parseUUID(r.URL.Query().Get("fleet"))
	if !ok {
		httpError(w, http.StatusBadRequest, "missing ?fleet=<fleet id>")
		return 0, false
	}
	wid, err := s.q.ResolveFleetForMember(r.Context(), db.ResolveFleetForMemberParams{PublicID: pid, UserID: currentUser(r).ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpError(w, http.StatusForbidden, "not a member of this fleet")
			return 0, false
		}
		serverError(w, err)
		return 0, false
	}
	return wid, true
}

type roverCtx struct {
	ID, FleetID int64
	Name        string
	Tags        []string // tags ∪ auto_tags — what this rover may claim against
}

// normTags canonicalizes a tag set: lowercase + trim, drop empties, dedupe (order
// preserved). Applied on every write so matching (exact set membership) is reliable.
func normTags(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func unionTags(a, b []string) []string { return normTags(append(append([]string{}, a...), b...)) }

// roverAuth resolves the bearer connection token to a rover, records presence,
// and injects the rover identity, or 401.
func (s *Server) roverAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			httpError(w, http.StatusUnauthorized, "missing connection token")
			return
		}
		rv, err := s.q.GetRoverByTokenHash(r.Context(), auth.HashToken(token))
		if err != nil {
			httpError(w, http.StatusUnauthorized, "invalid connection token")
			return
		}
		_ = s.q.TouchRover(r.Context(), rv.ID) // presence
		ctx := context.WithValue(r.Context(), roverKey, roverCtx{ID: rv.ID, FleetID: rv.FleetID, Name: rv.Name, Tags: unionTags(rv.Tags, rv.AutoTags)})
		next(w, r.WithContext(ctx))
	}
}

func currentRover(r *http.Request) roverCtx { return r.Context().Value(roverKey).(roverCtx) }

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return ""
	}
	return h[len(prefix):]
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !s.originAllowed(r, origin) {
			httpError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		if len(s.allowedOrigins) > 0 {
			// Reflect only allowlisted origins, with credentials for the web app.
			if origin != "" && s.originAllowed(r, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
		} else {
			// No allowlist (dev): same-origin via the Next proxy, so this only serves
			// direct tooling; "*" cannot carry credentials, so no cookie is exposed.
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers -------------------------------------------------------------

func optInt8(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

func optText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func queryInt(q url.Values, key string, def int64) int64 {
	if v := q.Get(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// pathUUID parses the {id} path segment as a public id.
func pathUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id, ok := parseUUID(r.PathValue("id"))
	if !ok {
		httpError(w, http.StatusBadRequest, "invalid id")
		return pgtype.UUID{}, false
	}
	return id, true
}

// pathUserID resolves the {id} user public id to its internal id.
func (s *Server) pathUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	pid, ok := pathUUID(w, r)
	if !ok {
		return 0, false
	}
	id, err := s.q.GetUserIDByPublicID(r.Context(), pid)
	if err != nil {
		httpError(w, http.StatusNotFound, "user not found")
		return 0, false
	}
	return id, true
}

const (
	maxJSONBody  = 1 << 20  // 1 MiB
	maxLargeBody = 16 << 20 // 16 MiB (artifacts / telemetry)
)

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return readJSONLimit(w, r, dst, maxJSONBody)
}

func readJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		httpError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Live data should never come from a stale browser cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}
