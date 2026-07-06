"use client";

import { useEffect, useState } from "react";
import { Check, Circle, CircleDot, CircleOff, X, type LucideIcon } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TagEditor, TagList } from "@/components/tag-editor";
import { PilotIcon } from "@/components/pilot-icon";
import { t as translate, useT, type MessageKey } from "@/lib/i18n";
import { pilotLabel } from "@/lib/labels";
import { SECTION_ICONS } from "@/lib/section-icons";
import { cn } from "@/lib/utils";
import { BudgetEditor } from "@/components/budget-editor";

const ROVER_STATUS: Record<string, { labelKey: MessageKey; icon: LucideIcon; color: string }> = {
  online: { labelKey: "rovers.online", icon: Circle, color: "text-success" },
  full: { labelKey: "rovers.full", icon: CircleDot, color: "text-warning" },
  offline: { labelKey: "rovers.offline", icon: CircleOff, color: "text-muted-foreground" },
};
const EXPIRY_OPTIONS: { value: string; labelKey: MessageKey }[] = [
  { value: "1", labelKey: "rovers.expiry.1" },
  { value: "3", labelKey: "rovers.expiry.3" },
  { value: "7", labelKey: "rovers.expiry.7" },
  { value: "15", labelKey: "rovers.expiry.15" },
  { value: "30", labelKey: "rovers.expiry.30" },
  { value: "90", labelKey: "rovers.expiry.90" },
  { value: "180", labelKey: "rovers.expiry.180" },
  { value: "365", labelKey: "rovers.expiry.365" },
  { value: "never", labelKey: "rovers.labels.never" },
];
const MAX_ROVER_UNITS = 100;
const MAX_ENROLLMENT_CODE_USES = 100;
const WEB_ENROLLMENT_CODE_RE = /^[a-f0-9]{40}$/;

function shortDate(value?: string | null) {
  return value ? new Date(value).toLocaleDateString() : translate("rovers.labels.never");
}

function expiryISO(value: string) {
  if (value === "never") return undefined;
  const days = Number(value);
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
}

function clampUnits(value: unknown) {
  const n = typeof value === "number" ? value : Number.parseInt(String(value ?? ""), 10);
  return Number.isInteger(n) && n > 0 ? Math.min(n, MAX_ROVER_UNITS) : 1;
}

function metadataTags(metadata: Record<string, unknown> | undefined) {
  const tags = metadata?.tags;
  return Array.isArray(tags) ? tags.filter((tag): tag is string => typeof tag === "string" && tag.trim() !== "") : [];
}

function budgetSummary(metadata: Record<string, unknown> | undefined): string | null {
  const raw = metadata?.budget;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const budget = raw as Record<string, unknown>;
  const period = typeof budget.period === "string" && budget.period.trim() ? budget.period : "calendar_week";
  const periodLabel = period === "calendar_month" ? translate("budget.month") : period === "calendar_week" ? translate("budget.week") : period;
  const parts: string[] = [];
  if (typeof budget.max_runs === "number" && budget.max_runs > 0) parts.push(translate("rovers.budgetRuns", { count: budget.max_runs, period: periodLabel }));
  if (typeof budget.max_tokens === "number" && budget.max_tokens > 0) parts.push(translate("rovers.budgetTokens", { count: budget.max_tokens, period: periodLabel }));
  if (typeof budget.max_usd_micros === "number" && budget.max_usd_micros > 0) {
    parts.push(`$${(budget.max_usd_micros / 1_000_000).toFixed(2)}/${periodLabel}`);
  }
  return parts.length > 0 ? parts.join(" · ") : null;
}

type PendingEnrollment = {
  code: string;
  name: string;
  units: string;
  tags: string[];
  fleet: string;
};

function pendingEnrollmentFromHash(fleet: string): PendingEnrollment | null {
  if (typeof window === "undefined" || !window.location.hash.startsWith("#enroll=")) return null;
  const params = new URLSearchParams(window.location.hash.slice(1));
  const code = params.get("enroll")?.trim() ?? "";
  if (!code) return null;
  if (!WEB_ENROLLMENT_CODE_RE.test(code)) {
    clearPendingEnrollmentHash();
    return null;
  }
  const tags = params.getAll("tag").map((tag) => tag.trim()).filter(Boolean);
  return {
    code,
    name: params.get("name")?.trim() ?? "",
    units: params.get("units")?.trim() ?? "",
    tags,
    fleet,
  };
}

function clearPendingEnrollmentHash() {
  if (typeof window === "undefined") return;
  window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
}

