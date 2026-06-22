"use client";

import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { Archive, ArchiveRestore, ArrowDown, ArrowLeft, ArrowUp, Clock, GitPullRequest, Link2, Loader2, MessageCircleQuestion, Moon, Play, Plus, ScrollText, SmilePlus, Square, Sun, Timer, Users, X } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { StatusIcon } from "@/components/status-icon";
import { PriorityIcon } from "@/components/priority-icon";
import { PilotIcon } from "@/components/pilot-icon";
import { onFire, DetailFire } from "@/components/fire";
import { Markdown } from "@/components/markdown";
import { TelemetryDialog } from "@/components/telemetry-dialog";
import { SignalsMenu } from "@/components/signals-menu";
import { Button } from "@/components/ui/button";
import { TagEditor } from "@/components/tag-editor";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn, hideFlowControlFlags } from "@/lib/utils";
import { assigneeHasPilot, commentAuthor, pilotLabel, operationAssigneeValue, operationCode, PRIORITY, LABEL_COLOR } from "@/lib/labels";
import { elapsed } from "@/lib/timeline";
import { formatTimestamp, useCommsOrder, useTimeFormat, type TimeFormat } from "@/lib/view";
import type { Comment, Member, OperationReference, Operation, Reaction, Relation, Run } from "@/lib/types";

const ACTIVE = new Set(["queued", "claimed", "starting", "running"]);
const isActive = (r: Run) => ACTIVE.has(r.state);
const STATUSES = ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"];
const STATUS_LABEL: Record<string, string> = {
  backlog: "Backlog", todo: "Todo", in_progress: "In Progress", in_review: "In Review",
  done: "Done", blocked: "Blocked", cancelled: "Cancelled",
};
const EMOJI = ["👍", "🎉", "👀", "✅", "❤️", "🚀"];
const COMMENT_PREVIEW_LIMIT = 30;
const PILOT_STATUS_PREFIX = "Pilot set status: ";
const CAPTAIN_SPLIT_RE = /^Captain split into (\d+) sub-operations$/;

