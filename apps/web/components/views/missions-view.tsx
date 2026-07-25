"use client";

import { useState } from "react";
import { BookOpen, ChevronDown, ChevronRight, GitBranch, GitPullRequest, Pencil, Plus, Trash2, Wallet } from "lucide-react";
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
      <div className="mx-auto max-w-6xl space-y-3 p-4 pb-8">
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

type MissionLearningEntry = {
  operation_id: string;
  operation_code: string;
  operation_title: string;
  summary: string;
  artifacts?: { kind: "doc" | "skill"; path: string; summary?: string }[];
  captured_at: string;
};

function missionLearningEntries(metadata: Record<string, unknown> | undefined): MissionLearningEntry[] {
  const learning = metadata?.learning;
  if (!learning || typeof learning !== "object" || Array.isArray(learning)) return [];
  const entries = (learning as Record<string, unknown>).entries;
  if (!entries || typeof entries !== "object" || Array.isArray(entries)) return [];
  return Object.values(entries)
    .filter((entry): entry is MissionLearningEntry => (
      !!entry && typeof entry === "object" && !Array.isArray(entry) &&
      typeof (entry as Record<string, unknown>).operation_id === "string" &&
      typeof (entry as Record<string, unknown>).summary === "string"
    ))
    .sort((a, b) => b.captured_at.localeCompare(a.captured_at));
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
  const canEditForge = canEditBudget;
  const worktree = worktreeValue(mission.metadata);
  const hasBudget = budgetFromMetadata(mission.metadata) != null;
  const forgeIds = mission.forge_ids ?? [];
  const boundForges = app.forges.filter((f) => forgeIds.includes(f.id));
  const learningEntries = missionLearningEntries(mission.metadata);

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
          {boundForges.length > 0 && (
            <GitPullRequest
              className="size-3 shrink-0 text-muted-foreground"
              aria-label={boundForges.map((f) => f.key).join(", ")}
            />
          )}
          {learningEntries.length > 0 && <BookOpen className="size-3 shrink-0 text-muted-foreground" aria-label={t("missions.learning")} />}
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

          {learningEntries.length > 0 && (
            <div className="space-y-2 border-t border-border pt-2">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                  <BookOpen className="size-3.5" /> {t("missions.learning")}
                </div>
                {canEditBudget && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-6 px-1.5 text-[11px] text-muted-foreground hover:text-destructive"
                    onClick={() => void app.deleteMissionLearning(mission.id)}
                  >
                    {t("missions.learningClear")}
                  </Button>
                )}
              </div>
              <p className="text-[11px] text-muted-foreground">{t("missions.learningHint")}</p>
              <div className="divide-y divide-border/60">
                {learningEntries.map((entry) => (
                  <div key={entry.operation_id} className="space-y-1 py-2 text-xs">
                    <div className="flex min-w-0 items-center gap-1.5">
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{entry.operation_code}</span>
                      <span className="min-w-0 flex-1 truncate font-medium">{entry.operation_title}</span>
                      {canEditBudget && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          className="size-6 shrink-0 text-muted-foreground hover:text-destructive"
                          aria-label={t("missions.learningDelete")}
                          title={t("missions.learningDelete")}
                          onClick={() => void app.deleteMissionLearning(mission.id, entry.operation_id)}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      )}
                    </div>
                    <p className="whitespace-pre-wrap leading-relaxed">{entry.summary}</p>
                    {entry.artifacts?.map((artifact) => (
                      <div key={`${artifact.kind}:${artifact.path}`} className="flex min-w-0 items-baseline gap-1.5 text-[11px] text-muted-foreground">
                        <span className="shrink-0">{t(artifact.kind === "skill" ? "missions.learningSkill" : "missions.learningDoc")}</span>
                        <span className="truncate font-mono" title={artifact.path}>{artifact.path}</span>
                        {artifact.summary && <span className="min-w-0 truncate">· {artifact.summary}</span>}
                      </div>
                    ))}
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="space-y-1.5 border-t border-border pt-2">
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <GitPullRequest className="size-3.5" /> {t("missions.forge")}
            </div>
            {app.forges.length === 0 ? (
              <p className="text-[11px] text-muted-foreground">{t("missions.forgeCatalogEmpty")}</p>
            ) : (
              <div className="space-y-1">
                {app.forges.map((f) => {
                  const checked = forgeIds.includes(f.id);
                  return (
                    <label key={f.id} className="flex items-center gap-2 text-xs">
                      <input
                        type="checkbox"
                        className="size-3.5 accent-[var(--brand)]"
                        checked={checked}
                        disabled={!canEditForge}
                        onChange={(e) => {
                          const next = e.target.checked
                            ? [...forgeIds, f.id]
                            : forgeIds.filter((id) => id !== f.id);
                          void app.setMissionForges(mission.id, next);
                        }}
                      />
                      <span className="min-w-0 truncate">
                        <span className="font-mono font-medium">{f.key}</span>
                        <span className="text-muted-foreground"> · {f.repo}</span>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
            <p className="text-[11px] text-muted-foreground">{t("missions.forgeHint")}</p>
          </div>

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
