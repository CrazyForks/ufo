"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useTheme } from "next-themes";
import { ArrowUpDown, Clock, GitBranch, Languages, Monitor, Moon, SquareTerminal, Sun, UserRound, Wallet, type LucideIcon, BookOpen } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { SkillCatalog } from "@/components/skill-catalog";
import { BudgetEditor } from "@/components/budget-editor";
import { FleetUsagePanel } from "@/components/usage-summary";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { LOCALES, useI18n, useT, type LocalePreference } from "@/lib/i18n";
import { useCommsOrder, useTimeFormat, type CommsOrder, type TimeFormat } from "@/lib/view";

type Palette = "classic" | "console";
type Mode = "light" | "dark" | "system";

function paletteOf(theme: string | undefined): Palette {
  return theme?.startsWith("console-") ? "console" : "classic";
}

function modeOf(theme: string | undefined): Mode {
  if (theme === "dark" || theme === "console-dark") return "dark";
  if (theme === "light" || theme === "console-light") return "light";
  return "system";
}

function themeId(palette: Palette, mode: Mode): string {
  return palette === "classic" ? mode : `console-${mode}`;
}

function worktreeValue(metadata: Record<string, unknown> | undefined): boolean | undefined {
  return typeof metadata?.worktree_enabled === "boolean" ? metadata.worktree_enabled : undefined;
}

function metadataContextValue(metadata: Record<string, unknown> | undefined) {
  return typeof metadata?.context === "string" ? metadata.context : "";
}

function SectionTitle({ icon: Icon, children }: { icon: LucideIcon; children: React.ReactNode }) {
  return (
    <CardTitle className="flex items-center gap-2 text-sm font-semibold">
      <Icon className="size-3.5" /> {children}
    </CardTitle>
  );
}

