import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { muted } from "../../app/ui";
import { Button } from "../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
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
    <Dialog open onOpenChange={(o) => !o && finish(false)}>
      <DialogContent>
        <DialogTitle className="text-lg font-semibold">{t("stepUp.title")}</DialogTitle>
        <DialogDescription className={muted}>{t("stepUp.explain")}</DialogDescription>
        {sec.data?.has_password && (
          <form onSubmit={withPassword} className="flex flex-col gap-2">
            <Input
              type="password"
              autoComplete="current-password"
              placeholder={t("stepUp.password")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            {sec.data.mfa_enabled && (
              <Input
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder={t("stepUp.code")}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
              />
            )}
            <Button type="submit" disabled={busy}>
              {t("stepUp.confirm")}
            </Button>
          </form>
        )}
        {sec.data?.passkeys_available && passkeysSupported() && (
          <Button type="button" variant="outline" disabled={busy} onClick={withPasskey}>
            {t("stepUp.passkey")}
          </Button>
        )}
        {sec.data?.sso_available && (
          <Button type="button" variant="outline" disabled={busy} onClick={withSso}>
            {t("stepUp.sso")}
          </Button>
        )}
        {error && (
          <p role="alert" className="text-sm text-red-700 dark:text-red-400">
            {error}
          </p>
        )}
        <Button type="button" variant="outline" onClick={() => finish(false)}>
          {t("stepUp.cancel")}
        </Button>
      </DialogContent>
    </Dialog>
  );
}
