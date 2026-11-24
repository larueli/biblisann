import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { getPasskey, passkeysSupported } from "./passkeys";
import { settleStepUp, subscribeStepUp } from "./stepup";

/** Asks the user to prove themselves again before a sensitive action (valid for 5 minutes). */
export function StepUpDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => subscribeStepUp(setOpen), []);
  const sec = useQuery({
    queryKey: ["security"],
    enabled: open,
    queryFn: async () => (await api.GET("/v1/me/security")).data,
  });
  if (!open) return null;

  const finish = (ok: boolean) => {
    setPassword("");
    setCode("");
    setError("");
    setBusy(false);
    settleStepUp(ok);
  };
  const withPassword = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    const { response, error: err } = await api.POST("/v1/auth/step-up", {
      body: code ? { password, code } : { password },
    });
    if (response.ok) return finish(true);
    setBusy(false);
    setError(problemMessage(err));
  };
  const withPasskey = async () => {
    setBusy(true);
    try {
      const begin = await api.POST("/v1/auth/step-up/passkey/begin");
      if (!begin.data) throw new Error(problemMessage(begin.error));
      const credential = await getPasskey(begin.data.options);
      const { response, error: err } = await api.POST("/v1/auth/step-up/passkey/finish", {
        body: { session_id: begin.data.session_id, credential },
      });
      if (!response.ok) throw new Error(problemMessage(err));
      finish(true);
    } catch (e) {
      setBusy(false);
      setError(e instanceof Error ? e.message : t("stepUp.failed"));
    }
  };
  const withSso = () => {
    settleStepUp(false);
    window.location.assign("/v1/auth/oidc/step-up");
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="stepup-title"
        className="flex w-full max-w-sm flex-col gap-3 rounded bg-white p-4 dark:bg-slate-900"
      >
        <h2 id="stepup-title" className="text-lg font-semibold">
          {t("stepUp.title")}
        </h2>
        <p className={muted}>{t("stepUp.explain")}</p>
        {sec.data?.has_password && (
          <form onSubmit={withPassword} className="flex flex-col gap-2">
            <input
              className={field}
              type="password"
              autoComplete="current-password"
              placeholder={t("stepUp.password")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            {sec.data.mfa_enabled && (
              <input
                className={field}
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder={t("stepUp.code")}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
              />
            )}
            <button type="submit" className={primary} disabled={busy}>
              {t("stepUp.confirm")}
            </button>
          </form>
        )}
        {sec.data?.passkeys_available && passkeysSupported() && (
          <button type="button" className={secondary} disabled={busy} onClick={withPasskey}>
            {t("stepUp.passkey")}
          </button>
        )}
        {sec.data?.sso_available && (
          <button type="button" className={secondary} disabled={busy} onClick={withSso}>
            {t("stepUp.sso")}
          </button>
        )}
        {error && (
          <p role="alert" className="text-sm text-red-700 dark:text-red-400">
            {error}
          </p>
        )}
        <button type="button" className={secondary} onClick={() => finish(false)}>
          {t("stepUp.cancel")}
        </button>
      </div>
    </div>
  );
}
