import type { RunUsage } from "@/lib/types";
import type { MessageKey } from "@/lib/i18n";

type T = (key: MessageKey, vars?: Record<string, string | number>) => string;

export function formatRunUsage(
  usage: RunUsage | null | undefined,
  t: T,
): string | null {
  if (!usage) return null;
  const parts: string[] = [];
  if (usage.total_tokens > 0) {
    parts.push(
      t("usage.runTokens", { count: usage.total_tokens.toLocaleString() }),
    );
    if (usage.input_tokens || usage.output_tokens) {
      parts.push(
        t("usage.runInOut", {
          in: usage.input_tokens.toLocaleString(),
          out: usage.output_tokens.toLocaleString(),
        }),
      );
    }
  }
  if (usage.duration_ms != null && usage.duration_ms > 0) {
    parts.push(
      t("usage.runDuration", {
        seconds: (usage.duration_ms / 1000).toFixed(1),
      }),
    );
  }
  if (usage.cost_micros != null) {
    parts.push(formatCostMicros(usage.cost_micros));
  }
  if (usage.model) parts.push(usage.model);
  if (parts.length === 0) return null;
  return parts.join(" · ");
}

export function formatCostMicros(micros: number): string {
  const dollars = micros / 1_000_000;
  if (Math.abs(dollars) >= 1) return `$${dollars.toFixed(2)}`;
  if (Math.abs(dollars) >= 0.01) return `$${dollars.toFixed(4)}`;
  return `$${dollars.toFixed(6)}`;
}
