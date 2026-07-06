"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, GitBranch, Pencil, Plus, Wallet } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { BudgetEditor, budgetFromMetadata } from "@/components/budget-editor";
import { MissionUsagePanel } from "@/components/usage-summary";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useT } from "@/lib/i18n";
import { SECTION_ICONS } from "@/lib/section-icons";
import type { Mission } from "@/lib/types";

export function MissionsView() {
  const app = useApp();
  const t = useT();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [context, setContext] = useState("");

  const count = (missionId: string) => app.missionCounts[missionId] ?? 0;

  async function create(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || !key.trim()) return;
    if (await app.addMission(name, key, context)) { setName(""); setKey(""); setContext(""); }
  }

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-3 p-4 pb-8">
        <Card>
          <CardHeader className="px-4 py-3">
            <CardTitle className="flex items-center gap-2 text-base"><SECTION_ICONS.missions className="size-4" /> {t("missions.title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 px-4 pb-4 pt-0">
            <form className="space-y-2" onSubmit={create}>
              <div className="flex gap-2">
                <Input value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} placeholder="KEY" className="w-24 font-mono uppercase" maxLength={8} />
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("missions.namePlaceholder")} className="flex-1" />
                <Button type="submit" size="icon"><Plus /></Button>
              </div>
              <Textarea value={context} onChange={(e) => setContext(e.target.value)} placeholder={t("missions.contextPlaceholder")} className="min-h-16 resize-y text-sm" />
            </form>
            <div className="divide-y divide-border border-t border-border">
              {app.missions.map((m) => <MissionRow key={m.id} mission={m} count={count(m.id)} />)}
              {app.missions.length === 0 && <p className="py-2 text-sm text-muted-foreground">{t("missions.empty")}</p>}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function metadataContextValue(metadata: Record<string, unknown> | undefined) {
  return typeof metadata?.context === "string" ? metadata.context : "";
}

function worktreeValue(metadata: Record<string, unknown> | undefined): boolean | undefined {
  return typeof metadata?.worktree_enabled === "boolean" ? metadata.worktree_enabled : undefined;
}

function MissionRow({ mission, count }: { mission: Mission; count: number }) {
  const app = useApp();
  const t = useT();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(mission.name);
  const [key, setKey] = useState(mission.key);
  const [context, setContext] = useState(metadataContextValue(mission.metadata));
  const [saving, setSaving] = useState(false);
  const canEditBudget = app.myRole === "owner" || app.myRole === "admin";
  const worktree = worktreeValue(mission.metadata);
  const hasBudget = budgetFromMetadata(mission.metadata) != null;

  function openEditor() {
    setName(mission.name);
    setKey(mission.key);
    setContext(metadataContextValue(mission.metadata));
    setOpen(true);
  }

  async function saveIdentity(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || !key.trim()) return;
    setSaving(true);
    try {
      await app.updateMission(mission.id, name, key, context);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="py-2">
      <div className="flex items-center justify-between gap-2 text-sm">
        <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => (open ? setOpen(false) : openEditor())}>
          {open ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-xs font-medium">{mission.key}</span>
          <span className="min-w-0 truncate font-medium">{mission.name}</span>
          {hasBudget && <Wallet className="size-3 shrink-0 text-muted-foreground" aria-label={t("missions.budget")} />}
          {worktree !== undefined && <GitBranch className="size-3 shrink-0 text-muted-foreground" aria-label={t("missions.worktree")} />}
        </button>
        <span className="flex shrink-0 items-center gap-2">
          <span className="text-xs text-muted-foreground">{t("missions.operationsCount", { count })}</span>
          <Button variant="ghost" size="icon-sm" onClick={() => (open ? setOpen(false) : openEditor())} aria-label={t("common.edit")}>
            <Pencil />
          </Button>
        </span>
      </div>

      {open && (
        <div className="mt-2 space-y-3 rounded-md border border-border bg-muted/20 p-3">
          <form className="space-y-2" onSubmit={saveIdentity}>
            <div className="flex flex-wrap items-center gap-2">
              <Input value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} className="h-8 w-24 font-mono uppercase" maxLength={8} />
              <Input value={name} onChange={(e) => setName(e.target.value)} className="h-8 min-w-0 flex-1" />
              <Button type="submit" size="sm" disabled={saving}>{saving ? t("common.saving") : t("common.save")}</Button>
            </div>
            <Textarea value={context} onChange={(e) => setContext(e.target.value)} placeholder={t("missions.contextEditPlaceholder")} className="min-h-16 resize-y text-sm" />
          </form>

          <div className="space-y-1.5 border-t border-border pt-2">
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <GitBranch className="size-3.5" /> {t("missions.worktree")}
            </div>
            <div className="flex flex-wrap gap-1">
              <Button variant={worktree === undefined ? "default" : "outline"} size="sm" className="h-7 text-xs" onClick={() => app.setMissionWorktree(mission.id, null)}>{t("op.worktreeInherited")}</Button>
              <Button variant={worktree === true ? "default" : "outline"} size="sm" className="h-7 text-xs" onClick={() => app.setMissionWorktree(mission.id, true)}>{t("common.on")}</Button>
              <Button variant={worktree === false ? "default" : "outline"} size="sm" className="h-7 text-xs" onClick={() => app.setMissionWorktree(mission.id, false)}>{t("common.off")}</Button>
            </div>
            <p className="text-[11px] text-muted-foreground">{t("missions.worktreeHint")}</p>
          </div>

          <div className="space-y-1.5 border-t border-border pt-2">
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <Wallet className="size-3.5" /> {t("missions.budget")}
            </div>
            {app.fleet ? <MissionUsagePanel fleetId={app.fleet} missionId={mission.id} /> : null}
            <BudgetEditor
              key={mission.id}
              metadata={mission.metadata}
              disabled={!canEditBudget}
              onSave={(budget) => app.setMissionBudget(mission.id, budget)}
            />
          </div>
        </div>
      )}
    </div>
  );
}
