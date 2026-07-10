"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useT } from "@/lib/i18n";

export { Input as AuthInput } from "@/components/ui/input";
export { Button as AuthButton } from "@/components/ui/button";
export {
  MAX_AUTH_EMAIL_LEN,
  MAX_AUTH_NAME_LEN,
  MAX_AUTH_PASSWORD_LEN,
  validAuthEmail,
} from "@/lib/auth";

const AUTH_NEXT_STORAGE_KEY = "ufo.auth.next";

function safeNextPath(next: string | null | undefined): string {
  if (!next) return "/";
  if (!next.startsWith("/") || next.startsWith("//")) return "/";
  const pathOnly = next.split(/[?#]/, 2)[0] || "/";
  if (pathOnly === "/login" || pathOnly === "/signup" || pathOnly.startsWith("/api/")) return "/";
  return next;
}

export function clearAuthNextPath(): void {
  if (typeof window === "undefined") return;
  try {
    sessionStorage.removeItem(AUTH_NEXT_STORAGE_KEY);
  } catch {
  }
}

export function storeAuthNextPath(next: string): void {
  if (typeof window === "undefined") return;
  const safe = safeNextPath(next);
  try {
    if (safe === "/") sessionStorage.removeItem(AUTH_NEXT_STORAGE_KEY);
    else sessionStorage.setItem(AUTH_NEXT_STORAGE_KEY, safe);
  } catch {
  }
}

export function useCaptureAuthNext(): void {
  useEffect(() => {
    const next = new URLSearchParams(window.location.search).get("next");
    if (next) storeAuthNextPath(next);
    else clearAuthNextPath();
  }, []);
}

export function useAuthEmailPrefill(): string {
  const [email, setEmail] = useState("");
  useEffect(() => {
    const raw = new URLSearchParams(window.location.search).get("email")?.trim().toLowerCase() ?? "";
    if (raw) setEmail(raw);
  }, []);
  return email;
}

export function redirectAfterAuth(path?: string, fleetId?: string): void {
  if (path) {
    window.location.replace(path);
    return;
  }
  const next = authNextPath(true);
  if (next === "/" && fleetId) {
    window.location.replace(`/fleets/${encodeURIComponent(fleetId)}/operations`);
    return;
  }
  window.location.replace(next);
}

export function authNextPath(consume = false): string {
  if (typeof window === "undefined") return "/";
  const fromUrl = safeNextPath(new URLSearchParams(window.location.search).get("next"));
  let stored = "/";
  try {
    stored = safeNextPath(sessionStorage.getItem(AUTH_NEXT_STORAGE_KEY));
    if (consume) sessionStorage.removeItem(AUTH_NEXT_STORAGE_KEY);
  } catch {
    stored = "/";
  }
  if (fromUrl !== "/") return fromUrl;
  return stored;
}

function authPageHref(base: "/login" | "/signup", next?: string): string {
  const path = safeNextPath(next ?? (typeof window !== "undefined" ? authNextPath() : "/"));
  const params = new URLSearchParams();
  if (path !== "/") params.set("next", path);
  if (typeof window !== "undefined") {
    const email = new URLSearchParams(window.location.search).get("email")?.trim();
    if (email) params.set("email", email);
  }
  const q = params.toString();
  return q ? `${base}?${q}` : base;
}

export function loginHref(next?: string): string {
  return authPageHref("/login", next);
}

export function signupHref(next?: string): string {
  return authPageHref("/signup", next);
}

export function useRedirectIfAuthenticated(): void {
  useEffect(() => {
    let canceled = false;
    (async () => {
      try {
        const res = await fetch("/api/v1/users/me", {
          cache: "no-store",
          credentials: "same-origin",
        });
        if (canceled || !res.ok) return;
        const fleetId = await resolveActiveFleetId(readStoredFleetId());
        if (canceled) return;
        if (fleetId) setStoredFleet(fleetId);
        redirectAfterAuth(undefined, fleetId);
      } catch {
      }
    })();
    return () => {
      canceled = true;
    };
  }, []);
}

export const AUTH_SESSION_FAILED = "session_not_established";
export const AUTH_NO_FLEET = "no_fleet_after_signup";
export const AUTH_NOT_READY = "account_not_ready";

type FleetListItem = { id?: string; kind?: string };

function readStoredFleetId(): string | undefined {
  try {
    const id = localStorage.getItem("ufo.fleet") ?? "";
    return id || undefined;
  } catch {
    return undefined;
  }
}

async function resolveActiveFleetId(preferred?: string): Promise<string | undefined> {
  const fleetsRes = await fetch("/api/v1/fleets", {
    cache: "no-store",
    credentials: "same-origin",
  });
  if (!fleetsRes.ok) return undefined;
  const fleets = (await fleetsRes.json().catch(() => null)) as FleetListItem[] | null;
  if (!Array.isArray(fleets) || fleets.length === 0) return undefined;
  if (preferred) {
    const hit = fleets.find((f) => f.id === preferred);
    if (hit?.id) return hit.id;
  }
  const personal = fleets.find((f) => f.kind === "personal" && typeof f.id === "string" && f.id);
  if (personal?.id) return personal.id;
  const first = fleets.find((f) => typeof f.id === "string" && f.id);
  return first?.id;
}

async function fleetHasMissions(fleetId: string): Promise<boolean> {
  const res = await fetch(`/api/v1/missions?fleet_id=${encodeURIComponent(fleetId)}`, {
    cache: "no-store",
    credentials: "same-origin",
  });
  if (!res.ok) return false;
  const missions = (await res.json().catch(() => null)) as unknown;
  return Array.isArray(missions) && missions.length > 0;
}

export async function postAuth(
  path: "/api/v1/auth/signup" | "/api/v1/auth/login",
  body: Record<string, string>,
): Promise<{ ok: true; fleetId: string } | { ok: false; error: string; status?: number }> {
  try {
    const res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      cache: "no-store",
      body: JSON.stringify(body),
    });
    const data = (await res.json().catch(() => ({}))) as { error?: string; fleet_id?: string };
    if (!res.ok) {
      return { ok: false, error: data.error || "", status: res.status };
    }
    const me = await fetch("/api/v1/users/me", {
      cache: "no-store",
      credentials: "same-origin",
    });
    if (!me.ok) {
      return { ok: false, error: AUTH_SESSION_FAILED, status: me.status || 0 };
    }
    const fromHub = typeof data.fleet_id === "string" && data.fleet_id ? data.fleet_id : undefined;
    const preferred = path === "/api/v1/auth/signup" ? fromHub : (fromHub ?? readStoredFleetId());
    let fleetId = await resolveActiveFleetId(preferred);
    if (!fleetId && path === "/api/v1/auth/signup" && fromHub) {
      if (!(await fleetHasMissions(fromHub))) {
        return { ok: false, error: AUTH_NOT_READY, status: 0 };
      }
      return { ok: true, fleetId: fromHub };
    }
    if (!fleetId) {
      return { ok: false, error: AUTH_NO_FLEET, status: 0 };
    }
    if (path === "/api/v1/auth/signup" && !(await fleetHasMissions(fleetId))) {
      return { ok: false, error: AUTH_NOT_READY, status: 0 };
    }
    return { ok: true, fleetId };
  } catch {
    return { ok: false, error: "", status: 0 };
  }
}