export function OperationDetail() {
  const app = useApp();
  const open = app.selectedOperation != null;
  const d = app.operationDetail;
  const [comment, setComment] = useState("");
  const [telemetryRun, setTelemetryRun] = useState<Run | null>(null);
  const [showAllComments, setShowAllComments] = useState(false);
  const [runPromptOpen, setRunPromptOpen] = useState(false);
  const [runPrompt, setRunPrompt] = useState("");
  const { theme, resolvedTheme, setTheme } = useTheme();
  const { timeFormat } = useTimeFormat();
  const { commsOrder, setCommsOrder } = useCommsOrder();

  const runs = d?.runs ?? [];
  const activeRun = runs.find(isActive);
  useEffect(() => {
    setShowAllComments(false);
  }, [d?.operation.id]);

  function openTelemetry(run: Run) {
    app.setSelectedRun(run.id);
    setTelemetryRun(run);
  }
  function startRun(message?: string) {
    if (!d) return;
    app.runOperation(d.operation.id, message);
    setRunPrompt("");
    setRunPromptOpen(false);
  }
  function assigneeChange(v: string) {
    if (!d) return;
    if (v === "me") app.reassign(d.operation.id, "user", app.user.id);
    else { const [k, id] = v.split(":"); app.reassign(d.operation.id, k, id); }
  }
  const darkNow = resolvedTheme === "dark" || theme === "console-dark";
  function toggleTheme() {
    setTheme(theme?.startsWith("console-") ? (darkNow ? "console-light" : "console-dark") : (darkNow ? "light" : "dark"));
  }

  if (!open) {
    return <TelemetryDialog run={telemetryRun} open={telemetryRun != null} onOpenChange={(o) => { if (!o) { setTelemetryRun(null); app.setSelectedRun(null); } }} />;
  }
  if (!d) {
    return (
      <div className="flex min-h-0 flex-1 flex-col bg-background">
        <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
          <Button variant="ghost" size="icon-sm" onClick={() => app.openOperation(null)} title="Back"><ArrowLeft /></Button>
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
          <span className="flex-1 text-sm text-muted-foreground">Loading operation...</span>
          <div className="flex items-center gap-2">
            <SignalsMenu />
            <Button variant="ghost" size="icon-sm" onClick={toggleTheme} title="Toggle theme">
              {darkNow ? <Sun /> : <Moon />}
            </Button>
          </div>
        </header>
      </div>
    );
  }

  const fire = onFire(d.operation);
  const comments = d.comments ?? [];
  const activity = compactActivityComments(comments);
  const orderedActivity = commsOrder === "oldest_bottom" ? [...activity].reverse() : activity;
  const hiddenCommentCount = Math.max(0, activity.length - COMMENT_PREVIEW_LIMIT);
  const visibleActivity = showAllComments
    ? orderedActivity
    : commsOrder === "oldest_bottom"
      ? orderedActivity.slice(0, COMMENT_PREVIEW_LIMIT)
      : orderedActivity.slice(-COMMENT_PREVIEW_LIMIT);
  const workingActivityRun = activeRun && !activity.some((row) => runForPilotComment(row.comment, runs)?.id === activeRun.id) ? activeRun : null;
  const timestamp = (value: string) => formatTimestamp(value, timeFormat);
  const replyForm = (
    <>
      <form
        className="flex gap-2 pt-1"
        onSubmit={(e) => { e.preventDefault(); if (comment.trim()) { app.addComment(d.operation.id, comment); setComment(""); } }}
      >
        <Textarea value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Reply…" className="min-h-20 flex-1 resize-y" />
        <Button type="submit" size="sm" className="self-end">Send</Button>
      </form>
      {assigneeHasPilot(d.operation, app.crews) && <p className="text-[11px] text-muted-foreground">Replying resumes the pilot&apos;s session.</p>}
    </>
  );

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col bg-background">
        <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
          <Button variant="ghost" size="icon-sm" onClick={() => app.openOperation(null)} title="Back"><ArrowLeft /></Button>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <StatusIcon status={d.operation.status} className="size-3.5" />
            <span className="flex h-4 items-center font-mono text-[11px] font-medium uppercase text-muted-foreground">{operationCode(d.operation, app.missions)}</span>
            <span className="flex h-4 min-w-0 items-center truncate text-sm font-medium">{d.operation.title}</span>
          </div>
          <div className="flex items-center gap-2">
            {activeRun && <ActiveRunPill run={activeRun} />}
            {activeRun ? (
              <Button size="sm" variant="destructive" onClick={() => app.cancelRun(activeRun.id, d.operation.id)}><Square /> Stop</Button>
            ) : (
              <Popover open={runPromptOpen} onOpenChange={setRunPromptOpen}>
                <PopoverTrigger asChild><Button size="sm" variant="outline"><Play /> Run</Button></PopoverTrigger>
                <PopoverContent align="end" className="w-80 space-y-2 p-3">
                  <Textarea value={runPrompt} onChange={(e) => setRunPrompt(e.target.value)} placeholder="Add instructions for this run…" className="min-h-20 resize-y text-sm" autoFocus />
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="ghost" onClick={() => startRun()}>Use operation prompt</Button>
                    <Button size="sm" disabled={!runPrompt.trim()} onClick={() => startRun(runPrompt.trim())}><Play /> Start</Button>
                  </div>
                </PopoverContent>
              </Popover>
            )}
            {(d.operation.status === "done" || d.operation.status === "cancelled") && (
              <Button size="sm" variant="ghost" onClick={() => app.setArchived(d.operation.id, !d.operation.archived)} title={d.operation.archived ? "Unarchive" : "Archive"}>
                {d.operation.archived ? <ArchiveRestore /> : <Archive />} {d.operation.archived ? "Unarchive" : "Archive"}
              </Button>
            )}
            <SignalsMenu />
            <Button variant="ghost" size="icon-sm" onClick={toggleTheme} title="Toggle theme">
              {darkNow ? <Sun /> : <Moon />}
            </Button>
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto p-4 lg:px-16">
          <div className="flex w-full max-w-5xl">
            {/* main */}
            <div className="min-w-0 flex-1">
              <div className="space-y-4">
                <h1 className="text-xl font-semibold leading-snug">{d.operation.title}</h1>
                {d.operation.orchestrating && (
                  <Badge variant="secondary" className="gap-1 text-[10px]"><Users className="size-2.5" /> captain orchestrating sub-operations</Badge>
                )}
                {d.operation.body && <Markdown>{d.operation.body}</Markdown>}
                <ReactionBar reactions={d.operation.reactions ?? []} onToggle={(e, on) => app.react("operations", d.operation.id, e, d.operation.id, on)} />
                {d.operation.status === "in_review" && d.runs.some((r) => r.needs_input) && (
                  <div className="flex items-center gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
                    <MessageCircleQuestion className="size-4 shrink-0 text-warning" />
                    <span>A pilot is waiting for your input — reply below to continue.</span>
                  </div>
                )}

                <SubOperations mainId={d.operation.id} missionId={d.operation.mission_id} subOperations={d.sub_operations} />

                <div className="space-y-3 pr-4">
                  <div className="flex items-center justify-between">
                    <h2 className="text-sm font-semibold">Communications</h2>
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground"
                        title={commsOrder === "oldest_top" ? "Oldest top" : "Oldest bottom"}
                        aria-label="Toggle communications order"
                        onClick={() => setCommsOrder(commsOrder === "oldest_top" ? "oldest_bottom" : "oldest_top")}
                      >
                        {commsOrder === "oldest_top" ? <ArrowDown /> : <ArrowUp />}
                      </Button>
                      {!showAllComments && hiddenCommentCount > 0 && (
                        <Button variant="ghost" size="sm" className="text-xs text-muted-foreground" onClick={() => setShowAllComments(true)}>
                          Show all {activity.length} communications
                        </Button>
                      )}
                    </div>
                  </div>
                  {d.comments.length === 0 && <p className="text-sm text-muted-foreground">No communications yet.</p>}
                  {commsOrder === "oldest_bottom" && replyForm}
                  {workingActivityRun && commsOrder === "oldest_bottom" && <WorkingPilotActivityRow run={workingActivityRun} onTelemetry={openTelemetry} />}
                  {visibleActivity.map((row) => (
                    <CommentRow
                      key={row.comment.id}
                      c={row.comment}
                      operationId={d.operation.id}
                      pilotStatus={row.pilotStatus}
                      captainSplit={row.captainSplit}
                      run={runForPilotComment(row.comment, runs)}
                      onTelemetry={openTelemetry}
                      timeFormat={timeFormat}
                    />
                  ))}
                  {workingActivityRun && commsOrder === "oldest_top" && <WorkingPilotActivityRow run={workingActivityRun} onTelemetry={openTelemetry} />}
                  {commsOrder === "oldest_top" && replyForm}
                </div>
              </div>
            </div>

            {/* properties rail */}
            <div className="w-72 shrink-0 border-l border-border bg-muted/20">
              <div className="divide-y divide-border/60 text-sm">
                <div className="space-y-0.5 p-4">
                  <PropRow label="Status">
                    <RailSelect value={d.operation.status} onValueChange={(v) => app.moveOperation(d.operation.id, v)}>
                      {STATUSES.map((s) => <SelectItem key={s} value={s}><span className="flex items-center gap-2"><StatusIcon status={s} className="size-3.5" /> {STATUS_LABEL[s]}</span></SelectItem>)}
                    </RailSelect>
                  </PropRow>
                  <PropRow label="Assignee">
                    <RailSelect value={operationAssigneeValue(d.operation, app.user)} onValueChange={assigneeChange} placeholder="Unassigned">
                      <SelectItem value="me">Me</SelectItem>
                      {app.members.filter((m) => m.id !== app.user.id).map((m) => <SelectItem key={`u${m.id}`} value={`user:${m.id}`}>🧑 {m.name || m.email}</SelectItem>)}
                      {app.pilots.map((p) => <SelectItem key={`p${p.kind}`} value={`pilot:${p.kind}`} disabled={p.rovers === 0}><span className="flex items-center gap-2"><PilotIcon kind={p.kind} /> {pilotLabel(p.kind)}{p.rovers === 0 && " — no rover"}</span></SelectItem>)}
                      {app.crews.map((c) => <SelectItem key={`c${c.id}`} value={`crew:${c.id}`}>👥 {c.name}</SelectItem>)}
                    </RailSelect>
                  </PropRow>
                  <PropRow label="Priority">
                    <RailSelect value={String(d.operation.priority)} onValueChange={(v) => app.setPriority(d.operation.id, Number(v))}>
                      {PRIORITY.map((p, i) => <SelectItem key={i} value={String(i)}><span className="flex items-center gap-2"><PriorityIcon level={i} className="size-3.5" /> {p.label}</span></SelectItem>)}
                    </RailSelect>
                  </PropRow>
                  <PropRow label="Mission">
                    <span className="truncate text-xs">{app.missions.find((m) => m.id === d.operation.mission_id)?.name ?? "—"}</span>
                  </PropRow>
                  <PropRow label="Start">
                    <DateField value={d.operation.start_date} onChange={(v) => app.setDates(d.operation.id, v, d.operation.due_date)} />
                  </PropRow>
                  <PropRow label="Due">
                    <DateField value={d.operation.due_date} onChange={(v) => app.setDates(d.operation.id, d.operation.start_date, v)} />
                  </PropRow>
                </div>

                <div className="p-4">
                  <p className="mb-1.5 text-[11px] font-medium uppercase text-muted-foreground">Labels</p>
                  <Labels op={d.operation} />
                </div>

                <div className="p-4">
                  <p className="mb-1.5 text-[11px] font-medium uppercase text-muted-foreground">Dispatch · rover tags</p>
                  <div className="space-y-1.5">
                    <div className="flex items-start gap-2 text-xs">
                      <span className="w-12 shrink-0 pt-1 text-muted-foreground">need</span>
                      <TagEditor tags={d.operation.required_tags ?? []} onChange={(t) => app.setOperationTags(d.operation.id, t, d.operation.excluded_tags ?? [])} placeholder="any" />
                    </div>
                    <div className="flex items-start gap-2 text-xs">
                      <span className="w-12 shrink-0 pt-1 text-muted-foreground">avoid</span>
                      <TagEditor tags={d.operation.excluded_tags ?? []} onChange={(t) => app.setOperationTags(d.operation.id, d.operation.required_tags ?? [], t)} placeholder="none" />
                    </div>
                  </div>
                </div>

                <div className="p-4">
                  <p className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium uppercase text-muted-foreground"><Link2 className="size-3.5" /> Relationships</p>
                  <Relationships op={d.operation} relations={d.relations ?? []} subOperations={d.sub_operations ?? []} />
                </div>

                <div className="p-4">
                  <p className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium uppercase text-muted-foreground"><GitPullRequest className="size-3.5" /> Pull requests</p>
                  <PullRequests operationId={d.operation.id} />
                </div>

                <div className="space-y-1.5 p-4 text-xs text-muted-foreground">
                  <div className="flex items-center justify-between"><span>Created by</span><span className="text-foreground">{memberDisplay(d.operation.created_by, app.members, app.user.id)}</span></div>
                  <div className="flex items-center justify-between"><span>Created</span><span>{timestamp(d.operation.created_at)}</span></div>
                  <div className="flex items-center justify-between"><span>Updated</span><span>{timestamp(d.operation.updated_at)}</span></div>
                  {d.operation.started_at && <div className="flex items-center justify-between"><span>Started</span><span>{timestamp(d.operation.started_at)}</span></div>}
                  {d.operation.finished_at && <div className="flex items-center justify-between"><span>Finished</span><span>{timestamp(d.operation.finished_at)}</span></div>}
                </div>
              </div>
            </div>
          </div>
        </div>
        {fire && <DetailFire />}
      </div>
      <TelemetryDialog run={telemetryRun} open={telemetryRun != null} onOpenChange={(o) => { if (!o) { setTelemetryRun(null); app.setSelectedRun(null); } }} />
    </>
  );
}

