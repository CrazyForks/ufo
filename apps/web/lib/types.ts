export type User = { id: string; email: string; name: string; created_at: string; updated_at: string };
export type UserProfile = { id: string; name: string; fleets: Fleet[] };
export type Fleet = { id: string; name: string; kind: string; metadata: Record<string, unknown>; created_at: string; updated_at: string };
export type Member = { id: string; email: string; name: string; role: string; created_at: string; updated_at: string };
export type Invitation = { id: string; invitee_email: string; role: string; status: string; created_at: string; updated_at: string; expires_at: string };
export type MyInvite = { id: string; fleet_id: string; fleet_name: string; role: string; invitee_email: string };
export type Mission = {
  id: string;
  name: string;
  key: string;
  forge_ids: string[];
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};
export type ForgeCredentialKind = "rover_env" | "github_app" | "gitlab_app" | "secret_ref";
export type Forge = {
  id: string;
  key: string;
  name: string;
  provider: "github" | "gitlab" | string;
  base_url: string;
  repo: string;
  default_base_branch: string;
  credential_kind: ForgeCredentialKind | string;
  credential: Record<string, unknown>;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};
export type ForgeInput = {
  key: string;
  name?: string;
  provider: "github" | "gitlab";
  base_url?: string;
  repo: string;
  default_base_branch?: string;
  credential_kind?: ForgeCredentialKind;
  credential?: Record<string, unknown>;
};
export type Pilot = { kind: string; rovers: number; online_rovers: number };
export type CrewMember = { member_type: string; member_id: string; role: string; created_at: string; updated_at: string };
export type Crew = { id: string; name: string; created_at: string; updated_at: string; members?: CrewMember[] };
export type Label = { id: string; name: string; color: string; created_at: string; updated_at: string };
export type SkillFile = { path: string; content: string; size_bytes: number; created_at: string; updated_at: string };
export type Skill = { id: string; name: string; slug: string; description: string; archived: boolean; files: SkillFile[]; created_at: string; updated_at: string };
export type AssigneeType = "pilot" | "user" | "crew";
export type RoutineTriggerType = "manual" | "schedule";
export type RoutineMetadata = {
  trigger?: { kind?: RoutineTriggerType; cron?: string; enabled?: boolean };
  operation?: {
    pulse?: {
      start_immediately?: boolean;
      skip_if_active?: boolean;
      re_pulse_on_close?: boolean;
    };
    auto_commit?: {
      branch?: string;
      drop_worktree?: boolean;
    };
    ship_base?: {
      branch?: string;
      reference?: string;
      sync?: "rebase" | "merge" | "reset" | string;
    };
    forge?: {
      key?: string;
    };
    pull_request?: {
      create?: boolean;
      labels?: string[];
      ci_wait_timeout_seconds?: number | null;
    };
    checks?: { commands?: string[]; timeout_seconds?: number };
    priority?: number;
    assignee?: { type?: AssigneeType; id?: string };
    required_tags?: string[];
    excluded_tags?: string[];
  };
  [key: string]: unknown;
};
export type RoutineOperationMetadata = {
  context?: string;
  [key: string]: unknown;
};
export type Routine = {
  id: string;
  mission_id: string;
  title: string;
  body: string;
  metadata: RoutineMetadata;
  operation_metadata: RoutineOperationMetadata;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  next_pulse_at: string | null;
  last_pulsed_at: string | null;
};
export type Pulse = {
  id: string;
  routine_id: string;
  operation_id: string | null;
  status: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  finished_at: string | null;
};
export type OperationReference = { id: string; title: string; status: string; sequence: number; mission_id: string };
export type RelationKind = "blocks" | "blocked_by" | "relates" | "duplicate" | "duplicated_by";
export type Relation = { id: string; kind: RelationKind; operation: OperationReference; created_by: string | null; created_at: string };
export type SourceAction = {
  id: string;
  operation_id: string;
  run_id: string | null;
  rover_id: string | null;
  kind: "apply_to_source" | "create_source_branch" | "commit_to_branch" | "refresh_from_source";
  status: "queued" | "accepted" | "succeeded" | "failed" | "conflicted";
  branch_name: string;
  commit_sha: string;
  base_sha: string;
  source_head_sha: string;
  message: string;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  accepted_at: string | null;
  finished_at: string | null;
};
export type PullRequest = {
  id: string;
  url: string;
  title: string;
  status: string;
  number?: number | null;
  provider?: string;
  created_by_ufo?: boolean;
  head_sha?: string;
  ci_status?: string;
  head_branch?: string;
  base_branch?: string;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
};
export type SubOperationProgress = { total: number; done: number; in_progress: number; in_review: number; blocked: number; pilot_kinds: string[] };
export type Operation = {
  id: string;
  title: string;
  body: string;
  status: string;
  active_run_status: string;
  mission_id: string;
  sequence: number;
  priority: number;
  assignee_type: string | null;
  assignee_id: string | null;
  assignee_pilot_kind: string | null;
  required_tags: string[];
  excluded_tags: string[];
  labels: Label[];
  reactions: Reaction[];
  sub_operation_progress: SubOperationProgress;
  metadata: Record<string, unknown>;
  sub_operations_enabled: boolean;
  start_date: string | null;
  due_date: string | null;
  main_operation_id: string | null;
  orchestrating: boolean;
  archived: boolean;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  started_at: string | null;
  finished_at: string | null;
};
export type Reaction = { emoji: string; count: number; mine: boolean; users: string[] };
export type Comment = {
  id: string;
  author_type: string;
  author_id: string | null;
  author_pilot_kind: string | null;
  body: string;
  reactions: Reaction[];
  created_at: string;
  updated_at: string;
};
export type RunUsage = {
  provider?: string;
  model?: string;
  source?: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  reasoning_tokens?: number;
  total_tokens: number;
  duration_ms?: number | null;
  cost_micros?: number | null;
  metadata?: Record<string, unknown>;
  created_at: string;
};

