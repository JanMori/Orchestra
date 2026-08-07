export type CrewMemberType = "agent" | "member";

export type CrewActivityOutcome = "action" | "no_action" | "failed";

export interface CrewMemberPreview {
  member_type: CrewMemberType;
  member_id: string;
  role: string;
}

export interface Crew {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  leader_id: string;
  creator_id: string;
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
  member_count?: number;
  member_preview?: CrewMemberPreview[];
}

export interface CrewMember {
  id: string;
  crew_id: string;
  member_type: CrewMemberType;
  member_id: string;
  role: string;
  created_at: string;
}

export interface CrewActivityLog {
  id: string;
  crew_id: string;
  issue_id: string;
  trigger_comment_id: string | null;
  leader_id: string;
  outcome: CrewActivityOutcome;
  details: unknown;
  created_at: string;
}

export interface CreateCrewRequest {
  name: string;
  description?: string;
  leader_id: string;
  avatar_url?: string;
}

export interface UpdateCrewRequest {
  name?: string;
  description?: string;
  instructions?: string;
  leader_id?: string;
  avatar_url?: string;
}

export interface AddCrewMemberRequest {
  member_type: CrewMemberType;
  member_id: string;
  role?: string;
}

export interface RemoveCrewMemberRequest {
  member_type: CrewMemberType;
  member_id: string;
}

export interface UpdateCrewMemberRoleRequest {
  member_type: CrewMemberType;
  member_id: string;
  role: string;
}

export interface CreateCrewActivityLogRequest {
  crew_id: string;
  issue_id: string;
  trigger_comment_id?: string;
  outcome: CrewActivityOutcome;
  details?: unknown;
}

// CrewMemberStatus mirrors the five-way bucket the back-end derives in
// handler/crew.go::deriveCrewMemberStatus. Kept as a string union here
// (rather than re-derived from snapshot data) so the crew page can render
// the freshest server-side judgement without re-fetching the agent
// snapshot / runtime list. `archived` wins over every runtime/task signal.
export type CrewMemberStatusValue =
  | "working"
  | "idle"
  | "offline"
  | "unstable"
  | "archived";

export interface CrewActiveIssueBrief {
  issue_id: string;
  identifier: string;
  title: string;
  issue_status: string;
}

export interface CrewMemberStatus {
  member_type: CrewMemberType;
  member_id: string;
  // Human members are returned with status === null so the UI can render
  // them in the same list without showing a status pill (v1 has no
  // presence signal for humans).
  status: CrewMemberStatusValue | null;
  active_issues: CrewActiveIssueBrief[];
  last_active_at: string | null;
}

export interface CrewMemberStatusListResponse {
  members: CrewMemberStatus[];
}