function RoverName({ id, name, onRename }: { id: string; name: string; onRename: (id: string, name: string) => void }) {
  const t = useT();
  const [value, setValue] = useState(name);
  const [editing, setEditing] = useState(false);
  useEffect(() => setValue(name), [name]);
  const save = () => {
    const next = value.trim();
    if (next && next !== name) onRename(id, next);
    else setValue(name);
    setEditing(false);
  };
  if (!editing) {
    return (
      <button type="button" className="h-7 w-44 truncate px-1 text-left text-sm font-medium text-foreground" title={name} onClick={() => setEditing(true)}>
        {name}
      </button>
    );
  }
  return (
    <Input
      autoFocus
      aria-label={t("rovers.nameAria")}
      className="h-7 w-44 border-transparent px-1 shadow-none"
      value={value}
      onBlur={save}
      onChange={(e) => setValue(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
        if (e.key === "Escape") {
          setValue(name);
          setEditing(false);
        }
      }}
    />
  );
}

function RoverUnits({ id, units, running, onSet }: { id: string; units: number; running: number; onSet: (id: string, units: number) => void }) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(String(units));
  const label = `${units} ${units === 1 ? t("rovers.unit") : t("rovers.unitsPlural")}`;
  useEffect(() => setValue(String(units)), [units]);
  const save = () => {
    const next = Number(value);
    if (Number.isInteger(next) && next >= 1 && next <= MAX_ROVER_UNITS && next !== units) onSet(id, next);
    else setValue(String(units));
    setEditing(false);
  };
  if (!editing) {
    return (
      <button
        type="button"
        className="h-7 w-20 truncate px-1 text-left text-xs text-muted-foreground"
        title={t("rovers.runningOf", { running, label })}
        onClick={() => setEditing(true)}
      >
        {label}
      </button>
    );
  }
  return (
    <Input
      autoFocus
      type="number"
      min={1}
      max={MAX_ROVER_UNITS}
      aria-label={t("rovers.unitsAria")}
      className="h-7 w-20 border-transparent px-1 text-xs shadow-none"
      value={value}
      onBlur={save}
      onChange={(e) => setValue(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
        if (e.key === "Escape") {
          setValue(String(units));
          setEditing(false);
        }
      }}
    />
  );
}

