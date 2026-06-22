"use client";

import { useTheme } from "next-themes";
import { ArrowDown, ArrowUp, Clock, Monitor, Moon, SquareTerminal, Sun, type LucideIcon } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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

export function SettingsView() {
  const app = useApp();
  const { theme, setTheme } = useTheme();
  const { timeFormat, setTimeFormat } = useTimeFormat();
  const { commsOrder, setCommsOrder } = useCommsOrder();
  const fleet = app.fleets.find((f) => f.id === app.fleet);
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
    { v: "oldest_top", icon: ArrowDown, label: "Oldest top" },
    { v: "oldest_bottom", icon: ArrowUp, label: "Oldest bottom" },
  ];

  return (
    <div className="mx-auto max-w-2xl space-y-4 p-4">
      <Card>
        <CardHeader><CardTitle className="text-base">Appearance</CardTitle></CardHeader>
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
        <CardHeader><CardTitle className="text-base">Account</CardTitle></CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex justify-between"><span className="text-muted-foreground">Name</span><span>{app.user.name || "—"}</span></div>
          <div className="flex justify-between"><span className="text-muted-foreground">Email</span><span>{app.user.email}</span></div>
          <div className="flex justify-between"><span className="text-muted-foreground">Fleet</span><span>{fleet?.name}</span></div>
          <div className="pt-2">
            <Button variant="outline" size="sm" onClick={() => app.signOut()}>Sign out</Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
