import { useMutation } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary } from "../../app/ui";

/** Request a reset link (no token) or set a new password (token in the query string). */
export function ResetPage() {
  const { t } = useTranslation();
  const token = new URLSearchParams(window.location.search).get("token") ?? "";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const request = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/auth/password-reset", { body: { email } });
      if (!response.ok) throw new Error(problemMessage(error));
    },
  });
  const confirm = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/auth/password-reset/confirm", {
        body: { token, new_password: password },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
  });
  const m = token ? confirm : request;
  function submit(e: FormEvent) {
    e.preventDefault();
    m.mutate();
  }
  return (
    <main className="mx-auto flex min-h-dvh max-w-sm flex-col justify-center gap-4 p-6">
      <h1 className="text-2xl font-bold">{t("auth.resetTitle")}</h1>
      <form onSubmit={submit} className="flex flex-col gap-3">
        {token ? (
          <input
            className={field}
            type="password"
            required
            minLength={10}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-label={t("auth.newPassword")}
          />
        ) : (
          <input
            className={field}
            type="email"
            required
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-label={t("auth.email")}
          />
        )}
        <button type="submit" className={primary} disabled={m.isPending}>
          {t(token ? "auth.resetSet" : "auth.resetSend")}
        </button>
        {m.isSuccess && <p role="status">{t(token ? "auth.resetDone" : "auth.resetSent")}</p>}
        {m.isError && <p role="alert">{m.error.message}</p>}
      </form>
      <a className={`text-sm underline ${muted}`} href="/login">
        {t("auth.backToLogin")}
      </a>
    </main>
  );
}
