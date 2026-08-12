package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/JanMori/Orchestra/server/internal/analytics"
	obsmetrics "github.com/JanMori/Orchestra/server/internal/metrics"
	"github.com/JanMori/Orchestra/server/internal/util"
	db "github.com/JanMori/Orchestra/server/pkg/db/generated"
	"github.com/JanMori/Orchestra/server/pkg/protocol"
)

// ── Response types ──────────────────────────────────────────────────────────

type CrewResponse struct {
	ID            string                       `json:"id"`
	WorkspaceID   string                       `json:"workspace_id"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	Instructions  string                       `json:"instructions"`
	AvatarURL     *string                      `json:"avatar_url"`
	LeaderID      string                       `json:"leader_id"`
	CreatorID     string                       `json:"creator_id"`
	CreatedAt     string                       `json:"created_at"`
	UpdatedAt     string                       `json:"updated_at"`
	ArchivedAt    *string                      `json:"archived_at"`
	ArchivedBy    *string                      `json:"archived_by"`
	MemberCount   int                          `json:"member_count"`
	MemberPreview []CrewMemberPreviewResponse `json:"member_preview"`
}

type CrewMemberPreviewResponse struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
}

type crewMemberSummary struct {
	count   int
	preview []CrewMemberPreviewResponse
}

type CrewMemberResponse struct {
	ID         string `json:"id"`
	CrewID    string `json:"crew_id"`
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func (h *Handler) crewToResponse(s db.Crew) CrewResponse {
	return CrewResponse{
		ID:            uuidToString(s.ID),
		WorkspaceID:   uuidToString(s.WorkspaceID),
		Name:          s.Name,
		Description:   s.Description,
		Instructions:  s.Instructions,
		AvatarURL:     h.resolveAvatarURLPtr(textToPtr(s.AvatarUrl)),
		LeaderID:      uuidToString(s.LeaderID),
		CreatorID:     uuidToString(s.CreatorID),
		CreatedAt:     timestampToString(s.CreatedAt),
		UpdatedAt:     timestampToString(s.UpdatedAt),
		ArchivedAt:    timestampToPtr(s.ArchivedAt),
		ArchivedBy:    uuidToPtr(s.ArchivedBy),
		MemberPreview: []CrewMemberPreviewResponse{},
	}
}

func crewMemberToResponse(m db.CrewMember) CrewMemberResponse {
	return CrewMemberResponse{
		ID:         uuidToString(m.ID),
		CrewID:    uuidToString(m.CrewID),
		MemberType: m.MemberType,
		MemberID:   uuidToString(m.MemberID),
		Role:       m.Role,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func addCrewMemberPreview(summary *crewMemberSummary, memberType string, memberID pgtype.UUID, role string) {
	summary.count++
	if len(summary.preview) >= 3 {
		return
	}
	summary.preview = append(summary.preview, CrewMemberPreviewResponse{
		MemberType: memberType,
		MemberID:   uuidToString(memberID),
		Role:       role,
	})
}

func applyCrewMemberSummary(resp *CrewResponse, summary *crewMemberSummary) {
	if summary == nil {
		return
	}
	resp.MemberCount = summary.count
	resp.MemberPreview = summary.preview
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// canManageCrew reports whether the member may mutate the crew. Workspace
// owner/admin manage every crew; a regular member manages only the crews
// they created. Crews stay creator-scoped for management while remaining
// visible workspace-wide (ListCrews is unfiltered). Mirrors the front-end
// per-crew `canManage` gate so the UI and API agree on who can rename / add
// members / archive (ISS-4223).
func canManageCrew(member db.Member, crew db.Crew) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return uuidToString(crew.CreatorID) == uuidToString(member.UserID)
}

// memberCanWireAgent reports whether the acting member may attach the given
// agent to a crew (as leader or worker). Workspace owner/admin may wire any
// workspace agent — their management surface is unchanged. A regular member
// (a creator managing their own crew) may only wire agents they can
// @-trigger: canInvokeAgent judged as the member themselves, so public_to
// agents on their allow-list and their own private agents pass, while other
// members' private / non-allow-listed agents are rejected. This stops a
// creator from smuggling an agent they cannot invoke into a crew and reaching
// it through crew routing (ISS-4223).
func (h *Handler) memberCanWireAgent(ctx context.Context, member db.Member, agent db.Agent, workspaceID string) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	uid := uuidToString(member.UserID)
	return h.canInvokeAgent(ctx, agent, "member", uid, uid, workspaceID)
}

// loadCrewInWorkspace loads a crew scoped to the current workspace.
func (h *Handler) loadCrewInWorkspace(w http.ResponseWriter, r *http.Request) (db.Crew, string, bool) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	crewID := chi.URLParam(r, "id")
	crewUUID, ok := parseUUIDOrBadRequest(w, crewID, "crew id")
	if !ok {
		return db.Crew{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Crew{}, "", false
	}
	crew, err := h.Queries.GetCrewInWorkspace(r.Context(), db.GetCrewInWorkspaceParams{
		ID:          crewUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "crew not found")
		return db.Crew{}, "", false
	}
	return crew, workspaceID, true
}

func (h *Handler) loadCrewMemberSummary(ctx context.Context, crewID pgtype.UUID) (*crewMemberSummary, error) {
	rows, err := h.Queries.ListCrewMemberPreviewRowsByCrew(ctx, crewID)
	if err != nil {
		return nil, err
	}
	summary := &crewMemberSummary{}
	for _, row := range rows {
		addCrewMemberPreview(summary, row.MemberType, row.MemberID, row.Role)
	}
	return summary, nil
}

func (h *Handler) crewToResponseWithPreview(ctx context.Context, crew db.Crew) (CrewResponse, error) {
	resp := h.crewToResponse(crew)
	summary, err := h.loadCrewMemberSummary(ctx, crew.ID)
	if err != nil {
		return resp, err
	}
	applyCrewMemberSummary(&resp, summary)
	return resp, nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListCrews(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	crews, err := h.Queries.ListCrews(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list crews")
		return
	}

	previewRows, err := h.Queries.ListCrewMemberPreviewRows(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list crew member preview")
		return
	}
	summaries := make(map[string]*crewMemberSummary, len(crews))
	for _, row := range previewRows {
		crewID := uuidToString(row.CrewID)
		summary := summaries[crewID]
		if summary == nil {
			summary = &crewMemberSummary{}
			summaries[crewID] = summary
		}
		addCrewMemberPreview(summary, row.MemberType, row.MemberID, row.Role)
	}

	resp := make([]CrewResponse, len(crews))
	for i, s := range crews {
		resp[i] = h.crewToResponse(s)
		applyCrewMemberSummary(&resp[i], summaries[uuidToString(s.ID)])
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateCrew(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	// Any workspace member can create a crew and becomes its creator
	// (CreatorID below). This aligns crews with agents/projects, which are
	// also member-creatable; management stays creator-scoped (ISS-4223).
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		LeaderID    string  `json:"leader_id"`
		AvatarURL   *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.LeaderID == "" {
		writeError(w, http.StatusBadRequest, "leader_id is required")
		return
	}

	leaderUUID, ok := parseUUIDOrBadRequest(w, req.LeaderID, "leader_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Validate leader is an agent in this workspace.
	leaderAgent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          leaderUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "leader must be a valid agent in this workspace")
		return
	}
	// A non-admin creator may only lead their crew with an agent they can
	// @-trigger; admins may wire any workspace agent (ISS-4223).
	if !h.memberCanWireAgent(r.Context(), member, leaderAgent, workspaceID) {
		writeError(w, http.StatusForbidden, "you can only use an agent you have access to as leader")
		return
	}

	avatarURL := pgtype.Text{}
	if req.AvatarURL != nil {
		accepted, ok := h.acceptAvatarURL(w, r, *req.AvatarURL, "")
		if !ok {
			return
		}
		avatarURL = pgtype.Text{String: accepted, Valid: true}
	}

	crew, err := h.Queries.CreateCrew(r.Context(), db.CreateCrewParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Description: req.Description,
		LeaderID:    leaderUUID,
		CreatorID:   member.UserID,
		AvatarUrl:   avatarURL,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create crew")
		return
	}

	// Auto-add leader as a member with role "leader".
	h.Queries.AddCrewMember(r.Context(), db.AddCrewMemberParams{
		CrewID:    crew.ID,
		MemberType: "agent",
		MemberID:   leaderUUID,
		Role:       "leader",
	})

	resp, err := h.crewToResponseWithPreview(r.Context(), crew)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load crew member preview")
		return
	}
	h.publish(protocol.EventCrewCreated, workspaceID, "member", uuidToString(member.UserID), map[string]any{"crew": resp})
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.CrewCreated(
		uuidToString(member.UserID),
		workspaceID,
		uuidToString(crew.ID),
		1,
	))
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetCrew(w http.ResponseWriter, r *http.Request) {
	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	resp, err := h.crewToResponseWithPreview(r.Context(), crew)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load crew member preview")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateCrew(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageCrew(member, crew) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		Instructions *string `json:"instructions"`
		LeaderID     *string `json:"leader_id"`
		AvatarURL    *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateCrewParams{ID: crew.ID}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Instructions != nil {
		params.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	if req.AvatarURL != nil {
		accepted, ok := h.acceptAvatarURL(w, r, *req.AvatarURL, crew.AvatarUrl.String)
		if !ok {
			return
		}
		params.AvatarUrl = pgtype.Text{String: accepted, Valid: true}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update crew")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Autopilot assignment takes FOR SHARE on the crew before locking its
	// leader Agent. Take the exclusive side in the same order so leader
	// rotation and active Autopilot saves cannot leave an automation pointing
	// at an unbound effective Agent.
	if _, err := qtx.LockCrewForUpdate(r.Context(), db.LockCrewForUpdateParams{
		ID:          crew.ID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update crew")
		return
	}

	newLeaderRuntimeBound := true
	if req.LeaderID != nil {
		lid, ok := parseUUIDOrBadRequest(w, *req.LeaderID, "leader_id")
		if !ok {
			return
		}
		// Stabilize runtime_id through commit. Runtime teardown takes FOR UPDATE
		// on this row and follows the same Agent→Autopilot lock order, so
		// whichever operation starts first produces a complete result.
		newLeader, err := qtx.LockAgentForAutopilotAssignment(r.Context(), db.LockAgentForAutopilotAssignmentParams{
			ID:          lid,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "leader must be a valid agent in this workspace")
			return
		}
		// A non-admin creator may only promote an agent they can @-trigger.
		if !h.memberCanWireAgent(r.Context(), member, newLeader, workspaceID) {
			writeError(w, http.StatusForbidden, "you can only use an agent you have access to as leader")
			return
		}
		// Ensure new leader is a crew member; auto-add if not.
		isMember, err := qtx.IsCrewMember(r.Context(), db.IsCrewMemberParams{
			CrewID: crew.ID, MemberType: "agent", MemberID: lid,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update crew")
			return
		}
		if !isMember {
			if _, err := qtx.AddCrewMember(r.Context(), db.AddCrewMemberParams{
				CrewID: crew.ID, MemberType: "agent", MemberID: lid, Role: "leader",
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update crew")
				return
			}
		}
		params.LeaderID = lid
		newLeaderRuntimeBound = newLeader.RuntimeID.Valid
	}

	updated, err := qtx.UpdateCrew(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update crew")
		return
	}
	var pausedAutopilots []db.Autopilot
	if req.LeaderID != nil && !newLeaderRuntimeBound {
		pausedAutopilots, err = qtx.PauseAutopilotsByUnrunnableCrew(r.Context(), crew.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update crew")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update crew")
		return
	}

	resp, err := h.crewToResponseWithPreview(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load crew member preview")
		return
	}
	h.publish(protocol.EventCrewUpdated, workspaceID, "member", requestUserID(r), map[string]any{"crew": resp})
	for _, autopilot := range pausedAutopilots {
		h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", requestUserID(r), map[string]any{
			"autopilot": autopilotToResponse(autopilot, nil),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteCrew(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageCrew(member, crew) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if crew.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "crew is already archived")
		return
	}

	// Transfer issues assigned to this crew to the leader agent.
	if err := h.Queries.TransferCrewAssignees(r.Context(), db.TransferCrewAssigneesParams{
		AssigneeID:   crew.ID,
		AssigneeID_2: crew.LeaderID,
	}); err != nil {
		slog.Warn("transfer crew assignees failed", "crew_id", uuidToString(crew.ID), "error", err)
	}

	// Mirror the issue-assignee transfer for autopilots that target this
	// crew. Without this, autopilot.assignee_id would still point at the
	// archived crew row and every subsequent dispatch would skip with
	// "assignee crew is archived" — visible to ops but useless to the
	// owner. Rewriting to the leader keeps the autopilot semantics
	// unchanged (Path A from ISS-2429 is leader-only execution anyway).
	if err := h.Queries.TransferCrewAutopilotsToLeader(r.Context(), db.TransferCrewAutopilotsToLeaderParams{
		AssigneeID:   crew.ID,
		AssigneeID_2: crew.LeaderID,
	}); err != nil {
		slog.Warn("transfer crew autopilots failed", "crew_id", uuidToString(crew.ID), "error", err)
	}

	userID := requestUserID(r)
	userUUID, _ := parseUUIDOrBadRequest(w, userID, "user_id")

	if _, err := h.Queries.ArchiveCrew(r.Context(), db.ArchiveCrewParams{
		ID:         crew.ID,
		ArchivedBy: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive crew")
		return
	}

	h.publish(protocol.EventCrewDeleted, workspaceID, "member", userID, map[string]any{
		"crew_id":  uuidToString(crew.ID),
		"leader_id": uuidToString(crew.LeaderID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Crew Members ───────────────────────────────────────────────────────────

func (h *Handler) ListCrewMembers(w http.ResponseWriter, r *http.Request) {
	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListCrewMembers(r.Context(), crew.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list crew members")
		return
	}
	resp := make([]CrewMemberResponse, len(members))
	for i, m := range members {
		resp[i] = crewMemberToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── Crew Member Status ────────────────────────────────────────────────────

// CrewMemberStatus is the per-member entry in the crew member status
// response. Agent members carry a derived working/idle/offline/unstable
// status plus any active issues; human members are returned with member_type
// only so the front-end can render them in the same list without
// reordering.
type CrewMemberStatusResponse struct {
	MemberType   string                  `json:"member_type"`
	MemberID     string                  `json:"member_id"`
	Status       *string                 `json:"status"`
	ActiveIssues []CrewActiveIssueBrief `json:"active_issues"`
	LastActiveAt *string                 `json:"last_active_at"`
}

type CrewActiveIssueBrief struct {
	IssueID     string `json:"issue_id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	IssueStatus string `json:"issue_status"`
}

