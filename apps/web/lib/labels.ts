import type { Pilot, Comment, Crew, Member, Mission, Operation } from "@/lib/types";

// Display id: <mission key>-<per-mission sequence>, e.g. MSJ-123.
export function operationCode(operation: Operation, missions: Mission[]): string {
  const key = missions.find((m) => m.id === operation.mission_id)?.key ?? "MISSION";
  return `${key}-${operation.sequence}`;
}

export function assigneeLabel(operation: Operation, user: { id: string }, _pilots: Pilot[], crews: Crew[], members: Member[] = []): string {
  if (!operation.assignee_type) return "Unassigned";
  if (operation.assignee_type === "user") return memberName(operation.assignee_id, user, members);
  if (operation.assignee_type === "pilot") return pilotLabel(operation.assignee_pilot_kind ?? "");
  if (operation.assignee_type === "crew") return crews.find((c) => c.id === operation.assignee_id)?.name ?? "Crew";
  return operation.assignee_type;
}

function memberName(id: string | null, user: { id: string }, members: Member[]): string {
  if (id === user.id) return "You";
  const m = members.find((x) => x.id === id);
  return m ? m.name || m.email : "User";
}

export function assigneeHasPilot(operation: Operation, crews: Crew[] = []): boolean {
  if (operation.assignee_type === "pilot") return true;
  if (operation.assignee_type !== "crew") return false;
  return crews
    .find((c) => c.id === operation.assignee_id)
    ?.members?.some((m) => m.member_type === "pilot") ?? false;
}

export function operationAssigneeValue(operation: Operation, user: { id: string }): string {
  if (operation.assignee_type === "user" && operation.assignee_id === user.id) return "me";
  if (operation.assignee_type === "pilot") return `pilot:${operation.assignee_pilot_kind}`;
  if (operation.assignee_type) return `${operation.assignee_type}:${operation.assignee_id}`;
  return "";
}

export function commentAuthor(c: Comment, userId: string, _pilots: Pilot[]): string {
  if (c.author_type === "user") return c.author_id === userId ? "You" : "User";
  if (c.author_type === "pilot") return pilotLabel(c.author_pilot_kind ?? "");
  return "System";
}

const PILOT_LABELS: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
  antigravity: "Antigravity",
  cursor: "Cursor Agent",
  copilot: "GitHub Copilot",
  amp: "Amp Code",
  opencode: "OpenCode",
  openclaw: "OpenClaw",
  hermes: "Hermes",
  pi: "Pi",
  kimi: "Kimi",
  kiro: "Kiro",
};

export function pilotLabel(pilot: string): string {
  return PILOT_LABELS[pilot] ?? pilot;
}

// Priority rank (0 none -> 4 urgent) -> label + color.
export const PRIORITY: { label: string; color: string }[] = [
  { label: "No priority", color: "text-muted-foreground" },
  { label: "Low", color: "text-info" },
  { label: "Medium", color: "text-warning" },
  { label: "High", color: "text-destructive" },
  { label: "Urgent", color: "text-destructive" },
];

// Left-edge accent per priority.
export const PRIORITY_ACCENT = [
  "border-l-border", "border-l-info", "border-l-warning", "border-l-destructive", "border-l-destructive",
];

export const LABEL_COLOR: Record<string, string> = {
  gray: "bg-muted text-muted-foreground",
  red: "bg-destructive/15 text-destructive",
  orange: "bg-warning/15 text-warning",
  green: "bg-success/15 text-success",
  blue: "bg-info/15 text-info",
  purple: "bg-brand/15 text-brand",
};

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}
