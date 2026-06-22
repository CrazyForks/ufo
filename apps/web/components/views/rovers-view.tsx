"use client";

import { useEffect, useState } from "react";
import { Circle, CircleDot, CircleOff, Gauge, type LucideIcon } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TagEditor, TagList } from "@/components/tag-editor";
import { PilotIcon } from "@/components/pilot-icon";
import { pilotLabel } from "@/lib/labels";
import { SECTION_ICONS } from "@/lib/section-icons";
import { cn } from "@/lib/utils";

const ROVER_STATUS: Record<string, { label: string; icon: LucideIcon; color: string }> = {
  online: { label: "idle", icon: Circle, color: "text-success" },
  busy: { label: "active", icon: CircleDot, color: "text-info" },
  offline: { label: "offline", icon: CircleOff, color: "text-muted-foreground" },
};
const EXPIRY_OPTIONS = [
  ["1", "1 day"],
  ["3", "3 days"],
  ["7", "7 days"],
  ["15", "15 days"],
  ["30", "30 days"],
  ["90", "90 days"],
  ["180", "180 days"],
  ["365", "1 year"],
  ["never", "Never"],
] as const;

function shortDate(value?: string | null) {
  return value ? new Date(value).toLocaleDateString() : "never";
}

function expiryISO(value: string) {
  if (value === "never") return undefined;
  const days = Number(value);
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
}

function RoverName({ id, name, onRename }: { id: string; name: string; onRename: (id: string, name: string) => void }) {
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
      aria-label="Rover name"
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

export function RoversView() {
  const app = useApp();
  const [enrollmentCodeName, setEnrollmentCodeName] = useState("");
  const [enrollmentCodeExpiry, setEnrollmentCodeExpiry] = useState("30");
  const [enrollmentCodeUses, setEnrollmentCodeUses] = useState("");
  const uses = Number(enrollmentCodeUses);
  const wantsReusableCode = enrollmentCodeName.trim() !== "" || enrollmentCodeUses !== "";
  const canCreateCode = !wantsReusableCode || (enrollmentCodeName.trim() !== "" && Number.isInteger(uses) && uses >= 2 && uses <= 1000);
  const busyUnits = app.rovers.reduce((n, r) => n + (r.busy_units ?? 0), 0);
  const totalUnits = app.rovers.reduce((n, r) => n + r.units, 0);

  return (
    <div className="mx-auto flex h-full max-w-3xl flex-col p-4">
      <Card className="flex min-h-0 flex-1 flex-col">
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><SECTION_ICONS.rovers className="size-4" /> Rovers</CardTitle></CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={() => app.createEnrollmentCode({ name: enrollmentCodeName.trim(), expiresAt: expiryISO(enrollmentCodeExpiry), uses })} disabled={!canCreateCode}>Create enrollment code</Button>
            <Input value={enrollmentCodeName} onChange={(e) => setEnrollmentCodeName(e.target.value)} className="h-8 w-40" placeholder="Name" />
            <Select value={enrollmentCodeExpiry} onValueChange={setEnrollmentCodeExpiry}>
              <SelectTrigger className="h-8 w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                {EXPIRY_OPTIONS.map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
              </SelectContent>
            </Select>
            <Input type="number" min={2} max={1000} value={enrollmentCodeUses} onChange={(e) => setEnrollmentCodeUses(e.target.value)} className="h-8 w-28" placeholder="Uses" />
          </div>
          {app.newEnrollmentCode && (
            <pre className="overflow-x-auto rounded-md bg-foreground/90 p-3 text-xs text-background">
              {`UFO_ROVER_ENROLLMENT_CODE=${app.newEnrollmentCode} scripts/dev.sh rover`}
            </pre>
          )}
          <div className="text-sm text-muted-foreground">
            <span className="font-medium text-foreground">{busyUnits}</span> / {totalUnits} units busy
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto divide-y divide-border pr-1">
            {app.rovers.map((r) => {
              const state = ROVER_STATUS[r.status] ?? { label: r.status, icon: Circle, color: "text-muted-foreground" };
              const StateIcon = state.icon;
              return (
                <div key={r.id} className="space-y-2 py-3 text-sm">
                  <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2">
                      <RoverName id={r.id} name={r.name} onRename={app.renameRover} />
                      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" title={`Rover ${state.label}`}>
                        <StateIcon aria-hidden className={cn("size-3.5", state.color)} />
                        {state.label}
                      </span>
                      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" title={`${r.busy_units ?? 0} of ${r.units} units busy`}>
                        <Gauge aria-hidden className="size-3.5" />
                        {r.busy_units ?? 0}/{r.units} {r.units === 1 ? "unit" : "units"} busy
                      </span>
                    </span>
                    <Button variant="ghost" size="sm" className="text-destructive" onClick={() => app.revokeRover(r.id)}>Revoke</Button>
                  </div>
                  <div className="space-y-1 pl-4">
                    {(() => {
                      const auto = r.auto_tags ?? [];
                      const pilots = auto.filter((t) => t.startsWith("pilot:")).map((t) => t.slice(6));
                      return (
                        <>
                          {pilots.length > 0 && (
                            <div className="flex items-center gap-2">
                              <span className="w-12 shrink-0 text-[11px] uppercase text-muted-foreground">pilots</span>
                              <span className="flex flex-wrap items-center gap-2 text-xs">
                                {pilots.map((k) => (
                                  <span key={k} className="flex items-center gap-1"><PilotIcon kind={k} size={13} /> {pilotLabel(k)}</span>
                                ))}
                              </span>
                            </div>
                          )}
                          <div className="space-y-1">
                            <span className="text-[11px] uppercase text-muted-foreground">tags</span>
                            <div className="space-y-1 pl-3">
                              <div className="flex items-center gap-2">
                                <span className="w-12 shrink-0 text-[11px] uppercase text-muted-foreground">user</span>
                                <TagEditor tags={r.tags ?? []} onChange={(t) => app.setRoverTags(r.id, t)} />
                              </div>
                              {auto.length > 0 && (
                                <div className="flex items-center gap-2">
                                  <span className="w-12 shrink-0 text-[11px] uppercase text-muted-foreground">auto</span>
                                  <TagList tags={auto} />
                                </div>
                              )}
                            </div>
                          </div>
                        </>
                      );
                    })()}
                  </div>
                </div>
              );
            })}
            {app.rovers.length === 0 && <p className="py-2 text-sm text-muted-foreground">No rovers enrolled.</p>}
          </div>
          {app.enrollmentCodes.length > 0 && (
            <div className="space-y-1 border-t border-border pt-3">
              <p className="text-xs font-medium text-muted-foreground">Enrollment codes</p>
              {app.enrollmentCodes.map((t) => (
                <div key={t.id} className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>{t.name || "one-time"} · {t.code} · {t.remaining_uses} {t.remaining_uses === 1 ? "use" : "uses"} left · created {shortDate(t.created_at)} · expires {shortDate(t.expires_at)}</span>
                  <Button variant="ghost" size="icon-sm" onClick={() => app.revokeEnrollmentCode(t.id)}>×</Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
