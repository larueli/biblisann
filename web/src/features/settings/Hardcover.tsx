import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";

type Mode = "pull" | "push" | "both";

export function Hardcover() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const link = useQuery({
    queryKey: ["hardcover"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/me/hardcover");
      if (!data) throw new Error("hardcover failed");
      return data;
    },
  });
  const [token, setToken] = useState("");
  const [mode, setMode] = useState<Mode>("both");
  const refresh = () => qc.invalidateQueries({ queryKey: ["hardcover"] });
  const save = useMutation({
    mutationFn: async () => {
      const { error } = await api.PUT("/v1/me/hardcover", {
        body: { mode, ...(token ? { token } : {}) },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setToken("");
      await refresh();
    },
  });
  const unlink = useMutation({
    mutationFn: async () => {
      await api.DELETE("/v1/me/hardcover");
    },
    onSuccess: refresh,
  });
  const syncNow = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/hardcover/sync");
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
    onSuccess: refresh,
  });
  if (!link.data?.available) return null;
  const linked = link.data.linked;
  const current = (link.data.mode as Mode | undefined) ?? mode;
  function submit(e: FormEvent) {
    e.preventDefault();
    save.mutate();
  }
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("hardcover.title")}</h2>
      <p className={muted}>{t("hardcover.help")}</p>
      <form onSubmit={submit} className="flex flex-col gap-2">
        <label className="flex flex-col gap-1">
          <span>{t("hardcover.direction")}</span>
          <select
            className={field}
            value={linked ? current : mode}
            onChange={(e) => setMode(e.target.value as Mode)}
          >
            <option value="both">{t("hardcover.both")}</option>
            <option value="pull">{t("hardcover.pull")}</option>
            <option value="push">{t("hardcover.push")}</option>
          </select>
        </label>
        <input
          className={field}
          type="password"
          autoComplete="off"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder={linked ? t("hardcover.tokenOptional") : t("hardcover.token")}
          aria-label={t("hardcover.token")}
          required={!linked}
        />
        <div className="flex flex-wrap gap-2">
          <button type="submit" className={primary} disabled={save.isPending}>
            {t(linked ? "hardcover.update" : "hardcover.link")}
          </button>
          {linked && (
            <>
              <button
                type="button"
                className={secondary}
                onClick={() => syncNow.mutate()}
                disabled={syncNow.isPending}
              >
                {t("hardcover.syncNow")}
              </button>
              <button type="button" className={secondary} onClick={() => unlink.mutate()}>
                {t("hardcover.unlink")}
              </button>
            </>
          )}
        </div>
      </form>
      {linked && link.data.last_sync_at && (
        <p className={muted}>
          {t("hardcover.lastSync", { when: new Date(link.data.last_sync_at).toLocaleString() })}
        </p>
      )}
      {link.data.last_error && <p role="alert">{link.data.last_error}</p>}
      {save.isError && <p role="alert">{save.error.message}</p>}
      {syncNow.isSuccess && (
        <p role="status">
          {t("hardcover.synced", { pulled: syncNow.data.pulled, pushed: syncNow.data.pushed })}
        </p>
      )}
      {syncNow.isError && <p role="alert">{syncNow.error.message}</p>}
    </section>
  );
}