export function SettingsView() {
  const app = useApp();
  const t = useT();
  const { preference, setPreference } = useI18n();
  const { theme, setTheme } = useTheme();
  const { timeFormat, setTimeFormat } = useTimeFormat();
  const { commsOrder, setCommsOrder } = useCommsOrder();
  const fleet = app.fleets.find((f) => f.id === app.fleet);
  const fleetWorktree = worktreeValue(fleet?.metadata) ?? true;
  const savedFleetContext = metadataContextValue(fleet?.metadata);
  const [name, setName] = useState(app.user.name);
  const [savingName, setSavingName] = useState(false);
  const [fleetContext, setFleetContext] = useState(savedFleetContext);
  const [savingFleetContext, setSavingFleetContext] = useState(false);
  const palette = paletteOf(theme);
  const mode = modeOf(theme);
  const palettes: { v: Palette; icon: LucideIcon; label: string }[] = [
    { v: "classic", icon: Monitor, label: t("theme.classic") },
    { v: "console", icon: SquareTerminal, label: t("theme.console") },
  ];
  const modes: { v: Mode; icon: LucideIcon; label: string }[] = [
    { v: "light", icon: Sun, label: t("theme.mode.light") },
    { v: "dark", icon: Moon, label: t("theme.mode.dark") },
    { v: "system", icon: Monitor, label: t("theme.mode.system") },
  ];
  const timeFormats: { v: TimeFormat; label: string }[] = [
    { v: "12h", label: t("time.12h") },
    { v: "24h", label: t("time.24h") },
  ];
  const commsOrders: { v: CommsOrder; icon: LucideIcon; label: string }[] = [
    { v: "oldest_top", icon: ArrowUpDown, label: t("comms.oldestTop") },
    { v: "oldest_bottom", icon: ArrowUpDown, label: t("comms.oldestBottom") },
  ];
  const localeOptions: { v: LocalePreference; label: string }[] = [
    { v: "system", label: t("locale.system") },
    ...LOCALES.map((l) => ({ v: l.id as LocalePreference, label: t(l.labelKey) })),
  ];
  useEffect(() => setName(app.user.name), [app.user.name]);
  useEffect(() => setFleetContext(savedFleetContext), [savedFleetContext]);

  async function saveName(e: FormEvent) {
    e.preventDefault();
    setSavingName(true);
    try {
      await app.updateUserName(name);
    } finally {
      setSavingName(false);
    }
  }

  async function saveFleetContext(e: FormEvent) {
    e.preventDefault();
    setSavingFleetContext(true);
    try {
      await app.setFleetContext(fleetContext);
    } finally {
      setSavingFleetContext(false);
    }
  }

  const canEditBudget = app.myRole === "owner" || app.myRole === "admin";

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto grid max-w-5xl gap-3 p-4 pb-8 lg:grid-cols-2 lg:items-start">
        <div className="space-y-3">
          <Card>
            <CardHeader className="px-4 py-3">
              <SectionTitle icon={Monitor}>{t("settings.appearance")}</SectionTitle>
            </CardHeader>
            <CardContent className="space-y-2 px-4 pb-4 pt-0">
              <div className="flex flex-wrap gap-1.5">
                {palettes.map(({ v, icon: Icon, label }) => (
                  <Button key={v} variant={palette === v ? "default" : "outline"} size="sm" onClick={() => setTheme(themeId(v, mode))}>
                    <Icon /> {label}
                  </Button>
                ))}
                {modes.map(({ v, icon: Icon, label }) => (
                  <Button key={v} variant={mode === v ? "default" : "outline"} size="sm" onClick={() => setTheme(themeId(palette, v))}>
                    <Icon /> {label}
                  </Button>
                ))}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {timeFormats.map(({ v, label }) => (
                  <Button key={v} variant={timeFormat === v ? "default" : "outline"} size="sm" onClick={() => setTimeFormat(v)}>
                    <Clock /> {label}
                  </Button>
                ))}
                {commsOrders.map(({ v, icon: Icon, label }) => (
                  <Button key={v} variant={commsOrder === v ? "default" : "outline"} size="sm" onClick={() => setCommsOrder(v)}>
                    <Icon /> {label}
                  </Button>
                ))}
              </div>
              <div className="flex flex-wrap items-center gap-1.5 border-t border-border pt-2">
                <span className="mr-1 flex items-center gap-1 text-xs text-muted-foreground"><Languages className="size-3.5" /> {t("locale.language")}</span>
                {localeOptions.map(({ v, label }) => (
                  <Button key={v} variant={preference === v ? "default" : "outline"} size="sm" onClick={() => setPreference(v)}>
                    {label}
                  </Button>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="px-4 py-3">
              <SectionTitle icon={UserRound}>{t("settings.account")}</SectionTitle>
            </CardHeader>
            <CardContent className="space-y-2.5 px-4 pb-4 pt-0 text-sm">
              <form onSubmit={saveName} className="flex min-w-0 items-center gap-2">
                <span className="w-12 shrink-0 text-xs text-muted-foreground">{t("common.name")}</span>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("common.name")} className="h-8 min-w-0 flex-1" />
                <Button type="submit" variant="outline" size="sm" disabled={savingName || name.trim() === app.user.name}>{t("common.save")}</Button>
              </form>
              <div className="grid gap-1 text-xs sm:grid-cols-2">
                <div className="flex min-w-0 justify-between gap-2 sm:block">
                  <span className="text-muted-foreground">{t("common.email")}</span>
                  <span className="truncate font-medium sm:mt-0.5 sm:block">{app.user.email}</span>
                </div>
                <div className="flex min-w-0 justify-between gap-2 sm:block">
                  <span className="text-muted-foreground">{t("settings.fleet")}</span>
                  <span className="truncate font-medium sm:mt-0.5 sm:block">{fleet?.name}</span>
                </div>
              </div>
              <form onSubmit={saveFleetContext} className="space-y-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs text-muted-foreground">{t("settings.context")}</span>
                  <Button type="submit" variant="outline" size="sm" disabled={savingFleetContext || app.myRole !== "owner" || fleetContext.trim() === savedFleetContext}>{t("common.save")}</Button>
                </div>
                <Textarea value={fleetContext} onChange={(e) => setFleetContext(e.target.value)} placeholder={t("settings.fleetContextPlaceholder")} className="min-h-16 resize-y text-sm" disabled={app.myRole !== "owner"} />
              </form>
              <Button variant="outline" size="sm" onClick={() => app.signOut()}>{t("sidebar.signOut")}</Button>
            </CardContent>
          </Card>
        </div>

        <div className="space-y-3">
          <Card>
            <CardHeader className="px-4 py-3">
              <SectionTitle icon={Wallet}>{t("settings.spendLimits")}</SectionTitle>
            </CardHeader>
            <CardContent className="space-y-3 px-4 pb-4 pt-0 text-sm">
              <p className="text-[11px] leading-snug text-muted-foreground">{t("settings.fleetSpendHint")}</p>
              {app.fleet ? <FleetUsagePanel fleetId={app.fleet} /> : null}
              <div className="border-t border-border pt-2">
                <BudgetEditor
                  metadata={fleet?.metadata}
                  disabled={!canEditBudget}
                  onSave={(budget) => app.setFleetBudget(budget)}
                />
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="px-4 py-3">
              <SectionTitle icon={BookOpen}>{t("skills.title")}</SectionTitle>
            </CardHeader>
            <CardContent className="px-4 pb-4 pt-0">
              {app.fleet ? (
                <SkillCatalog fleetId={app.fleet} canManage={app.myRole === "owner" || app.myRole === "admin"} />
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="px-4 py-3">
              <SectionTitle icon={GitBranch}>{t("settings.worktree")}</SectionTitle>
            </CardHeader>
            <CardContent className="space-y-2 px-4 pb-4 pt-0 text-sm">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm">{t("settings.fleetDefault")}</div>
                  <div className="text-[11px] text-muted-foreground">{t("settings.fleetWorktreeHint")}</div>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button variant={fleetWorktree === true ? "default" : "outline"} size="sm" onClick={() => app.setFleetWorktree(true)}>
                    <GitBranch /> {t("common.on")}
                  </Button>
                  <Button variant={fleetWorktree === false ? "default" : "outline"} size="sm" onClick={() => app.setFleetWorktree(false)}>
                    {t("common.off")}
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
