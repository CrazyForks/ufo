"use client";

import { useEffect, useState } from "react";
import { getJSON } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { formatCostMicros } from "@/lib/usage";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  SPEND_BUDGET_PERIODS,
  SPEND_BUDGET_PERIOD_LABEL,
  type DayUsage, type OperationUsage, type PilotUsage, type RoverUsage, type SpendBudgetPeriod, type UsageSummary,
} from "@/lib/types";

export function UsagePeriodToggle({
  period,
  onChange,
}: {
  period: SpendBudgetPeriod;
  onChange: (p: SpendBudgetPeriod) => void;
}) {
  const t = useT();
  return (
    <div className="flex gap-1">
      {SPEND_BUDGET_PERIODS.map((p) => (
        <Button key={p} type="button" size="sm" variant={period === p ? "default" : "outline"} className="h-7 px-2 text-xs" onClick={() => onChange(p)}>
          {t(SPEND_BUDGET_PERIOD_LABEL[p])}
        </Button>
      ))}
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

export function useFleetUsage(
  fleetId: string | undefined,
  period: SpendBudgetPeriod,
  scope?: { missionId?: string; operationId?: string },
) {
  const [data, setData] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const missionId = scope?.missionId;
  const operationId = scope?.operationId;
  useEffect(() => {
    if (!fleetId) {
      setData(null);
      return;
    }
    let canceled = false;
    setLoading(true);
    const qs = new URLSearchParams({ fleet_id: fleetId, period });
    if (missionId) qs.set("mission_id", missionId);
    if (operationId) qs.set("operation_id", operationId);
    getJSON<UsageSummary>(`/api/v1/usage?${qs}`)
      .then((res) => {
        if (!canceled) setData(res);
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => { canceled = true; };
  }, [fleetId, period, missionId, operationId]);
  return { data, loading };
}

function BreakdownTable({
  label,
  rows,
}: {
  label: string;
  rows: { key: string; name: string; runs: number; total_tokens: number; cost_micros: number }[];
}) {
  const t = useT();
  if (rows.length === 0) return null;
  return (
    <div className="space-y-1">
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="overflow-x-auto rounded-md border border-border/80">
        <table className="w-full text-[11px]">
          <thead className="text-muted-foreground">
            <tr className="border-b border-border/70">
              <th className="px-2 py-1 text-left font-medium">{label}</th>
              <th className="px-2 py-1 text-right font-medium">{t("usage.runs")}</th>
              <th className="px-2 py-1 text-right font-medium">{t("usage.tokens")}</th>
              <th className="px-2 py-1 text-right font-medium">{t("usage.cost")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key} className="border-b border-border/50 last:border-0">
                <td className="max-w-[10rem] truncate px-2 py-1" title={row.name}>{row.name}</td>
                <td className="px-2 py-1 text-right tabular-nums">{row.runs.toLocaleString()}</td>
                <td className="px-2 py-1 text-right tabular-nums">{row.total_tokens.toLocaleString()}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatCostMicros(row.cost_micros)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function pilotRows(items: PilotUsage[] | undefined) {
  return (items ?? []).map((p) => ({
    key: p.pilot || "-",
    name: p.pilot || "-",
    runs: p.runs,
    total_tokens: p.total_tokens,
    cost_micros: p.cost_micros,
  }));
}

function roverRows(items: RoverUsage[] | undefined, unlabeled: string) {
  return (items ?? []).map((r) => ({
    key: r.id || r.name || unlabeled,
    name: r.name || unlabeled,
    runs: r.runs,
    total_tokens: r.total_tokens,
    cost_micros: r.cost_micros,
  }));
}

function operationRows(items: OperationUsage[] | undefined) {
  return (items ?? []).map((o) => ({
    key: o.id,
    name: o.code ? `${o.code} ${o.title}` : o.title,
    runs: o.runs,
    total_tokens: o.total_tokens,
    cost_micros: o.cost_micros,
  }));
}

function dayRows(items: DayUsage[] | undefined) {
  return (items ?? []).filter((d) => d.runs > 0 || d.total_tokens > 0 || d.cost_micros > 0).map((d) => ({
    key: d.day,
    name: d.day,
    runs: d.runs,
    total_tokens: d.total_tokens,
    cost_micros: d.cost_micros,
  }));
}

export function FleetUsagePanel({ fleetId }: { fleetId: string }) {
  const t = useT();
  const [period, setPeriod] = useState<SpendBudgetPeriod>("calendar_week");
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
        <div className="space-y-2">
          <div className="text-[11px] text-muted-foreground">{t("usage.periodKey", { key: data.period_key })}</div>
          <UsageTotalsView {...data.fleet} />
          <BreakdownTable label={t("usage.pilots")} rows={pilotRows(data.pilots)} />
          <BreakdownTable label={t("usage.rovers")} rows={roverRows(data.rovers, t("usage.unknownRover"))} />
          <BreakdownTable label={t("usage.days")} rows={dayRows(data.days)} />
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
  const [period, setPeriod] = useState<SpendBudgetPeriod>("calendar_week");
  const { data, loading } = useFleetUsage(fleetId, period, { missionId });
  const mission = data?.missions.find((m) => m.id === missionId) ?? data?.missions[0];
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs font-medium text-muted-foreground">{t("usage.periodUsage")}</div>
        <UsagePeriodToggle period={period} onChange={setPeriod} />
      </div>
      {loading && !data ? (
        <p className="text-xs text-muted-foreground">{t("common.loading")}</p>
      ) : mission ? (
        <div className="space-y-2">
          <div className="text-[11px] text-muted-foreground">{t("usage.periodKey", { key: data!.period_key })}</div>
          <UsageTotalsView {...mission} compact />
          <BreakdownTable label={t("usage.operations")} rows={operationRows(data?.operations.filter((o) => o.mission_id === missionId))} />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t("usage.none")}</p>
      )}
    </div>
  );
}

export function OperationUsagePanel({
  fleetId,
  operationId,
}: {
  fleetId: string;
  operationId: string;
}) {
  const t = useT();
  const [period, setPeriod] = useState<SpendBudgetPeriod>("calendar_week");
  const { data, loading } = useFleetUsage(fleetId, period, { operationId });
  const operation = data?.operations.find((o) => o.id === operationId) ?? data?.operations[0];
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs font-medium text-muted-foreground">{t("usage.periodUsage")}</div>
        <UsagePeriodToggle period={period} onChange={setPeriod} />
      </div>
      {loading && !data ? (
        <p className="text-xs text-muted-foreground">{t("common.loading")}</p>
      ) : operation ? (
        <div className="space-y-1.5">
          <div className="text-[11px] text-muted-foreground">{t("usage.periodKey", { key: data!.period_key })}</div>
          <UsageTotalsView {...operation} compact />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t("usage.none")}</p>
      )}
    </div>
  );
}