export function RoversView() {
  const app = useApp();
  const t = useT();
  const [activePendingID, setActivePendingID] = useState<string | null>(null);
  const [pendingFleet, setPendingFleet] = useState(app.fleet);
  const [pendingName, setPendingName] = useState("");
  const [pendingUnits, setPendingUnits] = useState("1");
  const [pendingTags, setPendingTags] = useState<string[]>([]);
  const [revokingRover, setRevokingRover] = useState<{ id: string; name: string } | null>(null);
  const [enrollmentCodeName, setEnrollmentCodeName] = useState("");
  const [enrollmentCodeExpiry, setEnrollmentCodeExpiry] = useState("30");
  const [enrollmentCodeUses, setEnrollmentCodeUses] = useState("");
  const uses = Number(enrollmentCodeUses);
  const hasMultiUseFields = enrollmentCodeName.trim() !== "" || enrollmentCodeUses !== "";
  const canCreateCode = !hasMultiUseFields || (enrollmentCodeName.trim() !== "" && Number.isInteger(uses) && uses >= 2 && uses <= MAX_ENROLLMENT_CODE_USES);
  const runningSlots = app.rovers.reduce((n, r) => n + (r.running_units ?? 0), 0);
  const totalSlots = app.rovers.reduce((n, r) => n + r.units, 0);
  const pendingApprovals = app.enrollmentCodes.filter((code) => code.kind === "web:pending");
  const enrollmentCodes = app.enrollmentCodes.filter((code) => code.kind === "code:approved" && code.fleet_id === app.fleet);
  const activePending = pendingApprovals.find((code) => code.id === activePendingID) ?? null;

  useEffect(() => {
    const next = pendingEnrollmentFromHash(app.fleet);
    if (next == null) return;
    const units = clampUnits(next.units);
    let canceled = false;
    app.savePendingRover(next.code, { name: next.name, units, tags: next.tags }).then((code) => {
      if (canceled || code == null) return;
      clearPendingEnrollmentHash();
      setActivePendingID(code.id);
      setPendingFleet(next.fleet);
      setPendingName(code.name || next.name);
      setPendingUnits(String(clampUnits(code.metadata?.units ?? units)));
      setPendingTags(metadataTags(code.metadata));
    });
    return () => {
      canceled = true;
    };
  }, [app.fleet, app.savePendingRover]);

  useEffect(() => {
    if (!activePending) return;
    setPendingFleet(app.fleet);
    setPendingName(activePending.name);
    setPendingUnits(String(clampUnits(activePending.metadata?.units)));
    setPendingTags(metadataTags(activePending.metadata));
  }, [activePending?.id, app.fleet]);

  const approvePendingEnrollment = async () => {
    if (!activePending) return;
    const units = clampUnits(pendingUnits);
    const ok = await app.approvePendingRover(activePending.id, {
      fleetId: pendingFleet,
      name: pendingName,
      units,
      tags: pendingTags,
    });
    if (!ok) return;
    if (pendingFleet !== app.fleet) app.switchFleet(pendingFleet);
    setActivePendingID(null);
  };

  const denyPendingEnrollment = async () => {
    if (!activePending) return;
    const ok = await app.denyPendingRover(activePending.id);
    if (!ok) return;
    setActivePendingID(null);
  };

  return (
    <div className="mx-auto flex h-full max-w-3xl flex-col p-4">
      <Dialog open={activePending != null} onOpenChange={(open) => { if (!open) setActivePendingID(null); }}>
        <DialogContent
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle>{t("rovers.approveTitle")}</DialogTitle>
            <DialogDescription>
              {t("rovers.approveDesc")}
            </DialogDescription>
          </DialogHeader>
          {activePending && (
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-[1fr_8rem]">
                <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                  {t("rovers.name")}
                  <Input value={pendingName} onChange={(e) => setPendingName(e.target.value)} placeholder={t("rovers.namePlaceholder")} />
                </label>
                <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                  {t("rovers.units")}
                  <Input type="number" min={1} max={MAX_ROVER_UNITS} value={pendingUnits} onChange={(e) => setPendingUnits(e.target.value)} />
                </label>
              </div>
              <label className="space-y-1.5 text-xs font-medium text-muted-foreground">
                {t("rovers.fleet")}
                <Select value={pendingFleet} onValueChange={setPendingFleet}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {app.fleets.map((fleet) => <SelectItem key={fleet.id} value={fleet.id}>{fleet.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </label>
              <div className="space-y-1.5">
                <p className="text-xs font-medium text-muted-foreground">{t("rovers.tags")}</p>
                <TagEditor tags={pendingTags} onChange={setPendingTags} />
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="destructive" onClick={denyPendingEnrollment}><X />{t("rovers.deny")}</Button>
                <Button variant="brand" disabled={!pendingFleet} onClick={approvePendingEnrollment}><Check />{t("rovers.approve")}</Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={revokingRover != null} onOpenChange={(open) => { if (!open) setRevokingRover(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("rovers.revokeTitle")}</DialogTitle>
            <DialogDescription>
              {t("rovers.revokeConfirm", { name: revokingRover?.name || t("rovers.thisRover") })}
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setRevokingRover(null)}>{t("common.cancel")}</Button>
            <Button variant="destructive" onClick={() => { if (revokingRover) app.revokeRover(revokingRover.id); setRevokingRover(null); }}>{t("rovers.revoke")}</Button>
          </div>
        </DialogContent>
      </Dialog>
      <Card className="flex min-h-0 flex-1 flex-col">
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><SECTION_ICONS.rovers className="size-4" /> {t("rovers.title")}</CardTitle></CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col space-y-4">
          {pendingApprovals.length > 0 && (
            <div className="space-y-2 rounded-md border border-brand/30 bg-brand/10 p-3">
              <p className="text-sm font-medium text-foreground">{t("rovers.pendingApprovals")}</p>
              {pendingApprovals.map((code) => {
                const units = clampUnits(code.metadata?.units);
                const tags = metadataTags(code.metadata);
                return (
                  <div key={code.id} className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                    <span className="min-w-0 truncate">
                      {code.name || t("rovers.unnamed")} · {units} {units === 1 ? t("rovers.unit") : t("rovers.unitsPlural")}
                      {tags.length > 0 && ` · ${tags.join(", ")}`}
                      {` · ${t("rovers.expires", { date: shortDate(code.expires_at) })}`}
                    </span>
                    <span className="flex items-center gap-2">
                      <Button size="sm" variant="outline" onClick={() => setActivePendingID(code.id)}>{t("rovers.review")}</Button>
                      <Button size="sm" variant="ghost" className="text-destructive" onClick={() => app.denyPendingRover(code.id)}>{t("rovers.deny")}</Button>
                    </span>
                  </div>
                );
              })}
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={() => app.createEnrollmentCode({ name: enrollmentCodeName.trim(), expiresAt: expiryISO(enrollmentCodeExpiry), uses })} disabled={!canCreateCode}>{t("rovers.createCode")}</Button>
            <Input value={enrollmentCodeName} onChange={(e) => setEnrollmentCodeName(e.target.value)} className="h-8 w-40" placeholder={t("rovers.codeName")} />
            <Select value={enrollmentCodeExpiry} onValueChange={setEnrollmentCodeExpiry}>
              <SelectTrigger className="h-8 w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                {EXPIRY_OPTIONS.map((opt) => <SelectItem key={opt.value} value={opt.value}>{t(opt.labelKey)}</SelectItem>)}
              </SelectContent>
            </Select>
            <Input type="number" min={2} max={MAX_ENROLLMENT_CODE_USES} value={enrollmentCodeUses} onChange={(e) => setEnrollmentCodeUses(e.target.value)} className="h-8 w-28" placeholder={t("rovers.uses")} />
          </div>
          {app.newEnrollmentCode && (
            <pre className="overflow-x-auto rounded-md bg-foreground/90 p-3 text-xs text-background">
              {`UFO_ROVER_ENROLLMENT_CODE=${app.newEnrollmentCode} ufo rover enroll`}
            </pre>
          )}
          <div className="text-sm text-muted-foreground">
            {t("rovers.hubSlots", { running: runningSlots, total: totalSlots })}
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto divide-y divide-border pr-1">
            {app.rovers.map((r) => {
              const statusMeta = ROVER_STATUS[r.status];
              const statusLabel = statusMeta ? t(statusMeta.labelKey) : r.status;
              const StatusIcon = statusMeta?.icon ?? Circle;
              const statusColor = statusMeta?.color ?? "text-muted-foreground";
              const budget = budgetSummary(r.metadata);
              return (
                <div key={r.id} className="space-y-2 py-3 text-sm">
                  <div className="flex items-center justify-between">
                    <span className="flex min-w-0 flex-wrap items-center gap-2">
                      <RoverName id={r.id} name={r.name} onRename={app.renameRover} />
                      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" title={t("rovers.statusTitle", { status: statusLabel })}>
                        <StatusIcon aria-hidden className={cn("size-3.5", statusColor)} />
                        {statusLabel}
                      </span>
                      <RoverUnits id={r.id} units={r.units} running={r.running_units ?? 0} onSet={app.setRoverUnits} />
                      {budget && (
                        <span className="truncate text-xs text-muted-foreground" title={t("rovers.optionalCap")}>
                          {t("rovers.cap", { budget })}
                        </span>
                      )}
                    </span>
                    <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setRevokingRover({ id: r.id, name: r.name })}>{t("rovers.revoke")}</Button>
                  </div>
                  <div className="space-y-1 pl-4">
                    {(() => {
                      const auto = r.auto_tags ?? [];
                      const pilots = auto.filter((tag) => tag.startsWith("pilot:")).map((tag) => tag.slice(6));
                      const autoTags = auto.filter((tag) => !tag.startsWith("pilot:"));
                      return (
                        <div className="grid gap-x-3 gap-y-1.5 text-xs sm:grid-cols-[4.75rem_minmax(0,1fr)]">
                          {pilots.length > 0 && (
                            <>
                              <span className="pt-1 text-[11px] uppercase text-muted-foreground">{t("rovers.pilots")}</span>
                              <PilotIconList pilots={pilots} />
                            </>
                          )}
                          {autoTags.length > 0 && (
                            <>
                              <span className="pt-1 text-[11px] uppercase text-muted-foreground">{t("rovers.autoTags")}</span>
                              <TagList tags={autoTags} />
                            </>
                          )}
                          <span className="pt-1 text-[11px] uppercase text-muted-foreground">{t("rovers.userTags")}</span>
                          <TagEditor tags={r.tags ?? []} onChange={(tags) => app.setRoverTags(r.id, tags)} />
                        </div>
                      );
                    })()}
                    {(app.myRole === "owner" || app.myRole === "admin") && (
                      <div className="pt-2">
                        <BudgetEditor metadata={r.metadata} onSave={(b) => app.setRoverBudget(r.id, b)} />
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
            {app.rovers.length === 0 && <p className="py-2 text-sm text-muted-foreground">{t("rovers.emptyEnrolled")}</p>}
          </div>
          {enrollmentCodes.length > 0 && (
            <div className="space-y-1 border-t border-border pt-3">
              <p className="text-xs font-medium text-muted-foreground">{t("rovers.enrollmentCodes")}</p>
              {enrollmentCodes.map((code) => (
                <div key={code.id} className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>
                    {code.name || t("rovers.oneTime")} · {code.code} · {t("rovers.usesLeft", { count: code.remaining_uses, noun: code.remaining_uses === 1 ? t("rovers.use") : t("rovers.usesPlural") })} · {t("rovers.created", { date: shortDate(code.created_at) })} · {t("rovers.expiresLabel", { date: shortDate(code.expires_at) })}
                  </span>
                  <Button variant="ghost" size="icon-sm" onClick={() => app.revokeEnrollmentCode(code.id)}>×</Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function PilotIconList({ pilots }: { pilots: string[] }) {
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
      {pilots.map((kind) => (
        <span key={kind} className="inline-flex size-6 items-center justify-center rounded-full border border-border bg-muted/30 text-muted-foreground" aria-label={pilotLabel(kind)}>
          <PilotIcon kind={kind} size={14} />
        </span>
      ))}
    </span>
  );
}
