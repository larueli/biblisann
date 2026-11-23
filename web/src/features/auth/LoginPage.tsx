import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { LanguageSwitcher } from "../home/LanguageSwitcher";
import { useAuthActions } from "./session";

export function LoginPage() {
  const { t } = useTranslation();
  const { login, register } = useAuthActions();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const navigate = useNavigate();
  const toHome = { onSuccess: () => void navigate({ to: "/" }) };
  const action = mode === "login" ? login : register;

  function submit(e: FormEvent) {
    e.preventDefault();
    if (mode === "login") login.mutate({ email, password }, toHome);
    else register.mutate({ email, password, name }, toHome);
  }

  const field = "w-full rounded border border-slate-400 bg-transparent px-3 py-2";
  return (
    <main className="mx-auto flex min-h-dvh max-w-sm flex-col justify-center gap-6 p-6">
      <header className="flex items-start justify-between">
        <h1 className="text-3xl font-bold">{t("app.name")}</h1>
        <LanguageSwitcher />
      </header>
      <form onSubmit={submit} className="flex flex-col gap-3">
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
        {mode === "register" && (
          <p className="text-sm text-slate-600 dark:text-slate-300">{t("auth.passwordHint")}</p>
        )}
        {action.isError && (
          <p role="alert" className="text-red-700 dark:text-red-400">
            {action.error.message}
          </p>
        )}
        <button
          type="submit"
          disabled={action.isPending}
          className="rounded bg-slate-800 px-4 py-2 text-white disabled:opacity-50 dark:bg-slate-200 dark:text-slate-900"
        >
          {t(mode === "login" ? "auth.signIn" : "auth.createAccount")}
        </button>
        <button
          type="button"
          className="text-sm underline"
          onClick={() => setMode(mode === "login" ? "register" : "login")}
        >
          {t(mode === "login" ? "auth.switchToRegister" : "auth.switchToLogin")}
        </button>
      </form>
    </main>
  );
}
