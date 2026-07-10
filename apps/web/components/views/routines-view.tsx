"use client";

import { useRef, useState } from "react";
import { Activity, Loader2, Paperclip, Pause, Pencil, Play, Plus, Tags, Trash2, UserRound, X, type LucideIcon } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { AssetChipStrip } from "@/components/asset-display";
import { PriorityIcon } from "@/components/priority-icon";
import { appendAssetLink } from "@/lib/assets";
import { CrewOption, PilotOption } from "@/components/assignee-select";
import { TagEditor, TagList } from "@/components/tag-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { priorityLabel, useT, type MessageKey } from "@/lib/i18n";
import { PRIORITY_LEVELS, memberLabel, pilotLabel, userLabel } from "@/lib/labels";
import { SECTION_ICONS } from "@/lib/section-icons";
import type { Asset, AssigneeType, Crew, Pulse, Routine, RoutineTriggerType } from "@/lib/types";

const CRON_PRESET_KEYS: { value: string; labelKey: MessageKey }[] = [
  { value: "@hourly", labelKey: "routines.hourly" },
  { value: "@daily", labelKey: "routines.daily" },
  { value: "@weekly", labelKey: "routines.weekly" },
  { value: "*/15 * * * *", labelKey: "routines.every15" },
  { value: "0 9 * * *", labelKey: "routines.daily9" },
];

function crewCanDispatch(crew: Crew | undefined) {
  return !!crew?.members?.some((m) => m.member_type === "pilot");
}

function canDispatchAssignee(value: string, crews: Crew[]) {
  return value.startsWith("pilot:") || (value.startsWith("crew:") && crewCanDispatch(crews.find((c) => `crew:${c.id}` === value)));
}

function assigneeInput(value: string, userId: string): { type: AssigneeType; id: string } {
  if (value === "me") return { type: "user", id: userId };
  const [type, id] = value.split(":") as [AssigneeType, string];
  return { type, id };
}

