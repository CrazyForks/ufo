"use client";

import { useEffect, useState } from "react";
import {
  AuthCard,
  AuthInput,
  AuthButton,
  authErrorMessage,
  authSwitchHref,
  MAX_AUTH_EMAIL_LEN,
  MAX_AUTH_NAME_LEN,
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

export default function SignupPage() {
  const t = useT();
  useCaptureAuthNext();
  useRedirectIfAuthenticated();
  const prefilledEmail = useAuthEmailPrefill();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [emailTaken, setEmailTaken] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (prefilledEmail) setEmail(prefilledEmail);
  }, [prefilledEmail]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const trimmedName = name.trim();
    const trimmedEmail = email.trim().toLowerCase();
    if (!trimmedEmail) {
      setError(t("auth.emailRequired"));
      setEmailTaken(false);
      return;
    }
    if (!validAuthEmail(trimmedEmail)) {
      setError(t("auth.invalidEmail"));
      setEmailTaken(false);
      return;
    }
    if (trimmedName.length > MAX_AUTH_NAME_LEN) {
      setError(t("auth.nameHint"));
      setEmailTaken(false);
      return;
    }
    if (password.length < 8 || password.length > MAX_AUTH_PASSWORD_LEN) {
      setError(t("auth.passwordHint"));
      setEmailTaken(false);
      return;
    }
    setSubmitting(true);
    setError(null);
    setEmailTaken(false);
    const result = await postAuth("/api/v1/auth/signup", {
      name: trimmedName,
      email: trimmedEmail,
      password,
    });
    setSubmitting(false);
    if (result.ok) {
      setStoredFleet(result.fleetId);
      redirectAfterAuth(undefined, result.fleetId);
      return;
    }
    if (result.status === 409) {
      setError(result.error || t("auth.emailTaken"));
      setEmailTaken(true);
      return;
    }
    setError(
      authErrorMessage(
        result,
        t("auth.signupFailed"),
        t("auth.rateLimited"),
        t("auth.networkError"),
        t("auth.sessionFailed"),
        t("auth.noFleet"),
        t("auth.notReady"),
      ),
    );
  }

  const signInHref = authSwitchHref("/login", email);

  return (
    <AuthCard
      title={t("auth.createAccount")}
      error={error}
      footer={{ text: t("auth.haveAccount"), href: signInHref, label: t("auth.signIn") }}
    >
      <form onSubmit={submit} className="space-y-3" noValidate>
        <AuthInput
          name="name"
          autoComplete="name"
          maxLength={MAX_AUTH_NAME_LEN}
          placeholder={t("auth.name")}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <AuthInput
          type="email"
          name="email"
          autoComplete="email"
          required
          maxLength={MAX_AUTH_EMAIL_LEN}
          placeholder={t("auth.email")}
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            setEmailTaken(false);
          }}
        />
        <AuthInput
          type="password"
          name="password"
          autoComplete="new-password"
          required
          minLength={8}
          maxLength={MAX_AUTH_PASSWORD_LEN}
          placeholder={t("auth.passwordHint")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <AuthButton className="w-full" disabled={submitting} type="submit">
          {submitting ? t("auth.creating") : t("auth.createAccountButton")}
        </AuthButton>
        {emailTaken && (
          <p className="text-center text-sm text-muted-foreground">
            <a href={signInHref} className="font-medium text-brand hover:underline">
              {t("auth.signInInstead")}
            </a>
          </p>
        )}
      </form>
    </AuthCard>
  );
}