type CrewMemberStatusListResponse struct {
	Members []CrewMemberStatusResponse `json:"members"`
}

// deriveCrewMemberStatus collapses runtime + task signals into the five
// status buckets used by the crew UI. Mirrors the workload+availability
// split in packages/core/agents/derive-presence.ts: working wins over
// runtime health (an agent that is in the middle of dispatched/running
// work counts as working even if the runtime briefly drops), then
// availability buckets decide between idle / unstable / offline.
//
// Thresholds match deriveRuntimeHealth: any offline runtime whose
// last_seen_at is within the last 5 minutes is reported as "unstable" so
// the crew UI surfaces transient drops the same way the agent dot does.
//
// Archived agents always report `archived` regardless of any leftover
// runtime row or task — they should appear in the list but never look
// like they're still working or merely offline (a leftover online
// runtime row would otherwise read as "offline" and hide the fact that
// the agent has been archived). Per the RFC decision (see ISS-2319), we
// surface archived agents in this endpoint rather than filtering them
// out in the SQL.
func deriveCrewMemberStatus(
	archived bool,
	runtimeStatus pgtype.Text,
	lastSeen pgtype.Timestamptz,
	hasActiveTask bool,
	now time.Time,
) string {
	if archived {
		return "archived"
	}
	if hasActiveTask {
		return "working"
	}
	if !runtimeStatus.Valid {
		return "offline"
	}
	if runtimeStatus.String == "online" {
		return "idle"
	}
	if !lastSeen.Valid {
		return "offline"
	}
	if now.Sub(lastSeen.Time) < 5*time.Minute {
		return "unstable"
	}
	return "offline"
}