// A compact label-left / control-right property row for the detail rail.
function PropRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-8 items-center justify-between gap-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 justify-end">{children}</div>
    </div>
  );
}

// Borderless compact Select for the rail (value reads as inline text until hovered).
function RailSelect({ value, onValueChange, placeholder, children }: { value: string; onValueChange: (v: string) => void; placeholder?: string; children: React.ReactNode }) {
  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger className="h-7 w-auto gap-1 border-0 bg-transparent px-1.5 text-xs hover:bg-accent focus:ring-0"><SelectValue placeholder={placeholder} /></SelectTrigger>
      <SelectContent>{children}</SelectContent>
    </Select>
  );
}

// Low-key date control: shows a muted "—" / "Jun 12", with a transparent native
// date picker overlaid so the mm/dd/yyyy field isn't visible until interacted with.
function DateField({ value, onChange }: { value: string | null; onChange: (v: string | null) => void }) {
  return (
    <label className="relative inline-flex cursor-pointer items-center rounded-md px-1.5 py-0.5 text-xs hover:bg-accent">
      <span className={value ? "" : "text-muted-foreground/40"}>
        {value ? new Date(value + "T00:00:00").toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "—"}
      </span>
      <input type="date" value={value ?? ""} onChange={(e) => onChange(e.target.value || null)} className="absolute inset-0 cursor-pointer opacity-0" />
    </label>
  );
}

