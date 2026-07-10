"use client";

import { useEffect, useState } from "react";
import { AppProvider } from "@/components/app-provider";
import { AppShell } from "@/components/app-shell";
import { apiFetch, getJSON } from "@/lib/api";
import { parseAppPath } from "@/lib/routes";
import type { Fleet, User } from "@/lib/types";
import { clearAuthSession, clearStoredFleet, loginHref, storeAuthNextPath } from "../auth-ui";
import { t, useT } from "@/lib/i18n";

type Boot =
  | { status: "loading" }
  | { status: "error"; message: string; canSignOut?: boolean }
  | { status: "ready"; user: User; fleets: Fleet[]; fleet: string };

export default function Page() {
  const translate = useT();
  const [boot, setBoot] = useState<Boot>({ status: "loading" });
  const [signingOut, setSigningOut] = useState(false);

  useEffect(() => {
    let canceled = false;
    (async () => {
      try {
        const me = await apiFetch("/api/v1/users/me", { cache: "no-store", credentials: "same-origin" });
        if (canceled) return;
        if (me.status === 401) {
          const next = `${window.location.pathname}${window.location.search}${window.location.hash}`;
          storeAuthNextPath(next);
          window.location.href = loginHref(next);
          return;
        }
        if (!me.ok) {
          setBoot({ status: "error", message: t("auth.bootFailed"), canSignOut: true });
          return;
        }
        const user = (await me.json()) as User;
        const fleets = (await getJSON<Fleet[]>("/api/v1/fleets")) ?? [];
        if (canceled) return;
        if (fleets.length === 0) {
          setBoot({ status: "error", message: t("auth.noFleet"), canSignOut: true });
          return;
        }
        const route = parseAppPath(window.location.pathname);
        const fromUrl = fleets.find((f) => f.id === route.fleetId)?.id;
        const saved = localStorage.getItem("ufo.fleet") ?? "";
        const fleet =
          fromUrl ??
          fleets.find((f) => f.id === saved)?.id ??
          fleets.find((f) => f.kind === "personal")?.id ??
          fleets[0].id;
        localStorage.setItem("ufo.fleet", fleet);
        if (route.fleetId && route.fleetId !== fleet) {
          const suffix = window.location.pathname.replace(/^\/fleets\/[^/]+/, "") || "/operations";
          const nextPath = `/fleets/${encodeURIComponent(fleet)}${suffix.startsWith("/") ? suffix : `/${suffix}`}`;
          window.history.replaceState(null, "", `${nextPath}${window.location.search}${window.location.hash}`);
        }
        setBoot({ status: "ready", user, fleets, fleet });
      } catch {
        if (!canceled) setBoot({ status: "error", message: t("auth.bootFailed"), canSignOut: true });
      }
    })();
    return () => {
      canceled = true;
    };
  }, []);

  async function signOutAndLogin() {
    setSigningOut(true);
    await clearAuthSession();
    clearStoredFleet();
    window.location.href = "/login";
  }

  if (boot.status === "loading") {
    return <div className="flex h-svh items-center justify-center text-sm text-muted-foreground">{translate("common.loading")}</div>;
  }

  if (boot.status === "error") {
    return (
      <div className="flex h-svh flex-col items-center justify-center gap-3 p-6 text-center">
        <p className="text-sm text-destructive">{boot.message}</p>
        {boot.canSignOut ? (
          <button
            type="button"
            disabled={signingOut}
            onClick={signOutAndLogin}
            className="text-sm font-medium text-brand hover:underline disabled:opacity-60"
          >
            {signingOut ? translate("common.loading") : translate("sidebar.signOut")}
          </button>
        ) : (
          <a href="/login" className="text-sm font-medium text-brand hover:underline">{translate("auth.signIn")}</a>
        )}
      </div>
    );
  }

  return (
    <AppProvider user={boot.user} fleets={boot.fleets} initialFleet={boot.fleet}>
      <AppShell />
    </AppProvider>
  );
}
