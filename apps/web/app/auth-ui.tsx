"use client";

import type { ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export { Input as AuthInput } from "@/components/ui/input";
export { Button as AuthButton } from "@/components/ui/button";

const AUTH_NEXT_STORAGE_KEY = "ufo.auth.next";

function safeNextPath(next: string | null): string {
  if (!next) return "/";
  if (!next.startsWith("/") || next.startsWith("//")) return "/";
  return next;
}

export function storeAuthNextPath(next: string): void {
  if (typeof window === "undefined") return;
  const safe = safeNextPath(next);
  try {
    if (safe === "/") sessionStorage.removeItem(AUTH_NEXT_STORAGE_KEY);
    else sessionStorage.setItem(AUTH_NEXT_STORAGE_KEY, safe);
  } catch {
    // Best effort only; login still works without preserving the target.
  }
}

export function authNextPath(consume = false): string {
  if (typeof window === "undefined") return "/";
  let stored = "/";
  try {
    stored = safeNextPath(sessionStorage.getItem(AUTH_NEXT_STORAGE_KEY));
    if (consume) sessionStorage.removeItem(AUTH_NEXT_STORAGE_KEY);
  } catch {
    stored = "/";
  }
  if (stored !== "/") return stored;
  return safeNextPath(new URLSearchParams(window.location.search).get("next"));
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
  return (
    <main className="flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-1 flex items-center gap-2">
            <div className="flex size-7 items-center justify-center rounded-md bg-brand text-sm font-bold text-brand-foreground">U</div>
            <span className="text-xs font-medium text-muted-foreground">UFO — Unified Fleet Orchestrator</span>
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
            <a href={footer.href} className="font-medium text-brand hover:underline" onClick={() => storeAuthNextPath(authNextPath())}>{footer.label}</a>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