// ListCrewMemberStatus returns one entry per crew member with derived
// status, the issues each agent member is currently running, and the last
// observed runtime activity. The endpoint is read-only and inherits the
// workspace-membership guard from the route middleware — any member of the
// workspace can read it.
func (h *Handler) ListCrewMemberStatus(w http.ResponseWriter, r *http.Request) {
	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListCrewMemberStatusRows(r.Context(), crew.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list crew member status")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), crew.WorkspaceID)
	now := time.Now()

	// Group rows by member_id while preserving the SQL ORDER BY (crew_member
	// insertion order). One member may appear in multiple rows when they have
	// more than one active task.
	type memberAcc struct {
		response       CrewMemberStatusResponse
		archived       bool
		hasActiveTask  bool
		runtimeStatus  pgtype.Text
		runtimeSeenAt  pgtype.Timestamptz
		latestActiveAt pgtype.Timestamptz
	}
	order := make([]string, 0, len(rows))
	acc := make(map[string]*memberAcc, len(rows))

	for _, row := range rows {
		memberID := uuidToString(row.MemberID)
		entry, exists := acc[memberID]
		if !exists {
			entry = &memberAcc{
				response: CrewMemberStatusResponse{
					MemberType:   row.MemberType,
					MemberID:     memberID,
					ActiveIssues: []CrewActiveIssueBrief{},
				},
				archived:      row.AgentArchivedAt.Valid,
				runtimeStatus: row.RuntimeStatus,
				runtimeSeenAt: row.RuntimeLastSeenAt,
			}
			acc[memberID] = entry
			order = append(order, memberID)
		}

		if row.MemberType != "agent" {
			continue
		}

		// A dispatched/running task occupies an agent slot even when it
		// has no associated issue (chat / quick-create tasks set
		// agent_task_queue.issue_id = NULL). The `working` bucket is
		// defined by task presence, not by whether we can render an
		// issue link, so flag the agent here regardless of issue_id.
		if row.TaskID.Valid {
			entry.hasActiveTask = true

			if row.TaskIssueID.Valid {
				brief := CrewActiveIssueBrief{
					IssueID:    uuidToString(row.TaskIssueID),
					Identifier: prefix + "-" + strconv.Itoa(int(row.IssueNumber.Int32)),
					Title:      row.IssueTitle.String,
					IssueStatus: func() string {
						if row.IssueStatus.Valid {
							return row.IssueStatus.String
						}
						return ""
					}(),
				}
				entry.response.ActiveIssues = append(entry.response.ActiveIssues, brief)
			}

			if row.TaskDispatchedAt.Valid && (!entry.latestActiveAt.Valid ||
				row.TaskDispatchedAt.Time.After(entry.latestActiveAt.Time)) {
				entry.latestActiveAt = row.TaskDispatchedAt
			}
		}
	}

	resp := CrewMemberStatusListResponse{
		Members: make([]CrewMemberStatusResponse, 0, len(order)),
	}
	for _, id := range order {
		entry := acc[id]
		if entry.response.MemberType == "agent" {
			status := deriveCrewMemberStatus(
				entry.archived,
				entry.runtimeStatus,
				entry.runtimeSeenAt,
				entry.hasActiveTask,
				now,
			)
			entry.response.Status = &status
			// last_active_at prefers the freshest active-task dispatch
			// over the runtime heartbeat: a working agent should not
			// look stale because the runtime heartbeat is a few seconds
			// behind. Falls back to runtime last_seen_at otherwise.
			if entry.latestActiveAt.Valid {
				entry.response.LastActiveAt = timestampToPtr(entry.latestActiveAt)
			} else if entry.runtimeSeenAt.Valid {
				entry.response.LastActiveAt = timestampToPtr(entry.runtimeSeenAt)
			}
		}
		resp.Members = append(resp.Members, entry.response)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddCrewMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageCrew(member, crew) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MemberType != "agent" && req.MemberType != "member" {
		writeError(w, http.StatusBadRequest, "member_type must be 'agent' or 'member'")
		return
	}
	if req.MemberID == "" {
		writeError(w, http.StatusBadRequest, "member_id is required")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	// Validate the member belongs to this workspace.
	if req.MemberType == "agent" {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: memberUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "agent not found in this workspace")
			return
		}
		// A non-admin creator may only add agents they can @-trigger (public
		// or their own / allow-listed agents); admins may add any workspace
		// agent (ISS-4223).
		if !h.memberCanWireAgent(r.Context(), member, agent, workspaceID) {
			writeError(w, http.StatusForbidden, "you can only add an agent you have access to")
			return
		}
	} else {
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID: memberUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "member not found in this workspace")
			return
		}
	}

	sm, err := h.Queries.AddCrewMember(r.Context(), db.AddCrewMemberParams{
		CrewID:    crew.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
		Role:       req.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "member already in crew")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add crew member")
		return
	}

	writeJSON(w, http.StatusCreated, crewMemberToResponse(sm))
	h.publish(protocol.EventCrewUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"crew_id": uuidToString(crew.ID),
	})
}

