import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";

export function Security() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const sec = useQuery({
    queryKey: ["security"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/me/security");
      if (!data) throw new Error("security failed");
      return data;
    },
  });
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const [qr, setQr] = useState("");
  const enroll = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/mfa/totp");
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
    onSuccess: async (d) => setQr(await QRCode.toDataURL(d.uri, { margin: 1, width: 192 })),
  });
  useEffect(() => {
    if (!enroll.data) setQr("");
  }, [enroll.data]);
  const confirm = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/mfa/totp/confirm", { body: { code } });
      if (!data) throw new Error(problemMessage(error));
      return data.recovery_codes;
    },
    onSuccess: async (codes) => {
      setRecovery(codes);
      setCode("");
      enroll.reset();
      await qc.invalidateQueries({ queryKey: ["security"] });
    },
  });
  const disable = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.DELETE("/v1/me/mfa/totp", { body: { password } });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setPassword("");
      await qc.invalidateQueries({ queryKey: ["security"] });
    },
  });
  if (!sec.data?.mfa_available) return null;
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("security.title")}</h2>
      {sec.data.mfa_enabled ? (
        <>
          <p>{t("security.enabled")}</p>
          <div className="flex gap-2">
            <input
              className={field}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t("security.passwordToDisable")}
              aria-label={t("security.passwordToDisable")}
            />
            <button type="button" className={secondary} onClick={() => disable.mutate()}>
              {t("security.disable")}
            </button>
          </div>
          {disable.isError && <p role="alert">{disable.error.message}</p>}
        </>
      ) : enroll.data ? (
        <>
          <p className={muted}>{t("security.scan")}</p>
          {qr && <img src={qr} alt={t("security.qrAlt")} width={192} height={192} />}
          <code className="break-all text-sm">{enroll.data.secret}</code>
          <div className="flex gap-2">
            <input
              className={field}
              inputMode="numeric"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t("auth.code")}
              aria-label={t("auth.code")}
            />
            <button type="button" className={primary} onClick={() => confirm.mutate()}>
              {t("security.confirm")}
            </button>
          </div>
          {confirm.isError && <p role="alert">{confirm.error.message}</p>}
        </>
      ) : (
        <button type="button" className={`${primary} self-start`} onClick={() => enroll.mutate()}>
          {t("security.enable")}
        </button>
      )}
      {recovery && (
        <div role="status" className="rounded bg-amber-100 p-2 text-sm text-amber-900">
          <p>{t("security.recoveryOnce")}</p>
          <ul className="grid grid-cols-2 font-mono">
            {recovery.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
