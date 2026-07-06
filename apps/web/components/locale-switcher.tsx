"use client";

import { Globe } from "lucide-react";
import { useI18n, useT, type Locale, type LocalePreference } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

const LOCALE_MARK: Record<Locale, string> = {
  en: "EN",
  "zh-CN": "简",
  "zh-TW": "繁",
  "zh-HK": "港",
};

const OPTIONS: { pref: LocalePreference; mark: string; labelKey: "locale.system" | "locale.en" | "locale.zh-CN" | "locale.zh-HK" | "locale.zh-TW" }[] = [
  { pref: "system", mark: "SYS", labelKey: "locale.system" },
  { pref: "en", mark: LOCALE_MARK.en, labelKey: "locale.en" },
  { pref: "zh-CN", mark: LOCALE_MARK["zh-CN"], labelKey: "locale.zh-CN" },
  { pref: "zh-HK", mark: LOCALE_MARK["zh-HK"], labelKey: "locale.zh-HK" },
  { pref: "zh-TW", mark: LOCALE_MARK["zh-TW"], labelKey: "locale.zh-TW" },
];

export function LocaleSwitcher({ className }: { className?: string }) {
  const t = useT();
  const { locale, preference, setPreference } = useI18n();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className={cn("font-semibold tracking-wide", className)}
          title={t("locale.language")}
          aria-label={t("locale.language")}
        >
          {preference === "system" ? (
            <Globe />
          ) : (
            <span className="text-[10px] leading-none text-foreground" aria-hidden>
              {LOCALE_MARK[locale]}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[11rem]">
        {OPTIONS.map(({ pref, mark, labelKey }) => (
          <DropdownMenuItem
            key={pref}
            onClick={() => setPreference(pref)}
            className={cn(preference === pref && "bg-accent font-medium")}
          >
            <span className="flex w-6 shrink-0 items-center justify-center text-muted-foreground" aria-hidden>
              {pref === "system" ? (
                <Globe className="size-3.5" />
              ) : (
                <span className="text-[10px] font-semibold tracking-wide">{mark}</span>
              )}
            </span>
            <span>{t(labelKey)}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