func (h *Handler) RemoveCrewMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageCrew(member, crew) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	// Prevent removing the leader.
	if req.MemberType == "agent" && uuidToString(crew.LeaderID) == req.MemberID {
		writeError(w, http.StatusBadRequest, "cannot remove the crew leader; change leader first")
		return
	}

	rows, err := h.Queries.RemoveCrewMember(r.Context(), db.RemoveCrewMemberParams{
		CrewID:    crew.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove crew member")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "crew member not found")
		return
	}

	h.publish(protocol.EventCrewUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"crew_id": uuidToString(crew.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateCrewMemberRole(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	crew, _, ok := h.loadCrewInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageCrew(member, crew) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	sm, err := h.Queries.UpdateCrewMemberRole(r.Context(), db.UpdateCrewMemberRoleParams{
		CrewID:    crew.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
		Role:       req.Role,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "crew member not found")
		return
	}

	h.publish(protocol.EventCrewUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"crew_id": uuidToString(crew.ID),
	})
	writeJSON(w, http.StatusOK, crewMemberToResponse(sm))
}

// ── Crew Leader Evaluation ──────────────────────────────────────────────────

// RecordCrewLeaderEvaluation records a crew leader's evaluation decision
// into the unified activity_log. Called by the leader agent via CLI after
// each trigger to record whether it took action, stayed silent, or failed.
func (h *Handler) RecordCrewLeaderEvaluation(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req struct {
		Outcome string `json:"outcome"` // action | no_action | failed
		Reason  string `json:"reason"`  // short explanation from leader
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Outcome != "action" && req.Outcome != "no_action" && req.Outcome != "failed" {
		writeError(w, http.StatusBadRequest, "outcome must be 'action', 'no_action', or 'failed'")
		return
	}

	// The issue must be assigned to a crew.
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "crew" || !issue.AssigneeID.Valid {
		writeError(w, http.StatusBadRequest, "issue is not assigned to a crew")
		return
	}

	crew, err := h.Queries.GetCrewInWorkspace(r.Context(), db.GetCrewInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "crew not found")
		return
	}

	// Security: only the crew leader agent can record evaluations.
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" || actorID != uuidToString(crew.LeaderID) {
		writeError(w, http.StatusForbidden, "only the crew leader agent can record evaluations")
		return
	}

	taskID := r.Header.Get("X-Task-ID")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil || !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusBadRequest, "task does not belong to issue")
		return
	}

	details, _ := json.Marshal(map[string]string{
		"crew_id": uuidToString(crew.ID),
		"task_id":  util.UUIDToString(taskUUID),
		"outcome":  req.Outcome,
		"reason":   req.Reason,
	})

	activity, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     crew.LeaderID,
		Action:      "crew_leader_evaluated",
		Details:     details,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record evaluation")
		return
	}

	h.publish(protocol.EventActivityCreated, uuidToString(issue.WorkspaceID), "agent", actorID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": "agent",
			"actor_id":   actorID,
			"action":     activity.Action,
			"details":    json.RawMessage(details),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":         uuidToString(activity.ID),
		"action":     activity.Action,
		"created_at": timestampToString(activity.CreatedAt),
	})
}

