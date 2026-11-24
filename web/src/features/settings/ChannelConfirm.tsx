import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, secondary } from "../../app/ui";

/** An email channel stays silent until the code mailed to the address is entered here. */
export function ChannelConfirm({ id }: { id: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const params = { params: { path: { id } } };
  const confirm = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/me/notification-channels/{id}/confirm", {
        ...params,
        body: { code },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
  const resend = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/me/notification-channels/{id}/code", params);
      if (!response.ok) throw new Error(problemMessage(error));
    },
  });
  const err = confirm.error ?? resend.error;
  return (
    <form
      className="flex flex-wrap items-center gap-2"
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        confirm.mutate();
      }}
    >
      <span className={muted}>{t("settings.confirmEmail")}</span>
      <input
        className={`${field} w-32`}
        inputMode="numeric"
        autoComplete="one-time-code"
        required
        value={code}
        onChange={(e) => setCode(e.target.value)}
        aria-label={t("settings.confirmCode")}
      />
      <button type="submit" className={secondary}>
        {t("settings.confirm")}
      </button>
      <button type="button" className={secondary} onClick={() => resend.mutate()}>
        {t("settings.resendCode")}
      </button>
      {resend.isSuccess && <span role="status">{t("settings.codeSent")}</span>}
      {err && <span role="alert">{err.message}</span>}
    </form>
  );
}