function memberDisplay(id: string | null, members: Member[], userId: string): string {
  if (!id) return "—";
  if (id === userId) return "You";
  const m = members.find((x) => x.id === id);
  return m ? m.name || m.email : "—";
}

function pilotStatusFromComment(c: Comment) {
  if (c.author_type !== "system" || !c.body.startsWith(PILOT_STATUS_PREFIX)) return null;
  const status = c.body.slice(PILOT_STATUS_PREFIX.length).trim();
  return STATUS_LABEL[status] ? status : null;
}

function captainSplitFromComment(c: Comment) {
  if (c.author_type !== "system") return null;
  return c.body.match(CAPTAIN_SPLIT_RE)?.[1] ?? null;
}

function compactActivityComments(comments: Comment[]) {
  const rows: { comment: Comment; pilotStatus?: string; captainSplit?: string }[] = [];
  for (const c of comments) {
    const status = pilotStatusFromComment(c);
    const split = captainSplitFromComment(c);
    const prev = rows[rows.length - 1];
    if (status && prev?.comment.author_type === "pilot") {
      prev.pilotStatus = status;
      continue;
    }
    if (status) {
      rows.push({ comment: c, pilotStatus: status });
      continue;
    }
    if (split && prev?.comment.author_type === "pilot") {
      prev.captainSplit = split;
      continue;
    }
    rows.push({ comment: c });
  }
  return rows;
}