// ── Crew Trigger Logic ─────────────────────────────────────────────────────

// shouldSuppressCrewLeaderSelfTrigger reports whether a crew leader's own
// comment should be blocked from re-enqueuing that same leader. The only
// leader-authored non-leader task allowed to wake the assigned leader is a
// same-crew worker task; generic agent tasks such as direct mentions and
// thread-parent replies are not worker-role proof and must not self-trigger.
func (h *Handler) shouldSuppressCrewLeaderSelfTrigger(ctx context.Context, issueID, leaderID, crewID pgtype.UUID) bool {
	latest, err := h.Queries.GetLatestTaskRoleForIssueAndAgent(ctx, db.GetLatestTaskRoleForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: leaderID,
	})
	if err != nil {
		return false
	}
	if latest.IsLeaderTask {
		return true
	}
	return !latest.CrewID.Valid || uuidToString(latest.CrewID) != uuidToString(crewID)
}

// commentMentionsAnyone returns true when the comment body contains at least
// one routing-style mention — [@Name](mention://agent|member|crew|all/<id>).
// Issue cross-references (mention://issue/...) are ignored because they are
// not directed at a participant. Only the current comment is inspected —
// parent (thread root) mentions are NOT inherited here.
func commentMentionsAnyone(content string) bool {
	for _, m := range util.ParseMentions(content) {
		switch m.Type {
		case "agent", "member", "crew", "all":
			return true
		}
	}
	return false
}