function formatPulseTime(value: string | null, fallback: string) {
  if (!value) return fallback;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return fallback;
  return d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function routineTrigger(routine: Routine) {
  return routine.metadata.trigger ?? { kind: "manual" as RoutineTriggerType };
}

function routineOperation(routine: Routine) {
  return routine.metadata.operation ?? {};
}

function routineContext(routine: Routine) {
  return typeof routine.operation_metadata.context === "string" ? routine.operation_metadata.context : "";
}

function routineAssigneeValue(routine: Routine, userId: string) {
  const assignee = routineOperation(routine).assignee;
  if (!assignee?.type || !assignee.id) return "me";
  if (assignee.type === "user" && assignee.id === userId) return "me";
  return `${assignee.type}:${assignee.id}`;
}

export function RoutinesView() {
  const app = useApp();
  const t = useT();
  const [editingRoutineId, setEditingRoutineId] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [missionId, setMissionId] = useState("");
  const [assignee, setAssignee] = useState("me");
  const [dispatchAfterPulse, setDispatchAfterPulse] = useState(true);
  const [skipIfActive, setSkipIfActive] = useState(true);
  const [rePulseOnClose, setRePulseOnClose] = useState(true);
  const [autoCommitBranch, setAutoCommitBranch] = useState("");
  const [dropWorktreeOnCommit, setDropWorktreeOnCommit] = useState(true);
  const [createPullRequest, setCreatePullRequest] = useState(false);
  const [forgeKey, setForgeKey] = useState("");
  const [shipBaseBranch, setShipBaseBranch] = useState("");
  const [shipBaseReference, setShipBaseReference] = useState("");
  const [shipBaseSync, setShipBaseSync] = useState<"rebase" | "merge" | "reset">("merge");
  const [pullRequestLabels, setPullRequestLabels] = useState("");
  const [checksCommand, setChecksCommand] = useState("");
  const [checksTimeoutSeconds, setChecksTimeoutSeconds] = useState("");
  const [scheduleEnabled, setScheduleEnabled] = useState(true);
  const [priority, setPriority] = useState("0");
  const [triggerType, setTriggerType] = useState<RoutineTriggerType>("manual");
  const [cron, setCron] = useState("@daily");
  const [context, setContext] = useState("");
  const [requiredTags, setRequiredTags] = useState<string[]>([]);
  const [excludedTags, setExcludedTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [assets, setAssets] = useState<Asset[]>([]);
  const uploadRef = useRef<HTMLInputElement | null>(null);

  const mission = missionId || app.missions[0]?.id || "";
  const sortedCrews = [...app.crews].sort((a, b) => a.name.localeCompare(b.name));
  const sortedPilots = [...app.pilots].sort((a, b) => pilotLabel(a.kind).localeCompare(pilotLabel(b.kind)));
  const sortedMembers = app.members.filter((m) => m.id !== app.user.id).sort((a, b) => (a.name || a.email).localeCompare(b.name || b.email));
  const dispatchAvailable = canDispatchAssignee(assignee, app.crews);
  const autoDispatch = dispatchAvailable && dispatchAfterPulse;
  const canSave = !!title.trim() && !!mission && (triggerType === "manual" || !!cron.trim());
  const editing = editingRoutineId != null;
  const SaveIcon = editing ? Pencil : Plus;

  function setAssigneeAndDispatch(value: string) {
    setAssignee(value);
    setDispatchAfterPulse(canDispatchAssignee(value, app.crews));
  }

  function shipOperationFields() {
    const branch = autoCommitBranch.trim();
    if (!branch) return {};
    const labels = pullRequestLabels
      .split(/[,;\n]/)
      .map((s) => s.trim())
      .filter(Boolean);
    const timeout = Number(checksTimeoutSeconds);
    const commands = checksCommand
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    const checks = commands.length
      ? {
          commands,
          ...(Number.isFinite(timeout) && timeout > 0 ? { timeout_seconds: Math.floor(timeout) } : {}),
        }
      : undefined;
    const shipBase: {
      branch?: string;
      reference?: string;
      sync?: "rebase" | "merge" | "reset";
    } = {};
    if (shipBaseBranch.trim()) shipBase.branch = shipBaseBranch.trim();
    if (shipBaseReference.trim()) shipBase.reference = shipBaseReference.trim();
    if (shipBaseReference.trim() || shipBaseBranch.trim()) shipBase.sync = shipBaseSync;
    return {
      auto_commit: {
        branch,
        drop_worktree: dropWorktreeOnCommit,
      },
      ...(forgeKey.trim() ? { forge: { key: forgeKey.trim() } } : {}),
      ...(Object.keys(shipBase).length ? { ship_base: shipBase } : {}),
      pull_request: {
        create: createPullRequest,
        ...(createPullRequest && labels.length ? { labels } : {}),
      },
      ...(checks ? { checks } : {}),
    };
  }

  function routineInput() {
    const contextText = context.trim();
    return {
      mission_id: mission,
      title: title.trim(),
      body: body.trim(),
      metadata: {
        trigger: {
          kind: triggerType,
          enabled: triggerType === "schedule" ? scheduleEnabled : true,
          ...(triggerType === "schedule" ? { cron: cron.trim() } : {}),
        },
        operation: {
          pulse: {
            start_immediately: autoDispatch,
            skip_if_active: skipIfActive,
            re_pulse_on_close: rePulseOnClose,
          },
          ...shipOperationFields(),
          priority: Number(priority),
          assignee: assigneeInput(assignee, app.user.id),
          required_tags: requiredTags,
          excluded_tags: excludedTags,
        },
      },
      operation_metadata: contextText ? { context: contextText } : {},
    };
  }

  function resetForm() {
    setEditingRoutineId(null);
    setTitle("");
    setBody("");
    setMissionId("");
    setAssignee("me");
    setDispatchAfterPulse(true);
    setSkipIfActive(true);
    setRePulseOnClose(true);
    setAutoCommitBranch("");
    setDropWorktreeOnCommit(true);
    setCreatePullRequest(false);
    setForgeKey("");
    setShipBaseBranch(""); setShipBaseReference(""); setShipBaseSync("merge");
    setPullRequestLabels("");
    setChecksCommand("");
    setChecksTimeoutSeconds("");
    setScheduleEnabled(true);
    setPriority("0");
    setTriggerType("manual");
    setCron("@daily");
    setContext("");
    setRequiredTags([]);
    setExcludedTags([]);
    setAssets([]);
  }

  function editRoutine(routine: Routine) {
    const trigger = routineTrigger(routine);
    const operation = routineOperation(routine);
    const nextAssignee = routineAssigneeValue(routine, app.user.id);
    setEditingRoutineId(routine.id);
    setTitle(routine.title);
    setBody(routine.body);
    setMissionId(routine.mission_id);
    setAssignee(nextAssignee);
    setDispatchAfterPulse(operation.pulse?.start_immediately ?? canDispatchAssignee(nextAssignee, app.crews));
    setSkipIfActive(operation.pulse?.skip_if_active ?? true);
    setRePulseOnClose(operation.pulse?.re_pulse_on_close ?? true);
    setAutoCommitBranch(operation.auto_commit?.branch ?? "");
    setDropWorktreeOnCommit(operation.auto_commit?.drop_worktree ?? true);
    setCreatePullRequest(operation.pull_request?.create ?? false);
    setForgeKey(operation.forge?.key ?? "");
    setShipBaseBranch(operation.ship_base?.branch ?? "");
    setShipBaseReference(operation.ship_base?.reference ?? "");
    setShipBaseSync(
      operation.ship_base?.sync === "rebase" || operation.ship_base?.sync === "reset"
        ? operation.ship_base.sync
        : "merge",
    );
    setPullRequestLabels((operation.pull_request?.labels ?? []).join(", "));
    setChecksCommand((operation.checks?.commands ?? []).join("\n"));
    setChecksTimeoutSeconds(operation.checks?.timeout_seconds != null ? String(operation.checks.timeout_seconds) : "");
    setScheduleEnabled(trigger.enabled ?? true);
    setPriority(String(operation.priority ?? 0));
    setTriggerType((trigger.kind ?? "manual") === "schedule" ? "schedule" : "manual");
    setCron(trigger.cron ?? "@daily");
    setContext(routineContext(routine));
    setRequiredTags(operation.required_tags ?? []);
    setExcludedTags(operation.excluded_tags ?? []);
    setAssets([]);
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!canSave) return;
    setSaving(true);
    const input = routineInput();
    const routine = editingRoutineId ? await app.updateRoutine(editingRoutineId, input) : await app.createRoutine(input);
    setSaving(false);
    if (routine) resetForm();
  }

  async function onFiles(files: FileList | null) {
    const selected = Array.from(files ?? []);
    if (selected.length === 0 || uploading) return;
    setUploading(true);
    try {
      for (const file of selected) {
        const asset = await app.uploadAsset(file);
        if (asset) {
          setAssets((prev) => [...prev, asset]);
          setBody((prev) => appendAssetLink(prev, asset));
        }
      }
    } finally {
      setUploading(false);
      if (uploadRef.current) uploadRef.current.value = "";
    }
  }

  return (
    <div className="mx-auto grid h-full max-w-6xl gap-4 overflow-y-auto p-4 lg:grid-cols-[minmax(0,1fr)_23rem] lg:overflow-hidden">
      <Card className="flex min-h-0 flex-col">
        <CardHeader className="p-4 pb-3">
          <CardTitle className="flex items-center justify-between gap-3 text-base">
            <span className="flex items-center gap-2"><SECTION_ICONS.routines className="size-4" /> {t("routines.title")}</span>
            <span className="text-xs font-normal text-muted-foreground">{app.routines.length}</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col p-4 pt-0">
          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
            {app.routines.map((routine) => <RoutineRow key={routine.id} routine={routine} editing={editingRoutineId === routine.id} onEdit={editRoutine} />)}
            {app.routines.length === 0 && <p className="py-2 text-sm text-muted-foreground">{t("routines.emptyPeriod")}</p>}
          </div>
        </CardContent>
      </Card>

      <Card className="flex min-h-0 flex-col">
        <CardHeader className="p-4 pb-3">
          <CardTitle className="flex items-center justify-between gap-3 text-base">
            <span className="flex items-center gap-2">{editing ? <Pencil className="size-4" /> : <Plus className="size-4" />} {editing ? t("routines.edit") : t("routines.new")}</span>
            {editing && (
              <Button type="button" variant="ghost" size="icon-sm" title={t("routines.cancelEdit")} aria-label={t("routines.cancelEdit")} onClick={resetForm}>
                <X />
              </Button>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="min-h-0 flex-1 overflow-y-auto p-4 pt-0">
          {app.missions.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("routines.createMissionFirst")}</p>
          ) : (
            <form id="routine-form" className="space-y-4" onSubmit={save}>
              <FormSection title={t("routines.pulse")} icon={Activity}>
                <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("routines.pulseTitle")} />
                <div className="space-y-1.5">
                  <Textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder={t("routines.bodyPlaceholder")} rows={3} />
                  <input ref={uploadRef} type="file" multiple className="sr-only" onChange={(e) => onFiles(e.target.files)} />
                  <div className="flex items-center gap-1">
                    <Button type="button" variant="ghost" size="icon-sm" className="text-muted-foreground" title={t("routines.uploadFiles")} aria-label={t("routines.uploadFiles")} disabled={uploading} onClick={() => uploadRef.current?.click()}>
                      {uploading ? <Loader2 className="size-3 animate-spin" /> : <Paperclip className="size-3" />}
                    </Button>
                  </div>
                  <AssetChipStrip assets={assets} onInsert={(asset) => setBody((prev) => appendAssetLink(prev, asset))} />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label className="text-xs text-muted-foreground">{t("routines.pulse")}</Label>
                    <Select value={triggerType} onValueChange={(value) => setTriggerType(value as RoutineTriggerType)}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="manual">{t("routines.manual")}</SelectItem>
                        <SelectItem value="schedule">{t("routines.schedule")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  {triggerType === "schedule" && (
                    <div className="space-y-1.5 sm:col-span-2">
                      <Label className="text-xs text-muted-foreground">{t("routines.schedule")}</Label>
                      <Select value={CRON_PRESET_KEYS.some((preset) => preset.value === cron) ? cron : "custom"} onValueChange={(value) => { if (value !== "custom") setCron(value); }}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {CRON_PRESET_KEYS.map((preset) => <SelectItem key={preset.value} value={preset.value}>{t(preset.labelKey)}</SelectItem>)}
                          <SelectItem value="custom">{t("routines.custom")}</SelectItem>
                        </SelectContent>
                      </Select>
                      <Input value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 9 * * *" />
                      <div className="rounded-md border border-border bg-muted/30 p-2 text-[11px] leading-snug text-muted-foreground">
                        <div className="font-mono">{t("routines.cronAliases")}</div>
                        <div className="font-mono">{t("routines.cronFields")}</div>
                        <div>{t("routines.cronHelp")}</div>
                      </div>
                    </div>
                  )}
                </div>
              </FormSection>

              <FormSection title={t("routines.dispatch")} icon={UserRound}>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("routines.mission")}</Label>
                  <Select value={mission} onValueChange={setMissionId}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {app.missions.map((m) => <SelectItem key={m.id} value={m.id}><span className="font-mono text-xs">{m.key}</span> - {m.name}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("routines.assignee")}</Label>
                  <Select value={assignee} onValueChange={setAssigneeAndDispatch}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="me">{userLabel(app.user)}</SelectItem>
                      {sortedCrews.map((c) => <SelectItem key={`c${c.id}`} value={`crew:${c.id}`}><CrewOption crew={c} /></SelectItem>)}
                      {sortedPilots.map((p) => <SelectItem key={`p${p.kind}`} value={`pilot:${p.kind}`} disabled={p.rovers === 0}><PilotOption kind={p.kind} unavailable={p.rovers === 0} /></SelectItem>)}
                      {sortedMembers.map((m) => <SelectItem key={`u${m.id}`} value={`user:${m.id}`}><span className="flex items-center gap-2"><UserRound className="size-3.5" /> {m.name || m.email}</span></SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("routines.priority")}</Label>
                  <Select value={priority} onValueChange={setPriority}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {PRIORITY_LEVELS.map((i) => (
                        <SelectItem key={i} value={String(i)}>
                          <span className="flex items-center gap-2"><PriorityIcon level={i} className="size-3.5" /> {priorityLabel(i)}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <label className="flex items-center justify-between gap-3 text-xs">
                  <span>
                    <span className="font-medium text-foreground">{t("routines.startRightAway")}</span>
                    <span className="block text-muted-foreground">
                      {dispatchAvailable
                        ? autoDispatch
                          ? t("routines.autoAcceptOn")
                          : t("routines.autoAcceptOff")
                        : t("routines.autoAcceptDisabled")}
                    </span>
                  </span>
                  <input
                    type="checkbox"
                    className="peer sr-only"
                    checked={autoDispatch}
                    disabled={!dispatchAvailable}
                    onChange={(e) => setDispatchAfterPulse(e.target.checked)}
                  />
                  <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-disabled:opacity-50" />
                </label>
                <label className="flex items-center justify-between gap-3 text-xs">
                  <span>
                    <span className="font-medium text-foreground">{t("routines.waitCurrentTitle")}</span>
                    <span className="block text-muted-foreground">
                      {skipIfActive
                        ? t("routines.skipActiveOn")
                        : t("routines.skipActiveOff")}
                    </span>
                  </span>
                  <input type="checkbox" className="peer sr-only" checked={skipIfActive} onChange={(e) => setSkipIfActive(e.target.checked)} />
                  <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
                </label>
                <label className="flex items-center justify-between gap-3 text-xs">
                  <span>
                    <span className="font-medium text-foreground">{t("routines.keepGoingTitle")}</span>
                    <span className="block text-muted-foreground">
                      {rePulseOnClose
                        ? t("routines.rePulseOn")
                        : t("routines.rePulseOff")}
                    </span>
                  </span>
                  <input type="checkbox" className="peer sr-only" checked={rePulseOnClose} onChange={(e) => setRePulseOnClose(e.target.checked)} />
                  <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
                </label>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground" htmlFor="routine-auto-commit-branch">{t("routines.autoCommitBranch")}</Label>
                  <Input
                    id="routine-auto-commit-branch"
                    value={autoCommitBranch}
                    onChange={(e) => setAutoCommitBranch(e.target.value)}
                    placeholder={t("routines.autoCommitBranchPlaceholder")}
                    className="h-8 font-mono text-xs"
                  />
                  <p className="text-[11px] text-muted-foreground">{t("routines.autoCommitBranchHint")}</p>
                </div>
                {autoCommitBranch.trim() ? (
                  <>
                    <label className="flex items-center justify-between gap-3 text-xs">
                      <span>
                        <span className="font-medium text-foreground">{t("routines.dropWorktreeTitle")}</span>
                        <span className="block text-muted-foreground">
                          {dropWorktreeOnCommit
                            ? t("routines.dropWorktreeOn")
                            : t("routines.dropWorktreeOff")}
                        </span>
                      </span>
                      <input type="checkbox" className="peer sr-only" checked={dropWorktreeOnCommit} onChange={(e) => setDropWorktreeOnCommit(e.target.checked)} />
                      <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
                    </label>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground" htmlFor="routine-forge-key">{t("routines.forgeKey")}</Label>
                      {app.forges.length > 0 ? (
                        <Select value={forgeKey || "__auto__"} onValueChange={(v) => setForgeKey(v === "__auto__" ? "" : v)}>
                          <SelectTrigger id="routine-forge-key" className="h-8 text-xs"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            <SelectItem value="__auto__">{t("routines.forgeKeyAuto")}</SelectItem>
                            {app.forges.map((f) => (
                              <SelectItem key={f.id} value={f.key}>
                                <span className="font-mono">{f.key}</span>
                                <span className="text-muted-foreground"> · {f.repo}</span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <Input
                          id="routine-forge-key"
                          value={forgeKey}
                          onChange={(e) => setForgeKey(e.target.value)}
                          placeholder="ufo-core"
                          className="h-8 font-mono text-xs"
                        />
                      )}
                      <p className="text-[11px] text-muted-foreground">{t("routines.forgeKeyHint")}</p>
                    </div>
                    <label className="flex items-center justify-between gap-3 text-xs">
                      <span>
                        <span className="font-medium text-foreground">{t("routines.createPullRequestTitle")}</span>
                        <span className="block text-muted-foreground">
                          {createPullRequest
                            ? t("routines.createPullRequestOn")
                            : t("routines.createPullRequestOff")}
                        </span>
                      </span>
                      <input type="checkbox" className="peer sr-only" checked={createPullRequest} onChange={(e) => setCreatePullRequest(e.target.checked)} />
                      <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
                    </label>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground" htmlFor="routine-ship-base">{t("routines.shipBaseBranch")}</Label>
                      <Input
                        id="routine-ship-base"
                        value={shipBaseBranch}
                        onChange={(e) => setShipBaseBranch(e.target.value)}
                        placeholder={t("routines.shipBaseBranchPlaceholder")}
                        className="h-8 font-mono text-xs"
                      />
                      <p className="text-[11px] text-muted-foreground">{t("routines.shipBaseBranchHint")}</p>
                    </div>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground" htmlFor="routine-ship-ref">{t("routines.shipBaseReference")}</Label>
                      <Input
                        id="routine-ship-ref"
                        value={shipBaseReference}
                        onChange={(e) => setShipBaseReference(e.target.value)}
                        placeholder={t("routines.shipBaseReferencePlaceholder")}
                        className="h-8 font-mono text-xs"
                      />
                      <p className="text-[11px] text-muted-foreground">{t("routines.shipBaseReferenceHint")}</p>
                    </div>
                    {shipBaseReference.trim() ? (
                      <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground" htmlFor="routine-ship-sync">{t("routines.shipBaseSync")}</Label>
                        <select
                          id="routine-ship-sync"
                          value={shipBaseSync}
                          onChange={(e) => setShipBaseSync(e.target.value as "rebase" | "merge" | "reset")}
                          className="h-8 w-full rounded-md border border-input bg-background px-2 font-mono text-xs"
                        >
                          <option value="merge">{t("routines.shipBaseSyncMerge")}</option>
                          <option value="rebase">{t("routines.shipBaseSyncRebase")}</option>
                          <option value="reset">{t("routines.shipBaseSyncReset")}</option>
                        </select>
                        <p className="text-[11px] text-muted-foreground">{t("routines.shipBaseSyncHint")}</p>
                      </div>
                    ) : null}
                    {createPullRequest ? (
                      <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground" htmlFor="routine-pr-labels">{t("routines.pullRequestLabels")}</Label>
                        <Input
                          id="routine-pr-labels"
                          value={pullRequestLabels}
                          onChange={(e) => setPullRequestLabels(e.target.value)}
                          placeholder={t("routines.pullRequestLabelsPlaceholder")}
                          className="h-8 text-xs"
                        />
                      </div>
                    ) : null}
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground" htmlFor="routine-checks-commands">{t("routines.checksCommand")}</Label>
                      <Textarea
                        id="routine-checks-commands"
                        value={checksCommand}
                        onChange={(e) => setChecksCommand(e.target.value)}
                        placeholder={t("routines.checksCommandPlaceholder")}
                        rows={3}
                        className="font-mono text-xs"
                      />
                      <p className="text-[11px] text-muted-foreground">{t("routines.checksCommandHint")}</p>
                    </div>
                    {checksCommand.trim() ? (
                      <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground" htmlFor="routine-checks-timeout">{t("routines.checksTimeout")}</Label>
                        <Input
                          id="routine-checks-timeout"
                          type="number"
                          min={1}
                          value={checksTimeoutSeconds}
                          onChange={(e) => setChecksTimeoutSeconds(e.target.value)}
                          placeholder="1200"
                          className="h-8 font-mono text-xs"
                        />
                      </div>
                    ) : null}
                  </>
                ) : null}
                {triggerType === "schedule" && (
                  <label className="flex items-center justify-between gap-3 text-xs">
                    <span>
                      <span className="font-medium text-foreground">{t("routines.runOnSchedule")}</span>
                      <span className="block text-muted-foreground">{scheduleEnabled ? t("routines.firesOnCron") : t("routines.pausedTimer")}</span>
                    </span>
                    <input type="checkbox" className="peer sr-only" checked={scheduleEnabled} onChange={(e) => setScheduleEnabled(e.target.checked)} />
                    <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
                  </label>
                )}
              </FormSection>

              <FormSection title={t("routines.context")} icon={Tags}>
                <div className="space-y-1.5">
                  <Textarea value={context} onChange={(e) => setContext(e.target.value)} placeholder={t("routines.context")} rows={3} />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("routines.requiredTags")}</Label>
                  <TagEditor tags={requiredTags} onChange={setRequiredTags} placeholder={t("routines.tagPlaceholder")} />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("routines.excludedTags")}</Label>
                  <TagEditor tags={excludedTags} onChange={setExcludedTags} placeholder={t("routines.tagPlaceholder")} />
                </div>
              </FormSection>
            </form>
          )}
        </CardContent>
        {app.missions.length > 0 && (
          <CardFooter className="border-t border-border p-4">
            <Button type="submit" form="routine-form" className="w-full" disabled={saving || !canSave}><SaveIcon /> {saving ? t("routines.saving") : editing ? t("routines.saveChanges") : t("routines.save")}</Button>
          </CardFooter>
        )}
      </Card>
    </div>
  );
}

function FormSection({ title, icon: Icon, children }: { title: string; icon: LucideIcon; children: React.ReactNode }) {
  return (
    <section className="space-y-2 border-t border-border pt-3 first:border-t-0 first:pt-0">
      <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
        <Icon className="size-3.5" />
        {title}
      </div>
      <div className="space-y-2">{children}</div>
    </section>
  );
}

function RoutineRow({ routine, editing, onEdit }: { routine: Routine; editing: boolean; onEdit: (routine: Routine) => void }) {
  const app = useApp();
  const t = useT();
  const [pulsing, setPulsing] = useState(false);
  const [history, setHistory] = useState<Pulse[] | null>(null);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const mission = app.missions.find((m) => m.id === routine.mission_id);
  const trigger = routineTrigger(routine);
  const operation = routineOperation(routine);
  const requiredTags = operation.required_tags ?? [];
  const excludedTags = operation.excluded_tags ?? [];
  const context = routineContext(routine);
  const triggerType = trigger.kind ?? "manual";
  const scheduleOn = trigger.enabled ?? true;
  const priority = operation.priority ?? 0;
  const skipActive = operation.pulse?.skip_if_active ?? true;
  const rePulse = operation.pulse?.re_pulse_on_close ?? true;
  const autoCommit = (operation.auto_commit?.branch ?? "").trim();
  const dropWorktree = operation.auto_commit?.drop_worktree ?? true;
  const createPR = operation.pull_request?.create ?? false;
  const prBase = (operation.ship_base?.branch ?? "").trim();
  const shipRef = (operation.ship_base?.reference ?? "").trim();
  const shipSync =
    operation.ship_base?.sync === "rebase" || operation.ship_base?.sync === "reset"
      ? operation.ship_base.sync
      : "merge";
  const prLabels = operation.pull_request?.labels ?? [];
  const checksCmds = operation.checks?.commands ?? [];
  const checksCmd = checksCmds.join("; ");

  async function pulse() {
    setPulsing(true);
    try {
      const pulse = await app.pulseRoutine(routine.id);
      if (pulse?.operation_id) app.openOperation(pulse.operation_id);
      if (history) {
        const next = await app.listRoutinePulses(routine.id);
        setHistory(next);
      }
    } finally {
      setPulsing(false);
    }
  }

  async function toggleHistory() {
    if (history) {
      setHistory(null);
      return;
    }
    setLoadingHistory(true);
    try {
      setHistory(await app.listRoutinePulses(routine.id));
    } finally {
      setLoadingHistory(false);
    }
  }

  async function toggleSchedule() {
    const nextEnabled = !scheduleOn;
    await app.updateRoutine(routine.id, {
      mission_id: routine.mission_id,
      title: routine.title,
      body: routine.body,
      metadata: {
        ...routine.metadata,
        trigger: {
          kind: "schedule",
          cron: trigger.cron,
          enabled: nextEnabled,
        },
        operation: {
          ...operation,
          pulse: {
            start_immediately: operation.pulse?.start_immediately ?? true,
            skip_if_active: skipActive,
            re_pulse_on_close: rePulse,
          },
          ...(autoCommit
            ? {
                auto_commit: {
                  branch: autoCommit,
                  drop_worktree: dropWorktree,
                },
                ...((prBase || shipRef)
                  ? {
                      ship_base: {
                        ...(prBase ? { branch: prBase } : {}),
                        ...(shipRef ? { reference: shipRef } : {}),
                        sync: shipSync,
                      },
                    }
                  : {}),
                pull_request: {
                  create: createPR,
                  ...(createPR && prLabels.length ? { labels: prLabels } : {}),
                },
                ...(checksCmds.length
                  ? {
                      checks: {
                        commands: checksCmds,
                        ...(operation.checks?.timeout_seconds != null ? { timeout_seconds: operation.checks.timeout_seconds } : {}),
                      },
                    }
                  : {}),
              }
            : {}),
        },
      },
      operation_metadata: routine.operation_metadata,
    });
  }

  const nextFallback = triggerType === "schedule" ? (scheduleOn ? t("routines.pending") : t("routines.paused")) : t("routines.manual");

  return (
    <div className={`rounded-md border p-3 text-sm ${editing ? "border-brand" : "border-border"}`}>
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <div className="truncate font-medium" title={routine.title}>{routine.title}</div>
            <Badge variant={triggerType === "schedule" ? "brand" : "secondary"} className="shrink-0 text-[10px]">
              {triggerType === "schedule" ? (scheduleOn ? t("routines.schedule") : t("routines.paused")) : t("routines.manual")}
            </Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span className="min-w-0 truncate">
              {mission ? <><span className="font-mono">{mission.key}</span> - {mission.name}</> : t("routines.mission")}
            </span>
            <span>{routineAssigneeLabel(routine, app, t("common.crew"), t("common.unassigned"))}</span>
            <span>{operation.pulse?.start_immediately ?? true ? t("routines.startRightAway") : t("routines.createOnly")}</span>
            <span>{skipActive ? t("routines.waitFinish") : t("routines.allowOverlap")}</span>
            <span>{rePulse ? t("routines.keepGoing") : t("routines.stopFinished")}</span>
            {autoCommit ? <span className="font-mono">{t("routines.autoCommitTo", { branch: autoCommit })}</span> : null}
            {autoCommit ? <span>{dropWorktree ? t("routines.dropWorktreeShort") : t("routines.keepWorktreeShort")}</span> : null}
            {autoCommit ? <span className="font-mono">{t("routines.shipBaseShort", { base: prBase || t("routines.shipBaseBranchPlaceholder") })}</span> : null}
            {autoCommit && shipRef ? <span className="font-mono">{t("routines.shipBaseTrackShort", { reference: shipRef, sync: shipSync })}</span> : null}
            {autoCommit && createPR ? <span>{t("routines.createPullRequestShort", { base: prBase || t("routines.shipBaseBranchPlaceholder") })}</span> : null}
            {autoCommit && !createPR ? <span>{t("routines.localShipShort")}</span> : null}
            {checksCmd ? <span className="font-mono truncate max-w-[12rem]" title={checksCmd}>{t("routines.checksShort")}</span> : null}
            <span className="flex items-center gap-1"><PriorityIcon level={priority} className="size-3.5" /> {priorityLabel(priority)}</span>
          </div>
          <div className="mt-2 grid gap-1 text-[11px] text-muted-foreground sm:grid-cols-2">
            <span className="min-w-0 truncate">{triggerType === "schedule" ? t("routines.scheduleCron", { cron: trigger.cron ?? "" }) : t("routines.manualPulse")}</span>
            <span className="min-w-0 truncate">{t("routines.nextPulse", { time: formatPulseTime(routine.next_pulse_at, nextFallback) })}</span>
            <span className="min-w-0 truncate sm:col-span-2">{t("routines.lastPulsed", { time: formatPulseTime(routine.last_pulsed_at, t("common.never")) })}</span>
          </div>
        </div>
        <Button variant="ghost" size="icon-sm" title={t("routines.pulseHistory")} aria-label={t("routines.pulseHistory")} onClick={toggleHistory} disabled={loadingHistory}>
          {loadingHistory ? <Loader2 className="animate-spin" /> : <Activity />}
        </Button>
        {triggerType === "schedule" && (
          <Button variant="ghost" size="icon-sm" title={scheduleOn ? t("routines.pauseSchedule") : t("routines.resumeSchedule")} aria-label={scheduleOn ? t("routines.pauseSchedule") : t("routines.resumeSchedule")} onClick={toggleSchedule}>
            {scheduleOn ? <Pause /> : <Play />}
          </Button>
        )}
        <Button variant="ghost" size="icon-sm" title={t("routines.edit")} aria-label={t("routines.edit")} onClick={() => onEdit(routine)}><Pencil /></Button>
        <Button variant="ghost" size="icon-sm" title={t("routines.pulseRoutine")} aria-label={t("routines.pulseRoutine")} onClick={pulse} disabled={pulsing}><Play /></Button>
        <Button variant="ghost" size="icon-sm" title={t("routines.delete")} aria-label={t("routines.delete")} onClick={() => app.deleteRoutine(routine.id)}><Trash2 /></Button>
      </div>

      {(requiredTags.length > 0 || excludedTags.length > 0) && (
        <div className="mt-2 space-y-1">
          {requiredTags.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="w-14 shrink-0 text-[11px] uppercase text-muted-foreground">{t("routines.require")}</span>
              <TagList tags={requiredTags} />
            </div>
          )}
          {excludedTags.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="w-14 shrink-0 text-[11px] uppercase text-muted-foreground">{t("routines.exclude")}</span>
              <TagList tags={excludedTags} />
            </div>
          )}
        </div>
      )}
      {context && <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">{t("routines.contextPrefix", { context })}</p>}
      {routine.body && <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">{routine.body}</p>}
      {history && (
        <div className="mt-2 space-y-1 border-t border-border pt-2">
          <div className="text-[11px] font-medium uppercase text-muted-foreground">{t("routines.history")}</div>
          {history.length === 0 && <p className="text-xs text-muted-foreground">{t("routines.noHistoryPeriod")}</p>}
          {history.map((p) => (
            <div key={p.id} className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
              <span className="min-w-0 truncate">
                <Badge variant="secondary" className="mr-1 text-[10px]">{p.status}</Badge>
                {formatPulseTime(p.finished_at ?? p.created_at, p.created_at)}
              </span>
              {p.operation_id ? (
                <button type="button" className="shrink-0 font-mono text-[11px] text-brand hover:underline" onClick={() => app.openOperation(p.operation_id!)}>
                  {t("routines.open")}
                </button>
              ) : (
                <span className="shrink-0 text-[11px]">-</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function routineAssigneeLabel(routine: Routine, app: ReturnType<typeof useApp>, crewFallback: string, unassigned: string) {
  const assignee = routineOperation(routine).assignee;
  if (assignee?.type === "pilot") return pilotLabel(assignee.id ?? "");
  if (assignee?.type === "crew") return app.crews.find((c) => c.id === assignee.id)?.name ?? crewFallback;
  if (assignee?.type === "user") return memberLabel(assignee.id ?? "", app.user, app.members);
  return unassigned;
}