function runForPilotComment(c: Comment, runs: Run[]) {
  if (c.author_type !== "pilot" || !c.author_pilot_kind) return null;
  const at = new Date(c.created_at).getTime();
  // ponytail: timestamp match; store comment.run_id if overlapping same-pilot runs need exact links.
  return runs
    .filter((r) => r.pilot === c.author_pilot_kind && new Date(r.created_at).getTime() <= at)
    .sort((a, b) => b.created_at.localeCompare(a.created_at))[0] ?? null;
}

function SplitIcon() {
  return (
    <span className="inline-flex size-4 items-center justify-center rounded-full bg-info/15 text-info">
      <svg viewBox="0 0 14 14" className="size-3" aria-hidden>
        <path d="M4 7 H7 L10 4 M7 7 L10 10" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        <circle cx="4" cy="7" r="1.2" fill="currentColor" />
        <circle cx="10" cy="4" r="1.2" fill="currentColor" />
        <circle cx="10" cy="10" r="1.2" fill="currentColor" />
      </svg>
    </span>
  );
}

function CommentRow({ c, operationId, pilotStatus, captainSplit, run, onTelemetry, timeFormat }: { c: Comment; operationId: string; pilotStatus?: string; captainSplit?: string; run: Run | null; onTelemetry: (run: Run) => void; timeFormat: TimeFormat }) {
  const app = useApp();
  const isPilot = c.author_type === "pilot";
  const isSystem = c.author_type === "system";
  const active = run ? isActive(run) : false;
  const body = hideFlowControlFlags(c.body);
  const systemPilotAction = isSystem && pilotStatus && c.body.startsWith(PILOT_STATUS_PREFIX);
  return (
    <div className="flex gap-2.5">
      <Avatar className="-mt-0.5 size-6">
        <AvatarFallback className={cn(isPilot && "bg-brand/15 text-brand", isSystem && "bg-muted text-muted-foreground")}>
          {isPilot ? <PilotIcon kind={c.author_pilot_kind ?? ""} size={13} /> : isSystem ? "·" : "U"}
        </AvatarFallback>
      </Avatar>
      <div className="flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className={cn("truncate text-sm font-medium", isPilot && "text-brand", isSystem && "text-muted-foreground")}>
              {commentAuthor(c, app.user.id, app.pilots)}
            </span>
            {isPilot && run && active && (
              <ActiveRunElapsed run={run} />
            )}
            {pilotStatus && (
              <span className="inline-flex items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                <StatusIcon status={pilotStatus} className="size-3" /> {STATUS_LABEL[pilotStatus]}
              </span>
            )}
            {captainSplit && (
              <span className="inline-flex items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                <SplitIcon /> {captainSplit} sub-operations
              </span>
            )}
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-1.5">
            <span className="text-[11px] text-muted-foreground">{formatTimestamp(c.created_at, timeFormat)}</span>
            {isPilot && run && !active && (
              <span className="inline-flex items-center gap-1 text-[11px] tabular-nums text-muted-foreground">
                <Timer className="size-3" />
                {elapsed(run.created_at, new Date(run.updated_at).getTime())}
              </span>
            )}
            {isPilot && run && (
              <Button variant="ghost" size="icon-sm" className="size-6 text-muted-foreground" title="Open run log" aria-label="Open run log" onClick={() => onTelemetry(run)}>
                <ScrollText className="size-3.5" />
              </Button>
            )}
          </div>
        </div>
        {body.trim() && !systemPilotAction && (isSystem ? <p className="text-sm text-muted-foreground">{body}</p> : <Markdown>{body}</Markdown>)}
        <ReactionBar reactions={c.reactions} onToggle={(e, on) => app.react("comments", c.id, e, operationId, on)} />
      </div>
    </div>
  );
}

