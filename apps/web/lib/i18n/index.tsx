"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { en, type MessageKey, type Messages } from "./messages/en";
import { zhCN } from "./messages/zh-CN";
import { zhTW } from "./messages/zh-TW";
import { zhHK } from "./messages/zh-HK";

export type Locale = "en" | "zh-CN" | "zh-TW" | "zh-HK";
export type LocalePreference = Locale | "system";

export const LOCALES: { id: Locale; labelKey: MessageKey }[] = [
  { id: "en", labelKey: "locale.en" },
  { id: "zh-CN", labelKey: "locale.zh-CN" },
  { id: "zh-HK", labelKey: "locale.zh-HK" },
  { id: "zh-TW", labelKey: "locale.zh-TW" },
];

const STORAGE_KEY = "ufo.locale";
const CATALOGS: Record<Locale, Messages> = {
  en,
  "zh-CN": zhCN,
  "zh-TW": zhTW,
  "zh-HK": zhHK,
};

const VALID_LOCALES = new Set<string>(LOCALES.map((l) => l.id));

type Vars = Record<string, string | number>;

let activeMessages: Messages = en;
let activeLocale: Locale = "en";

export function translate(key: MessageKey, vars?: Vars): string {
  let s = activeMessages[key] ?? en[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll(`{${k}}`, String(v));
    }
  }
  return s;
}

export const t = translate;

export function statusLabel(status: string): string {
  const key = `status.${status}` as MessageKey;
  return key in en ? translate(key) : status;
}

export function priorityLabel(level: number): string {
  const keys = ["priority.none", "priority.low", "priority.medium", "priority.high", "priority.urgent"] as const;
  return translate(keys[level] ?? "priority.none");
}

export function sortLabel(sort: string): string {
  const map: Record<string, MessageKey> = {
    created_desc: "sort.newest",
    created_asc: "sort.oldest",
    priority: "sort.priority",
    due: "sort.due",
    title: "sort.title",
  };
  return map[sort] ? translate(map[sort]) : sort;
}

export function cardPropLabel(prop: string): string {
  const key = `cardProp.${prop}` as MessageKey;
  return key in en ? translate(key) : prop;
}

export function monthLabel(monthIndex: number): string {
  const keys = [
    "month.jan", "month.feb", "month.mar", "month.apr", "month.may", "month.jun",
    "month.jul", "month.aug", "month.sep", "month.oct", "month.nov", "month.dec",
  ] as const;
  return translate(keys[monthIndex] ?? "month.jan");
}

export function detectBrowserLocale(): Locale {
  if (typeof navigator === "undefined") return "en";
  const tags = [...(navigator.languages ?? []), navigator.language].filter(Boolean).map((l) => l.toLowerCase());
  for (const lang of tags) {
    if (lang === "zh-hk" || lang.startsWith("zh-hk-") || lang === "zh-hant-hk" || lang === "zh-mo" || lang.startsWith("zh-mo")) return "zh-HK";
    if (lang === "zh-tw" || lang.startsWith("zh-tw-") || lang === "zh-hant-tw") return "zh-TW";
    if (lang === "zh-hant" || lang.startsWith("zh-hant-")) return "zh-TW";
    if (lang === "zh-cn" || lang.startsWith("zh-cn-") || lang === "zh-hans" || lang.startsWith("zh-hans-") || lang === "zh-sg") return "zh-CN";
    if (lang === "zh" || lang.startsWith("zh-")) return "zh-CN";
    if (lang === "en" || lang.startsWith("en-")) return "en";
  }
  return "en";
}

function readStoredPreference(): LocalePreference {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === "system" || VALID_LOCALES.has(raw ?? "")) return raw as LocalePreference;
  } catch {
    /* ignore */
  }
  return "system";
}

function resolveLocale(pref: LocalePreference): Locale {
  return pref === "system" ? detectBrowserLocale() : pref;
}

function applyLocale(locale: Locale) {
  activeLocale = locale;
  activeMessages = CATALOGS[locale] ?? en;
  if (typeof document !== "undefined") {
    document.documentElement.lang = locale;
  }
}

type I18nContextValue = {
  locale: Locale;
  preference: LocalePreference;
  setPreference: (pref: LocalePreference) => void;
  t: (key: MessageKey, vars?: Vars) => string;
};

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<LocalePreference>("system");
  const [locale, setLocale] = useState<Locale>("en");

  useEffect(() => {
    const pref = readStoredPreference();
    const resolved = resolveLocale(pref);
    setPreferenceState(pref);
    setLocale(resolved);
    applyLocale(resolved);
  }, []);

  useEffect(() => {
    if (preference !== "system") return;
    const onLang = () => {
      const resolved = detectBrowserLocale();
      setLocale(resolved);
      applyLocale(resolved);
    };
    window.addEventListener("languagechange", onLang);
    return () => window.removeEventListener("languagechange", onLang);
  }, [preference]);

  const setPreference = useCallback((pref: LocalePreference) => {
    setPreferenceState(pref);
    try {
      localStorage.setItem(STORAGE_KEY, pref);
    } catch {
      /* ignore */
    }
    const resolved = resolveLocale(pref);
    setLocale(resolved);
    applyLocale(resolved);
  }, []);

  const tFn = useCallback((key: MessageKey, vars?: Vars) => {
    void locale;
    return translate(key, vars);
  }, [locale]);

  const value = useMemo(
    () => ({ locale, preference, setPreference, t: tFn }),
    [locale, preference, setPreference, tFn],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n outside I18nProvider");
  return ctx;
}

export function useT() {
  return useI18n().t;
}

export type { MessageKey, Messages };
