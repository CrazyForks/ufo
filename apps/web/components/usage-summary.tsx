"use client";

import { useEffect, useState } from "react";
import { getJSON } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { formatCostMicros } from "@/lib/usage";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { UsageSummary } from "@/lib/types";

export function UsagePeriodToggle({
  period,
  onChange,
}: {
  period: "calendar_week" | "calendar_month";
  onChange: (p: "calendar_week" | "calendar_month") => void;
}) {
  const t = useT();
  return (
    <div className="flex gap-1">
      <Button type="button" size="sm" variant={period === "calendar_week" ? "default" : "outline"} className="h-7 px-2 text-xs" onClick={() => onChange("calendar_week")}>
        {t("budget.periodWeek")}
      </Button>
      <Button type="button" size="sm" variant={period === "calendar_month" ? "default" : "outline"} className="h-7 px-2 text-xs" onClick={() => onChange("calendar_month")}>
        {t("budget.periodMonth")}
      </Button>
    </div>
  );
}

function capLine(used: number, max: number | null | undefined, unit: string): string {
  if (max != null && max > 0) return `${used.toLocaleString()} / ${max.toLocaleString()} ${unit}`;
  return `${used.toLocaleString()} ${unit}`;
}

export function UsageTotalsView({
  runs,
  total_tokens,
  cost_micros,
  max_runs,
  max_tokens,
  max_usd_micros,
  compact,
}: {
  runs: number;
  total_tokens: number;
  cost_micros: number;
  max_runs?: number | null;
  max_tokens?: number | null;
  max_usd_micros?: number | null;
  compact?: boolean;
}) {
  const t = useT();
  const rows = [
    { label: t("usage.runs"), text: capLine(runs, max_runs, t("usage.runsUnit")), over: max_runs != null && runs >= max_runs },
    { label: t("usage.tokens"), text: capLine(total_tokens, max_tokens, t("usage.tokensUnit")), over: max_tokens != null && total_tokens >= max_tokens },
  ];
  if (cost_micros > 0 || (max_usd_micros != null && max_usd_micros > 0)) {
    const costLabel = max_usd_micros != null && max_usd_micros > 0
      ? `${formatCostMicros(cost_micros)} / ${formatCostMicros(max_usd_micros)}`
      : formatCostMicros(cost_micros);
    rows.push({
      label: t("usage.cost"),
      text: costLabel,
      over: max_usd_micros != null && cost_micros >= max_usd_micros,
    });
  }
  return (
    <div className={cn("grid gap-1", compact ? "text-[11px]" : "text-xs", "sm:grid-cols-3")}>
      {rows.map((row) => (
        <div key={row.label} className="min-w-0 rounded-md border border-border/80 bg-background/60 px-2 py-1.5">
          <div className="text-muted-foreground">{row.label}</div>
          <div className={cn("truncate font-medium tabular-nums", row.over && "text-destructive")}>{row.text}</div>
        </div>
      ))}
    </div>
  );
}

export function useFleetUsage(fleetId: string | undefined, period: "calendar_week" | "calendar_month") {
  const [data, setData] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (!fleetId) {
      setData(null);
      return;
    }
    let canceled = false;
    setLoading(true);
    getJSON<UsageSummary>(`/api/v1/usage?fleet_id=${encodeURIComponent(fleetId)}&period=${period}`)
      .then((res) => {
        if (!canceled) setData(res);
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => { canceled = true; };
  }, [fleetId, period]);
  return { data, loading };
}

export function FleetUsagePanel({ fleetId }: { fleetId: string }) {
  const t = useT();
  const [period, setPeriod] = useState<"calendar_week" | "calendar_month">("calendar_week");
  const { data, loading } = useFleetUsage(fleetId, period);
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs font-medium text-muted-foreground">{t("usage.periodUsage")}</div>
        <UsagePeriodToggle period={period} onChange={setPeriod} />
      </div>
      {loading && !data ? (
        <p className="text-xs text-muted-foreground">{t("common.loading")}</p>
      ) : data ? (
        <div className="space-y-1.5">
          <div className="text-[11px] text-muted-foreground">{t("usage.periodKey", { key: data.period_key })}</div>
          <UsageTotalsView {...data.fleet} />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t("usage.unavailable")}</p>
      )}
    </div>
  );
}

export function MissionUsagePanel({
  fleetId,
  missionId,
}: {
  fleetId: string;
  missionId: string;
}) {
  const t = useT();
  const [period, setPeriod] = useState<"calendar_week" | "calendar_month">("calendar_week");
  const { data, loading } = useFleetUsage(fleetId, period);
  const mission = data?.missions.find((m) => m.id === missionId);
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs font-medium text-muted-foreground">{t("usage.periodUsage")}</div>
        <UsagePeriodToggle period={period} onChange={setPeriod} />
      </div>
      {loading && !data ? (
        <p className="text-xs text-muted-foreground">{t("common.loading")}</p>
      ) : mission ? (
        <div className="space-y-1.5">
          <div className="text-[11px] text-muted-foreground">{t("usage.periodKey", { key: data!.period_key })}</div>
          <UsageTotalsView {...mission} compact />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t("usage.none")}</p>
      )}
    </div>
  );
}