// Shared reaction strip: existing reactions (hover → reactors) + an add-emoji menu.
function ReactionBar({ reactions, onToggle }: { reactions: Reaction[]; onToggle: (emoji: string, on?: boolean) => void }) {
  return (
    <TooltipProvider delayDuration={150}>
      <div className="mt-1 flex flex-wrap items-center gap-1">
        {reactions.map((r) => (
          <Tooltip key={r.emoji}>
            <TooltipTrigger asChild>
              <button onClick={() => onToggle(r.emoji, !r.mine)} className={cn("rounded-full border px-1.5 py-0.5 text-xs", r.mine ? "border-brand bg-brand/10" : "border-border")}>
                {r.emoji} {r.count}
              </button>
            </TooltipTrigger>
            <TooltipContent>{(r.users ?? []).join(", ") || r.emoji}</TooltipContent>
          </Tooltip>
        ))}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="rounded-full p-1 text-muted-foreground hover:text-foreground"><SmilePlus className="size-3.5" /></button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="flex gap-1 p-1">
            {EMOJI.map((e) => <DropdownMenuItem key={e} onSelect={() => onToggle(e)} className="px-1 py-0 text-base">{e}</DropdownMenuItem>)}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </TooltipProvider>
  );
}

function Labels({ op }: { op: Operation }) {
  const app = useApp();
  const [name, setName] = useState("");
  const onOperation = new Set(op.labels.map((l) => l.id));
  const available = app.labels.filter((l) => !onOperation.has(l.id));
  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap gap-1">
        {op.labels.map((l) => (
          <span key={l.id} className={cn("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px]", LABEL_COLOR[l.color] ?? LABEL_COLOR.gray)}>
            {l.name}
            <button onClick={() => app.detachLabel(op.id, l.id)} className="opacity-70 hover:opacity-100"><X className="size-2.5" /></button>
          </span>
        ))}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="h-6 px-1.5 text-xs text-muted-foreground"><Plus className="size-3" /> label</Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-48">
          {available.map((l) => (
            <DropdownMenuItem key={l.id} onClick={() => app.attachLabel(op.id, l.id)}>
              <span className={cn("mr-2 size-2 rounded-full", LABEL_COLOR[l.color] ?? LABEL_COLOR.gray)} />{l.name}
            </DropdownMenuItem>
          ))}
          <form
            className="flex gap-1 p-1"
            onSubmit={async (e) => { e.preventDefault(); if (!name.trim()) return; const l = await app.createLabel(name.trim(), "blue"); if (l) app.attachLabel(op.id, l.id); setName(""); }}
          >
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="new label" className="h-7 text-xs" />
          </form>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function PullRequests({ operationId }: { operationId: string }) {
  const app = useApp();
  const pullRequests = app.operationDetail?.pull_requests ?? [];
  const [url, setUrl] = useState("");
  return (
    <div className="space-y-1.5">
      {pullRequests.map((p) => (
        <div key={p.id} className="flex items-center gap-1.5 text-xs">
          <a href={p.url} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate text-info hover:underline">{p.title || p.url}</a>
          <button onClick={() => app.deletePullRequest(p.id, operationId)} className="text-muted-foreground hover:text-destructive"><X className="size-3" /></button>
        </div>
      ))}
      {pullRequests.length === 0 && <p className="text-xs text-muted-foreground">No linked pull requests.</p>}
      <form
        onSubmit={(e) => { e.preventDefault(); if (url.trim()) { app.addPullRequest(operationId, url.trim(), ""); setUrl(""); } }}
      >
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="Link a pull request URL..." className="h-7 text-xs" />
      </form>
    </div>
  );
}

const REL_LABEL: Record<string, string> = {
  blocks: "Blocks", blocked_by: "Blocked by", relates: "Relates to",
  duplicate: "Duplicate of", duplicated_by: "Duplicated by",
};
const REL_ORDER = ["blocks", "blocked_by", "relates", "duplicate", "duplicated_by"];

function Relationships({ op, relations, subOperations }: { op: Operation; relations: Relation[]; subOperations: Operation[] }) {
  const app = useApp();
  const [addKind, setAddKind] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [results, setResults] = useState<OperationReference[]>([]);
  useEffect(() => {
    if (addKind === null) return;
    let active = true;
    app.searchOperations(q).then((r) => { if (active) setResults(r.filter((o) => o.id !== op.id)); });
    return () => { active = false; };
  }, [q, addKind, op.id, app]);
  const groups = REL_ORDER.map((k) => ({ k, items: relations.filter((r) => r.kind === k) })).filter((g) => g.items.length > 0);
  return (
    <div className="space-y-2">
      {subOperations.length > 0 && (
        <div className="space-y-0.5">
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground/70">Sub-operations</p>
          {subOperations.map((child) => (
            <div key={child.id} className="flex items-center gap-1.5 text-xs">
              <StatusIcon status={child.status} className="size-3.5 shrink-0" />
              <button onClick={() => app.openOperation(child.id)} className="flex min-w-0 flex-1 items-center gap-1.5 text-left hover:underline">
                <span className="font-mono text-[10px] text-muted-foreground">{operationCode(child, app.missions)}</span>
                <span className="truncate">{child.title}</span>
              </button>
            </div>
          ))}
        </div>
      )}
      {groups.map((g) => (
        <div key={g.k} className="space-y-0.5">
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground/70">{REL_LABEL[g.k]}</p>
          {g.items.map((r) => (
            <div key={r.id} className="group flex items-center gap-1.5 text-xs">
              <StatusIcon status={r.operation.status} className="size-3.5 shrink-0" />
              <button onClick={() => app.openOperation(r.operation.id)} className="flex min-w-0 flex-1 items-center gap-1.5 text-left hover:underline">
                <span className="font-mono text-[10px] text-muted-foreground">{operationCode(r.operation as Operation, app.missions)}</span>
                <span className="truncate">{r.operation.title}</span>
              </button>
              <button onClick={() => app.removeRelation(r.id, op.id)} className="shrink-0 text-muted-foreground opacity-0 hover:text-destructive group-hover:opacity-100"><X className="size-3" /></button>
            </div>
          ))}
        </div>
      ))}
      {addKind === null ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="text-xs text-muted-foreground"><Plus className="size-3" /> Add relation</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {REL_ORDER.map((k) => <DropdownMenuItem key={k} onClick={() => { setAddKind(k); setQ(""); setResults([]); }}>{REL_LABEL[k]}…</DropdownMenuItem>)}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <div className="space-y-1">
          <div className="flex items-center gap-1 text-[10px] uppercase tracking-wide text-muted-foreground/70">
            {REL_LABEL[addKind]}
            <button onClick={() => setAddKind(null)} className="ml-auto hover:text-foreground"><X className="size-3" /></button>
          </div>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search operations…" className="h-7 text-xs" autoFocus />
          <div className="max-h-40 space-y-0.5 overflow-auto">
            {results.map((o) => (
              <button key={o.id} onClick={() => { app.addRelation(op.id, addKind, o.id); setAddKind(null); }} className="flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-left text-xs hover:bg-accent">
                <StatusIcon status={o.status} className="size-3.5 shrink-0" />
                <span className="font-mono text-[10px] text-muted-foreground">{operationCode(o as Operation, app.missions)}</span>
                <span className="truncate">{o.title}</span>
              </button>
            ))}
            {q && results.length === 0 && <p className="px-1.5 py-1 text-xs text-muted-foreground">No matches.</p>}
          </div>
        </div>
      )}
    </div>
  );
}