export function authErrorMessage(
  result: { ok: false; error: string; status?: number },
  fallback: string,
  rateLimited: string,
  network: string,
  sessionFailed?: string,
  noFleet?: string,
  notReady?: string,
): string {
  if (result.error === AUTH_SESSION_FAILED) return sessionFailed || fallback;
  if (result.error === AUTH_NO_FLEET) return noFleet || fallback;
  if (result.error === AUTH_NOT_READY) return notReady || fallback;
  if (result.error) return result.error;
  if (result.status === 429) return rateLimited;
  if (result.status === 0) return network;
  return fallback;
}

export function setStoredFleet(fleetId: string | undefined): void {
  try {
    if (fleetId) localStorage.setItem("ufo.fleet", fleetId);
    else localStorage.removeItem("ufo.fleet");
  } catch {
  }
}

export function clearStoredFleet(): void {
  setStoredFleet(undefined);
}

export function authSwitchHref(
  base: "/login" | "/signup",
  email?: string,
  next?: string,
): string {
  const path = safeNextPath(next ?? (typeof window !== "undefined" ? authNextPath() : "/"));
  const params = new URLSearchParams();
  if (path !== "/") params.set("next", path);
  const trimmed = (email ?? "").trim();
  if (trimmed) params.set("email", trimmed);
  else if (typeof window !== "undefined") {
    const fromUrl = new URLSearchParams(window.location.search).get("email")?.trim();
    if (fromUrl) params.set("email", fromUrl);
  }
  const q = params.toString();
  return q ? `${base}?${q}` : base;
}

export async function clearAuthSession(): Promise<void> {
  try {
    await fetch("/api/v1/auth/logout", { method: "POST", credentials: "same-origin", cache: "no-store" });
  } catch {
  }
  clearAuthNextPath();
}

export function AuthCard({
  title,
  error,
  footer,
  children,
}: {
  title: string;
  error: string | null;
  footer: { text: string; href: string; label: string };
  children: ReactNode;
}) {
  const t = useT();
  return (
    <main className="flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-1 flex items-center gap-2">
            <div className="flex size-7 items-center justify-center rounded-md bg-brand text-sm font-bold text-brand-foreground">U</div>
            <span className="text-xs font-medium text-muted-foreground">{t("product.tagline")}</span>
          </div>
          <CardTitle className="text-xl">{title}</CardTitle>
        </CardHeader>
        <CardContent>
          {error && (
            <div className="mb-3 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          {children}
          <p className="mt-4 text-sm text-muted-foreground">
            {footer.text}{" "}
            <a href={footer.href} className="font-medium text-brand hover:underline">
              {footer.label}
            </a>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
