import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage, type Schemas } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { useSession } from "../auth/session";
import { useLibraries } from "../library/useLibraries";
import { AccountSection } from "./Account";
import { AdminUsers } from "./AdminUsers";
import { Hardcover } from "./Hardcover";
import { Security } from "./Security";

type ChannelKind = Schemas["ChannelBody"]["kind"];

function Tokens() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const tokens = useQuery({
    queryKey: ["tokens"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/me/tokens");
      if (!data) throw new Error("tokens failed");
      return data;
    },
  });
  const [name, setName] = useState("");
  const [write, setWrite] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: async (kind: "pat" | "feed") => {
      const { data, error } = await api.POST("/v1/me/tokens", {
        body: {
          name: name || kind,
          kind,
          ...(kind === "pat" ? { scopes: write ? ["read", "write"] : ["read"] } : {}),
        },
      });
      if (!data) throw new Error(problemMessage(error));
      return { token: data.token, kind };
    },
    onSuccess: async ({ token, kind }) => {
      setSecret(
        kind === "feed" ? `${window.location.origin}/v1/feeds/loans.ics?token=${token}` : token,
      );
      setName("");
      await qc.invalidateQueries({ queryKey: ["tokens"] });
    },
  });
  const revoke = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/me/tokens/{id}", { params: { path: { id } } });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("settings.tokens")}</h2>
      <p className={muted}>{t("settings.tokensHelp")}</p>
      <ul className="flex flex-col gap-1">
        {tokens.data?.map((k) => (
          <li key={k.id} className="flex items-center justify-between">
            <span>
              {k.name}{" "}
              <span className={muted}>
                ({k.kind === "feed" ? t("settings.feed") : k.scopes.join(", ")})
              </span>
            </span>
            <button type="button" className={secondary} onClick={() => revoke.mutate(k.id)}>
              {t("settings.revoke")}
            </button>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        <input
          className={`${field} flex-1`}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("settings.tokenName")}
          aria-label={t("settings.tokenName")}
        />
        <label className="flex items-center gap-1 text-sm">
          <input type="checkbox" checked={write} onChange={(e) => setWrite(e.target.checked)} />
          {t("settings.allowWrite")}
        </label>
        <button type="button" className={primary} onClick={() => create.mutate("pat")}>
          {t("settings.createToken")}
        </button>
        <button type="button" className={secondary} onClick={() => create.mutate("feed")}>
          {t("settings.createFeed")}
        </button>
      </div>
      {secret && (
        <p role="status" className="break-all rounded bg-amber-100 p-2 text-sm text-amber-900">
          {t("settings.shownOnce")} <code>{secret}</code>
        </p>
      )}
    </section>
  );
}

function Channels() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const channels = useQuery({
    queryKey: ["channels"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/me/notification-channels");
      if (!data) throw new Error("channels failed");
      return data;
    },
  });
  const [kind, setKind] = useState<ChannelKind>("ntfy");
  const [target, setTarget] = useState("");
  const [shownSecret, setShownSecret] = useState<string | null>(null);
  const add = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/me/notification-channels", {
        body: { kind, target },
      });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
    onSuccess: async (c) => {
      setShownSecret(c.secret ?? null);
      setTarget("");
      await qc.invalidateQueries({ queryKey: ["channels"] });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/me/notification-channels/{id}", { params: { path: { id } } });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
  const test = useMutation({
    mutationFn: async () => {
      const { response } = await api.POST("/v1/me/notification-channels/test");
      if (!response.ok) throw new Error(String(response.status));
    },
  });
  const kinds = channels.data?.available.filter((k) => k !== "webpush") ?? [];
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("settings.notifications")}</h2>
      <ul className="flex flex-col gap-1">
        {channels.data?.channels.map((c) => (
          <li key={c.id} className="flex items-center justify-between gap-2">
            <span className="break-all">
              {c.kind}: {c.target}
            </span>
            <button type="button" className={secondary} onClick={() => remove.mutate(c.id)}>
              {t("settings.revoke")}
            </button>
          </li>
        ))}
      </ul>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          add.mutate();
        }}
      >
        <select
          className={field}
          value={kind}
          onChange={(e) => setKind(e.target.value as ChannelKind)}
          aria-label={t("settings.channelKind")}
        >
          {kinds.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        <input
          className={`${field} flex-1`}
          required
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          placeholder={t("settings.channelTarget")}
          aria-label={t("settings.channelTarget")}
        />
        <button type="submit" className={primary}>
          {t("settings.addChannel")}
        </button>
      </form>
      {add.isError && <p role="alert">{add.error.message}</p>}
      {shownSecret && (
        <p role="status" className="break-all rounded bg-amber-100 p-2 text-sm text-amber-900">
          {t("settings.webhookSecret")} <code>{shownSecret}</code>
        </p>
      )}
      <button type="button" className={`${secondary} self-start`} onClick={() => test.mutate()}>
        {t("settings.sendTest")}
      </button>
      {test.isSuccess && <p role="status">{t("settings.testSent")}</p>}
      {test.isError && <p role="alert">{t("settings.testFailed")}</p>}
    </section>
  );
}

function GoodreadsImport() {
  const { t } = useTranslation();
  const libraries = useLibraries();
  const libraryId = libraries.data?.[0]?.id;
  const [source, setSource] = useState<"goodreads" | "storygraph" | "librarything">("goodreads");
  const imp = useMutation({
    mutationFn: async (file: File) => {
      if (!libraryId) throw new Error("no library");
      const text = await file.text();
      const { data, error } = await api.POST("/v1/libraries/{id}/import/{source}", {
        params: { path: { id: libraryId, source } },
        body: text as unknown as never,
        bodySerializer: (b: unknown) => String(b),
        headers: { "Content-Type": "text/csv" },
      });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("settings.importTitle")}</h2>
      <select
        className={field}
        value={source}
        onChange={(e) => setSource(e.target.value as typeof source)}
        aria-label={t("settings.importSource")}
      >
        <option value="goodreads">Goodreads</option>
        <option value="storygraph">StoryGraph</option>
        <option value="librarything">LibraryThing</option>
      </select>
      <input
        type="file"
        accept=".csv,.tsv,text/csv,text/tab-separated-values"
        aria-label={t("settings.importFile")}
        onChange={(e) => e.target.files?.[0] && imp.mutate(e.target.files[0])}
      />
      {imp.data && (
        <p role="status">
          {t("settings.imported", {
            books: imp.data.editions_created,
            copies: imp.data.instances_added,
          })}
        </p>
      )}
      {imp.isError && <p role="alert">{imp.error.message}</p>}
    </section>
  );
}

export function SettingsPage() {
  const { t } = useTranslation();
  const session = useSession();
  return (
    <main className="flex flex-col gap-8">
      <Security />
      <Tokens />
      <Channels />
      <GoodreadsImport />
      <Hardcover />
      <AccountSection />
      {session.data?.user.is_admin && <AdminUsers />}
      {session.data?.user.is_admin && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-semibold">{t("settings.backup")}</h2>
          <p className={muted}>{t("settings.backupHelp")}</p>
          <a className={`${secondary} self-start`} href="/v1/admin/backup" download>
            {t("settings.downloadBackup")}
          </a>
        </section>
      )}
    </main>
  );
}
