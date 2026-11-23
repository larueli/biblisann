import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { field, muted, primary } from "../../app/ui";
import { LanguageSwitcher } from "../home/LanguageSwitcher";
import { useAuthActions } from "./session";

/** Only same-origin paths are accepted as a post-login destination (no open redirect). */
export function safeNext(next: string | undefined): string | null {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) return null;
  return next;
}

export function LoginPage() {
  const { t } = useTranslation();
  const { login, register, completeMfa } = useAuthActions();
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as { next?: string; sso?: string };
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [mfaToken, setMfaToken] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const sso = useQuery({
    queryKey: ["oidc-info"],
    queryFn: async () => (await api.GET("/v1/auth/oidc")).data ?? { enabled: false },
  });
  const done = () => {
    const next = safeNext(search.next);
    if (next) window.location.assign(next);
    else void navigate({ to: "/" });
  };
  const action = mode === "login" ? login : register;

  function submit(e: FormEvent) {
    e.preventDefault();
    if (mfaToken !== null) {
      completeMfa.mutate({ mfaToken, code }, { onSuccess: done });
    } else if (mode === "login") {
      login.mutate(
        { email, password },
        { onSuccess: (r) => (r.mfaToken === null ? done() : setMfaToken(r.mfaToken)) },
      );
    } else {
      register.mutate({ email, password, name }, { onSuccess: done });
    }
  }

  return (
    <main className="mx-auto flex min-h-dvh max-w-sm flex-col justify-center gap-6 p-6">
      <header className="flex items-start justify-between">
        <h1 className="text-3xl font-bold">{t("app.name")}</h1>
        <LanguageSwitcher />
      </header>
      {search.sso && (
        <p role="alert" className="text-red-700 dark:text-red-400">
          {t(`auth.sso.${search.sso}`, { defaultValue: t("auth.sso.error") })}
        </p>
      )}
      <form onSubmit={submit} className="flex flex-col gap-3">
        {mfaToken !== null ? (
          <>
            <h2 className="text-xl font-semibold">{t("auth.mfaTitle")}</h2>
            <p className={muted}>{t("auth.mfaHelp")}</p>
            <input
              className={field}
              required
              autoComplete="one-time-code"
              inputMode="numeric"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              aria-label={t("auth.code")}
            />
            {completeMfa.isError && <p role="alert">{completeMfa.error.message}</p>}
            <button type="submit" disabled={completeMfa.isPending} className={primary}>
              {t("auth.verify")}
            </button>
          </>
        ) : (
          <>
            <h2 className="text-xl font-semibold">
              {t(mode === "login" ? "auth.signIn" : "auth.createAccount")}
            </h2>
            {mode === "register" && (
              <label className="flex flex-col gap-1">
                <span>{t("auth.name")}</span>
                <input
                  className={field}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoComplete="name"
                />
              </label>
            )}
            <label className="flex flex-col gap-1">
              <span>{t("auth.email")}</span>
              <input
                className={field}
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="email"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span>{t("auth.password")}</span>
              <input
                className={field}
                type="password"
                required
                minLength={mode === "register" ? 10 : 1}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={mode === "login" ? "current-password" : "new-password"}
              />
            </label>
            {mode === "register" && <p className={muted}>{t("auth.passwordHint")}</p>}
            {action.isError && (
              <p role="alert" className="text-red-700 dark:text-red-400">
                {action.error.message}
              </p>
            )}
            <button type="submit" disabled={action.isPending} className={primary}>
              {t(mode === "login" ? "auth.signIn" : "auth.createAccount")}
            </button>
            <button
              type="button"
              className="text-sm underline"
              onClick={() => setMode(mode === "login" ? "register" : "login")}
            >
              {t(mode === "login" ? "auth.switchToRegister" : "auth.switchToLogin")}
            </button>
            {mode === "login" && (
              <a className="text-sm underline" href="/reset">
                {t("auth.forgot")}
              </a>
            )}
          </>
        )}
      </form>
      {sso.data?.enabled && mfaToken === null && (
        <a className={`${primary} text-center`} href="/v1/auth/oidc/login">
          {sso.data.label || t("auth.sso.button")}
        </a>
      )}
    </main>
  );
}