// The crew-leader assign/promotion readiness decision now lives in the single
// service.IssueService.WillEnqueueRun predicate (ISS-3375), shared by the issue
// write paths and the preview endpoint. The former handler-local mirrors
// (shouldEnqueueCrewLeaderOnAssign / isCrewLeaderReady) were removed to stop
// the four-entry-point drift. The crew enqueue side effect still flows through
// enqueueCrewLeaderTask below, which keeps the leader access gate and pending
// dedup in one place.

// enqueueCrewLeaderTask triggers the crew leader agent for an issue assigned
// to a crew. Assign and backlog-promotion paths use this directly; comment
// paths go through computeCommentAgentTriggers so preview and create share the
// same trigger set.
// enqueueCrewLeaderTask returns true when it actually enqueued a leader task
// (so the caller can record a handoff trace only on a real run start).
func (h *Handler) enqueueCrewLeaderTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, authorType, authorID, handoffNote string) bool {
	crew, err := h.Queries.GetCrewInWorkspace(ctx, db.GetCrewInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}

	// The gate must judge the SAME top-of-chain human the enqueue path will
	// persist on the leader task row, or it drifts: an agent-created issue that
	// correctly inherits its originator (ISS-4305) would still be denied here
	// if the gate used an empty originator. Member authors are their own
	// originator; for agent/system-triggered assigns we resolve the originator
	// exactly like EnqueueTaskForCrewLeader* does (via the issue's origin
	// link). triggerCommentID is always empty on the assign/promote path, so we
	// pass an invalid UUID to match. A still-unresolved originator leaves
	// leaderOriginator empty, which correctly fails closed for member/team
	// targets while a workspace target still admits the agent principal.
	leaderOriginator := ""
	if authorType == "member" {
		leaderOriginator = authorID
	} else {
		leaderOriginator = uuidToString(h.TaskService.OriginatorForIssueTask(ctx, issue, pgtype.UUID{}))
	}
	if !h.canEnqueueCrewLeader(ctx, crew.LeaderID, authorType, authorID, leaderOriginator, uuidToString(issue.WorkspaceID)) {
		return false
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: crew.LeaderID,
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID),
	})
	if err != nil || hasPending {
		return false
	}

	// triggerCommentID is always empty on the assign/promote path; the handoff
	// note rides its own task column, never trigger_comment_id.
	_ = triggerCommentID
	// The member who performed the assign/promote is the accountable human for the
	// leader run (ISS-4302 §4) — the same principal the gate above judged. An agent
	// author is not a human, so only a member actor is threaded.
	if _, err := h.TaskService.EnqueueTaskForCrewLeaderWithHandoff(ctx, issue, crew.LeaderID, crew.ID, handoffNote, memberActorUserID(authorType, authorID)); err != nil {
		slog.Warn("enqueue crew leader task failed",
			"issue_id", uuidToString(issue.ID),
			"crew_id", uuidToString(crew.ID),
			"leader_id", uuidToString(crew.LeaderID),
			"error", err)
		return false
	}
	return true
}