export type UsageTotals = {
  runs: number;
  total_tokens: number;
  cost_micros: number;
  max_runs?: number | null;
  max_tokens?: number | null;
  max_usd_micros?: number | null;
};

export type MissionUsage = UsageTotals & {
  id: string;
  key: string;
  name: string;
};

export type UsageSummary = {
  period: "calendar_week" | "calendar_month";
  period_key: string;
  start_at: string;
  end_at: string;
  fleet: UsageTotals;
  missions: MissionUsage[];
};
export type Run = {
  id: string;
  operation_id: string;
  pilot?: string;
  status: string;
  needs_input?: boolean;
  metadata: Record<string, unknown>;
  usage?: RunUsage | null;
  created_at: string;
  updated_at: string;
};
export type RunEvent = { kind: string; message: string; created_at: string };
export type Asset = {
  id: string;
  filename: string;
  content_type: string;
  byte_size: number;
  checksums?: Record<string, string>;
  url: string;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
};
export type AssetUploadIntent = {
  asset_id: string;
  method: string;
  url: string;
  headers: Record<string, string>;
  expires_at: string;
};
export type Artifact = {
  id: string;
  asset_id: string | null;
  kind: string;
  name: string;
  content: string;
  content_type: string;
  byte_size: number;
  checksums?: Record<string, string>;
  metadata: Record<string, unknown>;
  created_at: string;
};
export type RunMessage = {
  sequence: number;
  type: "text" | "thinking" | "tool_use" | "tool_result" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown> | null;
  output?: string;
  created_at: string;
};
export type Rover = { id: string; fleet_id?: string; fleet_name?: string; name: string; status: string; units: number; running_units: number; auto_tags: string[]; tags: string[]; metadata: Record<string, unknown>; created_at: string; updated_at: string; last_seen_at?: string };
export type EnrollmentCode = {
  id: string;
  fleet_id?: string;
  code: string;
  kind: "code:approved" | "web:pending" | "web:approved" | "web:denied";
  name: string;
  remaining_uses: number;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  expires_at: string | null;
};
export type Signal = {
  id: string;
  operation_id: string | null;
  type: string;
  severity: string;
  title: string;
  body: string;
  read: boolean;
  created_at: string;
  updated_at: string;
};

export type ForgeAction = {
  id: string;
  kind: string;
  status: string;
  provider?: string;
  base_url?: string;
  repo?: string;
  head_branch?: string;
  base_branch?: string;
  commit_sha?: string;
  title?: string;
  body?: string;
  remote_url?: string;
  remote_number?: number | null;
  result_sha?: string;
  message?: string;
  created_at: string;
  updated_at: string;
};
export type OperationDetail = {
  operation: Operation;
  comments: Comment[];
  comments_more: boolean;
  runs: Run[];
  sub_operations: Operation[];
  relations: Relation[];
  source_action_available: boolean;
  source_rover_id: string | null;
  source_actions: SourceAction[];
  forge_actions?: ForgeAction[];
  pull_requests: PullRequest[];
};
export type RunDetail = { run: Run; events: RunEvent[]; artifacts: Artifact[]; messages: RunMessage[] };

export const BOARD_COLUMNS: { key: string; label: string }[] = [
  { key: "backlog", label: "Backlog" },
  { key: "todo", label: "Todo" },
  { key: "in_progress", label: "In Progress" },
  { key: "in_review", label: "In Review" },
  { key: "done", label: "Done" },
  { key: "blocked", label: "Blocked" },
];

export const STATUS_TEXT: Record<string, string> = {
  backlog: "text-muted-foreground",
  todo: "text-foreground",
  in_progress: "text-info",
  in_review: "text-warning",
  done: "text-success",
  blocked: "text-destructive",
  canceled: "text-muted-foreground",
};