function WorkingPilotActivityRow({ run, onTelemetry }: { run: Run; onTelemetry: (run: Run) => void }) {
  return (
    <div className="flex gap-2.5">
      <Avatar className="-mt-0.5 size-6">
        <AvatarFallback className="bg-brand/15 text-brand">
          <PilotIcon kind={run.pilot ?? ""} size={13} />
        </AvatarFallback>
      </Avatar>
      <div className="flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium text-brand">{pilotLabel(run.pilot ?? "pilot")}</span>
            <ActiveRunElapsed run={run} />
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-1.5">
            <Button variant="ghost" size="icon-sm" className="size-6 text-muted-foreground" title="Open run log" aria-label="Open run log" onClick={() => onTelemetry(run)}>
              <ScrollText className="size-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function SubOperations({ mainId, missionId, subOperations }: { mainId: string; missionId: string | null; subOperations: Operation[] }) {
  const app = useApp();
  const [title, setTitle] = useState("");
  const [adding, setAdding] = useState(false);
  if (subOperations.length === 0 && !adding) {
    return <Button variant="ghost" size="sm" className="text-xs text-muted-foreground" onClick={() => setAdding(true)}><Plus className="size-3" /> Add sub-operation</Button>;
  }
  return (
    <div className="space-y-1">
      <p className="text-[11px] font-medium uppercase text-muted-foreground">Sub-operations {subOperations.length > 0 && `· ${subOperations.filter((c) => c.status === "done").length}/${subOperations.length}`}</p>
      {subOperations.map((c) => (
        <button key={c.id} onClick={() => app.openOperation(c.id)} className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-accent/50">
          <StatusIcon status={c.status} className="size-3.5" />
          <span className="font-mono text-[10px] text-muted-foreground">{operationCode(c, app.missions)}</span>
          <span className="truncate">{c.title}</span>
        </button>
      ))}
      <form
        className="flex gap-1"
        onSubmit={(e) => { e.preventDefault(); if (title.trim() && missionId) { app.createOperation({ title: title.trim(), body: "", mission_id: missionId, assignee_type: "user", assignee_id: app.user.id, main_operation_id: mainId }); setTitle(""); } }}
      >
        <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="New sub-operation…" className="h-7 text-xs" autoFocus />
      </form>
    </div>
  );
}

function ActiveRunElapsed({ run }: { run: Run }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const queued = run.state === "queued";
  const Icon = queued ? Clock : Loader2;
  return (
    <span className={cn(
      "inline-flex items-center gap-1 text-[11px] tabular-nums",
      queued ? "text-muted-foreground" : "text-info",
    )}>
      <Icon className={cn("size-3", !queued && "animate-spin")} />
      {queued && "Queued "}
      {elapsed(run.created_at, now)}
    </span>
  );
}

function ActiveRunPill({ run }: { run: Run }) {
  const queued = run.state === "queued";
  return (
    <span className={cn(
      "flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium",
      queued ? "bg-muted text-muted-foreground" : "bg-info/10 text-info",
    )}>
      {queued ? <Clock className="size-3" /> : <Loader2 className="size-3 animate-spin" />}
      {queued ? "Queued" : "Working"}
    </span>
  );
}
