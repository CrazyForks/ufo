"use client";

import { useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { SpendBudgetInput } from "@/components/app-provider";
import { useT } from "@/lib/i18n";

export function budgetFromMetadata(metadata: Record<string, unknown> | undefined): SpendBudgetInput {
  const raw = metadata?.budget;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const b = raw as Record<string, unknown>;
  const period = b.period === "calendar_month" ? "calendar_month" : "calendar_week";
  const max_runs = typeof b.max_runs === "number" && b.max_runs > 0 ? b.max_runs : null;
  const max_tokens = typeof b.max_tokens === "number" && b.max_tokens > 0 ? b.max_tokens : null;
  const max_usd_micros = typeof b.max_usd_micros === "number" && b.max_usd_micros > 0 ? b.max_usd_micros : null;
  if (max_runs == null && max_tokens == null && max_usd_micros == null) return null;
  return { period, max_runs, max_tokens, max_usd_micros };
}

function usdFromMicros(value: number | null | undefined): string {
  if (value == null || value <= 0) return "";
  return String(value / 1_000_000).replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "");
}

export function BudgetEditor({
  metadata,
  disabled,
  onSave,
}: {
  metadata: Record<string, unknown> | undefined;
  disabled?: boolean;
  onSave: (budget: SpendBudgetInput) => Promise<boolean>;
}) {
  const t = useT();
  const saved = budgetFromMetadata(metadata);
  const [enabled, setEnabled] = useState(saved != null);
  const [period, setPeriod] = useState<"calendar_week" | "calendar_month">(saved?.period ?? "calendar_week");
  const [maxRuns, setMaxRuns] = useState(saved?.max_runs != null ? String(saved.max_runs) : "");
  const [maxTokens, setMaxTokens] = useState(saved?.max_tokens != null ? String(saved.max_tokens) : "");
  const [maxUsd, setMaxUsd] = useState(usdFromMicros(saved?.max_usd_micros));
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const next = budgetFromMetadata(metadata);
    setEnabled(next != null);
    setPeriod(next?.period ?? "calendar_week");
    setMaxRuns(next?.max_runs != null ? String(next.max_runs) : "");
    setMaxTokens(next?.max_tokens != null ? String(next.max_tokens) : "");
    setMaxUsd(usdFromMicros(next?.max_usd_micros));
  }, [metadata]);

  async function save(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      if (!enabled) {
        await onSave(null);
        return;
      }
      const runs = maxRuns.trim() === "" ? null : Number(maxRuns);
      const tokens = maxTokens.trim() === "" ? null : Number(maxTokens);
      const usd = maxUsd.trim() === "" ? null : Number(maxUsd);
      await onSave({
        period,
        max_runs: runs != null && Number.isFinite(runs) && runs > 0 ? runs : null,
        max_tokens: tokens != null && Number.isFinite(tokens) && tokens > 0 ? tokens : null,
        max_usd_micros: usd != null && Number.isFinite(usd) && usd > 0 ? Math.round(usd * 1_000_000) : null,
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={save} className="space-y-2 rounded-md border border-border p-3 text-sm">
      <label className="flex items-center justify-between gap-3 text-xs">
        <span>
          <span className="font-medium text-foreground">{t("op.budget")}</span>
          <span className="block text-muted-foreground">{t("budget.optionalCap")}</span>
        </span>
        <input
          type="checkbox"
          className="peer sr-only"
          checked={enabled}
          disabled={disabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-disabled:opacity-50" />
      </label>
      {enabled && (
        <div className="grid gap-2 sm:grid-cols-4">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{t("budget.period")}</Label>
            <Select value={period} onValueChange={(v) => setPeriod(v as "calendar_week" | "calendar_month")} disabled={disabled}>
              <SelectTrigger className="h-8"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="calendar_week">{t("budget.periodWeek")}</SelectItem>
                <SelectItem value="calendar_month">{t("budget.periodMonth")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{t("budget.maxRuns")}</Label>
            <Input className="h-8" type="number" min={1} placeholder={t("budget.unlimited")} value={maxRuns} disabled={disabled} onChange={(e) => setMaxRuns(e.target.value)} />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{t("budget.maxTokens")}</Label>
            <Input className="h-8" type="number" min={1} placeholder={t("budget.unlimited")} value={maxTokens} disabled={disabled} onChange={(e) => setMaxTokens(e.target.value)} />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{t("budget.maxUsd")}</Label>
            <Input className="h-8" type="number" min={0} step="0.01" placeholder={t("budget.unlimited")} value={maxUsd} disabled={disabled} onChange={(e) => setMaxUsd(e.target.value)} />
          </div>
        </div>
      )}
      <div className="flex justify-end">
        <Button type="submit" variant="outline" size="sm" disabled={disabled || saving}>
          {saving ? t("common.saving") : t("budget.save")}
        </Button>
      </div>
    </form>
  );
}
