import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type ChangeEvent, type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { clearLocalData, useSession } from "../auth/session";

function Sessions() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const sessions = useQuery({
    queryKey: ["sessions"],
    queryFn: async () => (await api.GET("/v1/me/sessions")).data ?? [],
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["sessions"] });
  const revoke = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/me/sessions/{id}", { params: { path: { id } } });
    },
    onSuccess: refresh,
  });
  const revokeOthers = useMutation({
    mutationFn: async () => {
      await api.POST("/v1/me/sessions/revoke-others");
    },
    onSuccess: refresh,
  });
  const apps = useQuery({
    queryKey: ["apps"],
    queryFn: async () => (await api.GET("/v1/me/authorized-apps")).data ?? [],
  });
  const revokeApp = useMutation({
    mutationFn: async (clientId: string) => {
      await api.DELETE("/v1/me/authorized-apps/{client_id}", {
        params: { path: { client_id: clientId } },
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["apps"] }),
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("account.sessions")}</h2>
      <ul className="flex flex-col gap-1">
        {sessions.data?.map((s) => (
          <li key={s.id} className="flex items-center justify-between gap-2">
            <span className="text-sm">
              {s.user_agent || t("account.unknownDevice")}{" "}
              <span className={muted}>{new Date(s.last_seen).toLocaleString()}</span>
              {s.current && <strong className="ml-2">{t("account.thisDevice")}</strong>}
            </span>
            {!s.current && (
              <button type="button" className={secondary} onClick={() => revoke.mutate(s.id)}>
                {t("settings.revoke")}
              </button>
            )}
          </li>
        ))}
      </ul>
      <button
        type="button"
        className={`${secondary} self-start`}
        onClick={() => revokeOthers.mutate()}
      >
        {t("account.signOutOthers")}
      </button>
      {apps.data && apps.data.length > 0 && (
        <>
          <h3 className="mt-2 font-semibold">{t("account.apps")}</h3>
          <ul className="flex flex-col gap-1">
            {apps.data.map((a) => (
              <li key={a.client_id} className="flex items-center justify-between gap-2">
                <span>
                  {a.name} <span className={muted}>({a.scopes.join(", ")})</span>
                </span>
                <button
                  type="button"
                  className={secondary}
                  onClick={() => revokeApp.mutate(a.client_id)}
                >
                  {t("settings.revoke")}
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

function DataExchange() {
  const { t } = useTranslation();
  const [message, setMessage] = useState<string | null>(null);
  const upload = useMutation({
    mutationFn: async ({
      file,
      path,
    }: {
      file: File;
      path: "/v1/me/import" | "/v1/libraries/import";
    }) => {
      const { data, error } = await api.POST(path, {
        body: file as unknown as never,
        bodySerializer: (b: unknown) => b as BodyInit,
        headers: { "Content-Type": "application/x-tar" },
      });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
    onSuccess: (d) =>
      setMessage(t("account.imported", { libraries: d.libraries, books: d.instances })),
  });
  const pick =
    (path: "/v1/me/import" | "/v1/libraries/import") => (e: ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (file) upload.mutate({ file, path });
      e.target.value = "";
    };
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("account.dataTitle")}</h2>
      <p className={muted}>{t("account.dataHelp")}</p>
      <a className={`${secondary} self-start`} href="/v1/me/export" download>
        {t("account.exportAll")}
      </a>
      <label className="flex flex-col gap-1 text-sm">
        {t("account.importAccount")}
        <input type="file" accept=".tar,application/x-tar" onChange={pick("/v1/me/import")} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t("account.importLibrary")}
        <input
          type="file"
          accept=".tar,application/x-tar"
          onChange={pick("/v1/libraries/import")}
        />
      </label>
      {message && <p role="status">{message}</p>}
      {upload.isError && <p role="alert">{upload.error.message}</p>}
    </section>
  );
}

function DeleteAccount() {
  const { t } = useTranslation();
  const session = useSession();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [promote, setPromote] = useState(false);
  const qc = useQueryClient();
  const del = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.DELETE("/v1/me", {
        body: {
          confirm_email: email,
          ...(password ? { password } : {}),
          promote_successors: promote,
        },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      await clearLocalData(qc);
      window.location.assign("/login");
    },
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    del.mutate();
  }
  return (
    <section className="flex flex-col gap-2 rounded border border-red-400 p-3">
      <h2 className="text-lg font-semibold">{t("account.deleteTitle")}</h2>
      {!open ? (
        <button type="button" className={`${secondary} self-start`} onClick={() => setOpen(true)}>
          {t("account.deleteStart")}
        </button>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-2">
          <p>{t("account.deleteWarning")}</p>
          <input
            className={field}
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={session.data?.user.email ?? ""}
            aria-label={t("account.typeEmail")}
          />
          <input
            className={field}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            aria-label={t("auth.password")}
          />
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={promote}
              onChange={(e) => setPromote(e.target.checked)}
            />
            {t("account.promote")}
          </label>
          <button type="submit" className={primary} disabled={del.isPending}>
            {t("account.deleteConfirm")}
          </button>
          {del.isError && <p role="alert">{del.error.message}</p>}
        </form>
      )}
    </section>
  );
}

export function AccountSection() {
  return (
    <>
      <Sessions />
      <DataExchange />
      <DeleteAccount />
    </>
  );
}
