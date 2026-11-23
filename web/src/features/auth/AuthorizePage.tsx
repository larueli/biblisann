import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { muted, primary, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";

/** Consent screen of the OAuth authorization server (MCP clients). */
export function AuthorizePage() {
  const { t } = useTranslation();
  const q = new URLSearchParams(window.location.search);
  const request = {
    client_id: q.get("client_id") ?? "",
    redirect_uri: q.get("redirect_uri") ?? "",
    code_challenge: q.get("code_challenge") ?? "",
    code_challenge_method: q.get("code_challenge_method") ?? "",
    ...(q.get("state") ? { state: q.get("state") as string } : {}),
    ...(q.get("scope") ? { scope: q.get("scope") as string } : {}),
    ...(q.get("resource") ? { resource: q.get("resource") as string } : {}),
  };
  const info = useQuery({
    queryKey: ["oauth-info", window.location.search],
    retry: false,
    queryFn: async () => {
      const { data, error } = await api.POST("/v1/oauth/request", { body: request });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
  });
  const libraries = useLibraries();
  const [write, setWrite] = useState(true); // read-write by default at consent time
  const [only, setOnly] = useState<string[]>([]);
  const decide = useMutation({
    mutationFn: async (approve: boolean) => {
      const { data, error } = await api.POST("/v1/oauth/consent", {
        body: {
          request,
          approve,
          scopes: write ? ["read", "write"] : ["read"],
          ...(only.length > 0 ? { library_ids: only } : {}),
        },
      });
      if (!data) throw new Error(problemMessage(error));
      return data.redirect_to;
    },
    onSuccess: (url) => window.location.assign(url),
  });

  if (info.isError) {
    // Never redirect back to an unverified address.
    return (
      <main className="mx-auto max-w-md p-6">
        <p role="alert">{t("oauth.invalid")}</p>
      </main>
    );
  }
  const wantsWrite = info.data?.requested_scopes.includes("write") ?? false;
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-4 p-6">
      <h1 className="text-2xl font-bold">
        {t("oauth.title", { client: info.data?.client_name ?? "…" })}
      </h1>
      <p>{t("oauth.explain")}</p>
      <label className="flex items-center gap-2">
        <input type="checkbox" checked={write} onChange={(e) => setWrite(e.target.checked)} />
        {t("oauth.allowWrite")}{" "}
        {wantsWrite ? "" : <span className={muted}>({t("oauth.notRequested")})</span>}
      </label>
      {libraries.data && libraries.data.length > 1 && (
        <fieldset className="flex flex-col gap-1">
          <legend className="font-semibold">{t("oauth.libraries")}</legend>
          <p className={muted}>{t("oauth.librariesHelp")}</p>
          {libraries.data.map((l) => (
            <label key={l.id} className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={only.includes(l.id)}
                onChange={(e) =>
                  setOnly(e.target.checked ? [...only, l.id] : only.filter((x) => x !== l.id))
                }
              />
              {l.name}
            </label>
          ))}
        </fieldset>
      )}
      <div className="flex gap-2">
        <button
          type="button"
          className={primary}
          disabled={!info.data || decide.isPending}
          onClick={() => decide.mutate(true)}
        >
          {t("oauth.allow")}
        </button>
        <button
          type="button"
          className={secondary}
          disabled={decide.isPending}
          onClick={() => decide.mutate(false)}
        >
          {t("oauth.deny")}
        </button>
      </div>
      {decide.isError && <p role="alert">{decide.error.message}</p>}
    </main>
  );
}
