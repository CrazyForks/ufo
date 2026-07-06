"use client";

import { useEffect, useState } from "react";
import { Bot, BookOpen, Loader2, Plus, Shield, Trash2, UserRound, X } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { PilotIcon } from "@/components/pilot-icon";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { del, getJSON, putJSON, withFleet } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { pilotLabel, userLabel } from "@/lib/labels";
import { SECTION_ICONS } from "@/lib/section-icons";
import type { Crew, CrewMember, Pilot, Skill } from "@/lib/types";
import { cn } from "@/lib/utils";

export function CrewsView() {
  const app = useApp();
  const t = useT();
  const [crewName, setCrewName] = useState("");
  const canManage = app.myRole === "owner" || app.myRole === "admin";

  return (
    <div className="mx-auto grid h-full max-w-5xl gap-4 overflow-y-auto p-4 lg:grid-cols-[minmax(0,1fr)_20rem] lg:overflow-hidden">
      <Card className="flex min-h-0 flex-col">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base"><SECTION_ICONS.crews className="size-4" /> {t("crews.title")}</CardTitle>
        </CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col space-y-3">
          <p className="text-xs text-muted-foreground">{t("crews.hint")}</p>
          {canManage && (
            <form
              className="flex gap-2"
              onSubmit={(e) => { e.preventDefault(); if (crewName.trim()) { app.addCrew(crewName); setCrewName(""); } }}
            >
              <Input value={crewName} onChange={(e) => setCrewName(e.target.value)} placeholder={t("crews.newPlaceholder")} className="flex-1" />
              <Button type="submit"><Plus /> {t("crews.addCrew")}</Button>
            </form>
          )}
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
            {app.crews.length === 0 && <p className="text-sm text-muted-foreground">{canManage ? t("crews.emptyManage") : t("crews.empty")}</p>}
            {app.crews.map((c) => <CrewCard key={c.id} crew={c} canManage={canManage} />)}
          </div>
        </CardContent>
      </Card>

      <Card className="flex min-h-0 flex-col">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base"><Bot className="size-4" /> {t("crews.pilots")}</CardTitle>
        </CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col space-y-3">
          <p className="text-xs text-muted-foreground">{t("crews.pilotsHint")}</p>
          {app.pilots.length > 0 && (
            <div className="grid grid-cols-[minmax(0,1fr)_6.75rem] px-2 text-[9px] font-medium uppercase text-muted-foreground">
              <span>{t("crews.pilot")}</span>
              <span className="grid grid-cols-[1fr_1px_1fr] items-center gap-1.5 px-1.5 text-center">
                <span>{t("crews.online")}</span>
                <span aria-hidden />
                <span>{t("crews.enrolled")}</span>
              </span>
            </div>
          )}
          <ul className="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
            {app.pilots.map((p) => (
              <li key={p.kind} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm">
                <span className={cn("flex min-w-0 flex-1 items-center gap-2", p.rovers === 0 && "opacity-50")}>
                  <PilotIcon kind={p.kind} />
                  <span className="truncate">{pilotLabel(p.kind)}</span>
                </span>
                <PilotAvailability pilot={p} />
              </li>
            ))}
            {app.pilots.length === 0 && <p className="text-sm text-muted-foreground">{t("crews.noPilots")}</p>}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}

function PilotAvailability({ pilot }: { pilot: Pilot }) {
  const t = useT();
  const unavailable = pilot.rovers === 0;
  if (unavailable) {
    return (
      <span
        aria-label={t("crews.noRoversAria")}
        className="inline-flex h-5 w-[6.75rem] shrink-0 items-center justify-center rounded-full border border-destructive/20 bg-destructive/5 px-1.5 text-[10px] font-medium uppercase text-destructive/75"
      >
        {t("crews.noRovers")}
      </span>
    );
  }
  const label = t("crews.onlineEnrolled", { online: pilot.online_rovers, enrolled: pilot.rovers });
  return (
    <span
      aria-label={label}
      className="inline-grid h-5 w-[6.75rem] shrink-0 grid-cols-[1fr_1px_1fr] items-center gap-1.5 rounded-full border border-border bg-muted/30 px-1.5 text-[11px] tabular-nums"
    >
      <span className="grid min-w-0 grid-cols-[0.375rem_1fr] items-center gap-1 font-medium text-success">
        <span className="size-1.5 rounded-full bg-success" aria-hidden />
        <span className="text-right">{displayCount(pilot.online_rovers)}</span>
      </span>
      <span className="h-3 w-px bg-border" aria-hidden />
      <span className="grid min-w-0 grid-cols-[0.375rem_1fr] items-center gap-1 font-medium text-info">
        <span className="size-1.5 rounded-full bg-info" aria-hidden />
        <span className="text-right">{displayCount(pilot.rovers)}</span>
      </span>
    </span>
  );
}

function displayCount(value: number) {
  return value > 99 ? "99+" : String(value);
}

function CrewName({ id, name, canManage, onRename }: { id: string; name: string; canManage: boolean; onRename: (id: string, name: string) => void }) {
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
  if (!canManage) {
    return <span className="block h-7 w-full truncate px-1 text-sm font-medium text-foreground" title={name}>{name}</span>;
  }
  if (!editing) {
    return (
      <button type="button" className="block h-7 w-full truncate px-1 text-left text-sm font-medium text-foreground" title={name} onClick={() => setEditing(true)}>
        {name}
      </button>
    );
  }
  return (
    <Input
      autoFocus
      aria-label={t("crews.nameAria")}
      className="h-7 w-full border-transparent px-1 shadow-none"
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

function CrewCard({ crew, canManage }: { crew: Crew; canManage: boolean }) {
  const app = useApp();
  const t = useT();
  const members = crew.members ?? [];
  const sortedMembers = [...members].sort((a, b) => memberTypeRank(a) - memberTypeRank(b) || Number(b.role === "captain") - Number(a.role === "captain") || memberName(a, app, t("crews.memberFallback")).localeCompare(memberName(b, app, t("crews.memberFallback"))));
  const captain = sortedMembers.find((m) => m.role === "captain");
  const captainValue = captain ? memberValue(captain, app.user.id) : "";

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0 py-3">
        <CardTitle className="min-w-0 flex-1 text-sm"><CrewName id={crew.id} name={crew.name} canManage={canManage} onRename={app.renameCrew} /></CardTitle>
        {canManage && <Button variant="ghost" size="icon-sm" onClick={() => app.delCrew(crew.id)}><Trash2 /></Button>}
      </CardHeader>
      <CardContent className="space-y-2">
        {members.length === 0 && <p className="text-xs text-muted-foreground">{canManage ? t("crews.noMembersManage") : t("crews.noMembers")}</p>}
        {canManage && members.length > 0 && (
          <div className="flex items-center gap-2 rounded-md border border-border/70 bg-muted/20 px-2 py-2">
            <div className="flex w-24 items-center gap-1.5 text-xs font-medium uppercase text-muted-foreground">
              <Shield className="size-3.5" /> {t("crews.captain")}
            </div>
            <Select value={captainValue} onValueChange={(v) => app.addMember(crew.id, v, "captain", app.user.id)}>
              <SelectTrigger className="h-8 flex-1 text-xs"><SelectValue placeholder={t("crews.selectCaptain")} /></SelectTrigger>
              <SelectContent>
                {sortedMembers.map((m) => (
                  <SelectItem key={`${m.member_type}${m.member_id}`} value={memberValue(m, app.user.id)}>
                    <span className="flex items-center gap-2">
                      {m.member_type === "pilot" ? <PilotIcon kind={m.member_id} /> : <UserRound className="size-4 text-muted-foreground" />}
                      {memberName(m, app, t("crews.memberFallback"))}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <ul className="space-y-1">
          {sortedMembers.map((m) => <MemberRow key={`${m.member_type}${m.member_id}`} crewId={crew.id} m={m} canManage={canManage} />)}
        </ul>
        {canManage && (
          <Select value="" onValueChange={(v) => app.addMember(crew.id, v, "member", app.user.id)}>
            <SelectTrigger className="mt-1 h-8 text-xs"><SelectValue placeholder={t("crews.addPersonOrPilot")} /></SelectTrigger>
            <SelectContent>
              <SelectItem value="me">🧑 {userLabel(app.user)}</SelectItem>
              {app.members.filter((m) => m.id !== app.user.id).map((m) => (
                <SelectItem key={`u${m.id}`} value={`user:${m.id}`}>🧑 {m.name || m.email}</SelectItem>
              ))}
              {app.pilots.map((p) => (
                <SelectItem key={p.kind} value={`pilot:${p.kind}`} disabled={p.rovers === 0}>
                  <span className="flex items-center gap-2"><PilotIcon kind={p.kind} /> {pilotLabel(p.kind)}{p.rovers === 0 && t("crews.noRoverSuffix")}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
              {canManage && <CrewSkills crewId={crew.id} fleetId={app.fleet} />}
</CardContent>
    </Card>
  );
}

function memberValue(m: CrewMember, userId: string) {
  return m.member_type === "user" && m.member_id === userId ? "me" : `${m.member_type}:${m.member_id}`;
}

function memberTypeRank(m: CrewMember) {
  return m.member_type === "user" ? 0 : 1;
}

function memberName(m: CrewMember, app: ReturnType<typeof useApp>, fallback: string) {
  if (m.member_type === "pilot") return pilotLabel(m.member_id);
  if (m.member_id === app.user.id) return userLabel(app.user);
  const u = app.members.find((x) => x.id === m.member_id);
  return u?.name || u?.email || fallback;
}

function MemberRow({ crewId, m, canManage }: { crewId: string; m: CrewMember; canManage: boolean }) {
  const app = useApp();
  const t = useT();
  const isPilot = m.member_type === "pilot";
  const name = memberName(m, app, t("crews.memberFallback"));
  return (
    <li className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted/50">
      {isPilot ? <PilotIcon kind={m.member_id} /> : <UserRound className="size-4 text-muted-foreground" />}
      {isPilot ? (
        <span className="flex-1 truncate">{name}</span>
      ) : (
        <button type="button" className="min-w-0 flex-1 truncate text-left hover:underline" onClick={() => app.openUser(m.member_id)}>
          {name}
        </button>
      )}
      {m.role === "captain" && <Badge variant="secondary" className="gap-1 text-[10px]"><Shield className="size-3.5" /> {t("crews.captain")}</Badge>}
      <span className="text-[10px] uppercase text-muted-foreground">{isPilot ? t("crews.pilotType") : t("crews.person")}</span>
      {canManage && <Button variant="ghost" size="icon-sm" onClick={() => app.removeMember(crewId, m.member_type, m.member_id)}><X /></Button>}
    </li>
  );
}

function CrewSkills({ crewId, fleetId }: { crewId: string; fleetId: string }) {
  const t = useT();
  const [catalog, setCatalog] = useState<Skill[]>([]);
  const [bound, setBound] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [updateError, setUpdateError] = useState(false);
  const [busySkill, setBusySkill] = useState<string | null>(null);
  const boundIds = new Set(bound.map((skill) => skill.id));
  const available = catalog.filter((skill) => !boundIds.has(skill.id));

  useEffect(() => {
    let ignore = false;
    setLoading(true);
    setLoadError(false);
    setUpdateError(false);
    setCatalog([]);
    setBound([]);
    Promise.all([
      getJSON<Skill[]>(withFleet("/api/v1/skills", fleetId)),
      getJSON<Skill[]>(withFleet(`/api/v1/skills?crew_id=${encodeURIComponent(crewId)}`, fleetId)),
    ]).then(([fleetSkills, crewSkills]) => {
      if (ignore) return;
      if (!fleetSkills || !crewSkills) {
        setLoadError(true);
        return;
      }
      setCatalog(sortSkills(fleetSkills));
      setBound(sortSkills(crewSkills));
    }).catch(() => {
      if (!ignore) setLoadError(true);
    }).finally(() => {
      if (!ignore) setLoading(false);
    });
    return () => { ignore = true; };
  }, [crewId, fleetId]);

  async function attachSkill(skillId: string) {
    const skill = catalog.find((item) => item.id === skillId);
    if (!skill || busySkill) return;
    setBusySkill(skillId);
    setUpdateError(false);
    try {
      const res = await putJSON(`/api/v1/crews/${crewId}/skills/${skill.id}`);
      if (!res.ok) {
        setUpdateError(true);
        return;
      }
      setBound((prev) => sortSkills([...prev.filter((item) => item.id !== skill.id), skill]));
    } catch {
      setUpdateError(true);
    } finally {
      setBusySkill(null);
    }
  }

  async function detachSkill(skill: Skill) {
    if (busySkill) return;
    setBusySkill(skill.id);
    setUpdateError(false);
    try {
      const res = await del(`/api/v1/crews/${crewId}/skills/${skill.id}`);
      if (!res.ok) {
        setUpdateError(true);
        return;
      }
      setBound((prev) => prev.filter((item) => item.id !== skill.id));
    } catch {
      setUpdateError(true);
    } finally {
      setBusySkill(null);
    }
  }

  return (
    <div className="rounded-md border border-border/70 bg-muted/20 px-2 py-2">
      <p className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium uppercase text-muted-foreground">
        <BookOpen className="size-3.5" /> {t("op.skills")}
        {loading && <Loader2 className="ml-auto size-3 animate-spin" />}
      </p>
      <div className="space-y-1.5">
        {loadError && <p className="rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">{t("crews.skillsLoadError")}</p>}
        {!loadError && updateError && <p className="rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">{t("crews.skillsUpdateError")}</p>}
        {!loading && !loadError && bound.length === 0 && <p className="text-xs text-muted-foreground">{t("crews.noSkillsBound")}</p>}
        {bound.length > 0 && (
          <div className="space-y-1">
            {bound.map((skill) => (
              <div key={skill.id} className="group flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 text-xs hover:bg-accent/60">
                <BookOpen className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate font-medium">{skill.name}</span>
                <Badge variant="secondary" className="max-w-24 shrink-0 truncate font-mono text-[10px]">{skill.slug}</Badge>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="size-6 shrink-0 text-muted-foreground opacity-0 hover:text-destructive group-hover:opacity-100 group-focus-within:opacity-100"
                  title={t("op.detachSkill", { name: skill.name })}
                  aria-label={t("op.detachSkill", { name: skill.name })}
                  disabled={busySkill === skill.id}
                  onClick={() => void detachSkill(skill)}
                >
                  {busySkill === skill.id ? <Loader2 className="size-3 animate-spin" /> : <X className="size-3" />}
                </Button>
              </div>
            ))}
          </div>
        )}
        {!loadError && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="h-7 px-1.5 text-xs text-muted-foreground" disabled={loading || busySkill != null}>
                <Plus className="size-3" /> {t("op.addSkill")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              {available.length === 0 ? (
                <DropdownMenuItem disabled>{catalog.length === 0 ? t("skills.empty") : t("op.noAvailableSkills")}</DropdownMenuItem>
              ) : available.map((skill) => (
                <DropdownMenuItem key={skill.id} onClick={() => void attachSkill(skill.id)} className="gap-2">
                  <BookOpen className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{skill.name}</span>
                  <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{skill.slug}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </div>
  );
}

function sortSkills(skills: Skill[]) {
  return [...skills].sort((a, b) => a.slug.localeCompare(b.slug) || a.name.localeCompare(b.name));
}
