"use client";

import { useEffect, useState } from "react";
import {
  AuthCard,
  AuthInput,
  AuthButton,
  authErrorMessage,
  authSwitchHref,
  MAX_AUTH_EMAIL_LEN,
  MAX_AUTH_PASSWORD_LEN,
  postAuth,
  redirectAfterAuth,
  setStoredFleet,
  useAuthEmailPrefill,
  useCaptureAuthNext,
  useRedirectIfAuthenticated,
  validAuthEmail,
} from "../auth-ui";
import { useT } from "@/lib/i18n";

export default function LoginPage() {
  const t = useT();
  useCaptureAuthNext();
  useRedirectIfAuthenticated();
  const prefilledEmail = useAuthEmailPrefill();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (prefilledEmail) setEmail(prefilledEmail);
  }, [prefilledEmail]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const trimmedEmail = email.trim().toLowerCase();
    if (!trimmedEmail || !password) {
      setError(t("auth.loginFailed"));
      return;
    }
    if (!validAuthEmail(trimmedEmail)) {
      setError(t("auth.invalidEmail"));
      return;
    }
    if (password.length > MAX_AUTH_PASSWORD_LEN) {
      setError(t("auth.loginFailed"));
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = await postAuth("/api/v1/auth/login", {
      email: trimmedEmail,
      password,
    });
    setSubmitting(false);
    if (result.ok) {
      setStoredFleet(result.fleetId);
      redirectAfterAuth(undefined, result.fleetId);
      return;
    }
    setError(
      authErrorMessage(
        result,
        t("auth.loginFailed"),
        t("auth.rateLimited"),
        t("auth.networkError"),
        t("auth.sessionFailed"),
        t("auth.noFleet"),
      ),
    );
  }

  return (
    <AuthCard
      title={t("auth.signIn")}
      error={error}
      footer={{ text: t("auth.needAccount"), href: authSwitchHref("/signup", email), label: t("auth.signUp") }}
    >
      <form onSubmit={submit} className="space-y-3" noValidate>
        <AuthInput
          type="email"
          name="email"
          autoComplete="email"
          required
          maxLength={MAX_AUTH_EMAIL_LEN}
          placeholder={t("auth.email")}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <AuthInput
          type="password"
          name="password"
          autoComplete="current-password"
          required
          maxLength={MAX_AUTH_PASSWORD_LEN}
          placeholder={t("auth.password")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <AuthButton className="w-full" disabled={submitting} type="submit">
          {submitting ? t("auth.signingIn") : t("auth.signIn")}
        </AuthButton>
      </form>
    </AuthCard>
  );
}
