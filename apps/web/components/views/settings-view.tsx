"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useTheme } from "next-themes";
import { ArrowUpDown, Clock, GitBranch, Monitor, Moon, SquareTerminal, Sun, UserRound, type LucideIcon } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
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

export function SettingsView() {
  const app = useApp();
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
    { v: "classic", icon: Monitor, label: "Classic" },
    { v: "console", icon: SquareTerminal, label: "Console" },
  ];
  const modes: { v: Mode; icon: LucideIcon; label: string }[] = [
    { v: "light", icon: Sun, label: "Light" },
    { v: "dark", icon: Moon, label: "Dark" },
    { v: "system", icon: Monitor, label: "System" },
  ];
  const timeFormats: { v: TimeFormat; label: string }[] = [
    { v: "12h", label: "12-hour" },
    { v: "24h", label: "24-hour" },
  ];
  const commsOrders: { v: CommsOrder; icon: LucideIcon; label: string }[] = [
    { v: "oldest_top", icon: ArrowUpDown, label: "Oldest top" },
    { v: "oldest_bottom", icon: ArrowUpDown, label: "Oldest bottom" },
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

  return (
    <div className="mx-auto max-w-2xl space-y-4 p-4">
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><Monitor className="size-4" /> Appearance</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            {palettes.map(({ v, icon: Icon, label }) => (
              <Button key={v} variant={palette === v ? "default" : "outline"} size="sm" onClick={() => setTheme(themeId(v, mode))}>
                <Icon /> {label}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            {modes.map(({ v, icon: Icon, label }) => (
              <Button key={v} variant={mode === v ? "default" : "outline"} size="sm" onClick={() => setTheme(themeId(palette, v))}>
                <Icon /> {label}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            {timeFormats.map(({ v, label }) => (
              <Button key={v} variant={timeFormat === v ? "default" : "outline"} size="sm" onClick={() => setTimeFormat(v)}>
                <Clock /> {label}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            {commsOrders.map(({ v, icon: Icon, label }) => (
              <Button key={v} variant={commsOrder === v ? "default" : "outline"} size="sm" onClick={() => setCommsOrder(v)}>
                <Icon /> {label}
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><UserRound className="size-4" /> Account</CardTitle></CardHeader>
        <CardContent className="space-y-3 text-sm">
          <form onSubmit={saveName} className="flex items-center gap-2">
            <span className="w-14 shrink-0 text-muted-foreground">Name</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name" className="h-8" />
            <Button type="submit" variant="outline" size="sm" disabled={savingName || name.trim() === app.user.name}>Save</Button>
          </form>
          <div className="flex justify-between"><span className="text-muted-foreground">Email</span><span>{app.user.email}</span></div>
          <div className="flex justify-between"><span className="text-muted-foreground">Fleet</span><span>{fleet?.name}</span></div>
          <form onSubmit={saveFleetContext} className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">Context</span>
              <Button type="submit" variant="outline" size="sm" disabled={savingFleetContext || app.myRole !== "owner" || fleetContext.trim() === savedFleetContext}>Save</Button>
            </div>
            <Textarea value={fleetContext} onChange={(e) => setFleetContext(e.target.value)} placeholder="Root repo, default branch, constraints..." className="min-h-20 resize-y" disabled={app.myRole !== "owner"} />
          </form>
          <div className="pt-2">
            <Button variant="outline" size="sm" onClick={() => app.signOut()}>Sign out</Button>
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><GitBranch className="size-4" /> Worktrees</CardTitle></CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0">
              <div>Fleet default</div>
              <div className="text-xs text-muted-foreground">Missions inherit this; operations inherit their mission unless overridden.</div>
            </div>
            <div className="flex shrink-0 gap-1">
              <Button variant={fleetWorktree === true ? "default" : "outline"} size="sm" onClick={() => app.setFleetWorktree(true)}>
                <GitBranch /> On
              </Button>
              <Button variant={fleetWorktree === false ? "default" : "outline"} size="sm" onClick={() => app.setFleetWorktree(false)}>
                Off
              </Button>
            </div>
          </div>
          <div className="space-y-2">
            {app.missions.map((mission) => {
              const value = worktreeValue(mission.metadata);
              return (
                <div key={mission.id} className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-xs font-medium">{mission.key}</span>
                    <span className="min-w-0 truncate">{mission.name}</span>
                  </span>
                  <div className="flex shrink-0 gap-1">
                    <Button variant={value === undefined ? "default" : "outline"} size="sm" onClick={() => app.setMissionWorktree(mission.id, null)}>Inherit</Button>
                    <Button variant={value === true ? "default" : "outline"} size="sm" onClick={() => app.setMissionWorktree(mission.id, true)}>On</Button>
                    <Button variant={value === false ? "default" : "outline"} size="sm" onClick={() => app.setMissionWorktree(mission.id, false)}>Off</Button>
                  </div>
                </div>
              );
            })}
            {app.missions.length === 0 && <p className="text-sm text-muted-foreground">No missions yet.</p>}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
