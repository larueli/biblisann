import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage, setCsrfToken } from "../../api/client";
import { field, muted, primary } from "../../app/ui";
import { useSession } from "./session";

/** Landing page of an invitation link: create an account or accept it with the current one. */
export function InvitePage() {
  const { t } = useTranslation();
  const { token = "" } = useSearch({ strict: false }) as { token?: string };
  const qc = useQueryClient();
  const session = useSession();
  const info = useQuery({
    queryKey: ["invitation", token],
    enabled: token !== "",
    retry: false,
    queryFn: async () => {
      const { data } = await api.GET("/v1/invitations/{token}", { params: { path: { token } } });
      return data ?? null;
    },
  });
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  if (info.isLoading || session.isLoading) return <p aria-busy="true" />;
  if (!info.data) {
    return (
      <main className="mx-auto max-w-sm p-6">
        <p role="alert">{t("invite.invalid")}</p>
      </main>
    );
  }
  const accept = async () => {
    const { response, error: err } = await api.POST("/v1/invitations/accept", { body: { token } });
    if (!response.ok) return setError(problemMessage(err));
    await qc.invalidateQueries({ queryKey: ["libraries"] });
    setDone(true);
  };
  const create = async (e: FormEvent) => {
    e.preventDefault();
    const { data, error: err } = await api.POST("/v1/auth/register", {
      body: { email: info.data?.email ?? "", password, name, invitation_token: token },
    });
    if (!data) return setError(problemMessage(err));
    setCsrfToken(data.csrf_token);
    await qc.invalidateQueries({ queryKey: ["session"] });
    await qc.invalidateQueries({ queryKey: ["libraries"] });
    setDone(true);
  };

  return (
    <main className="mx-auto flex max-w-sm flex-col gap-3 p-6">
      <h1 className="text-xl font-bold">
        {t("invite.title", { library: info.data.library_name })}
      </h1>
      {done ? (
        <>
          <p>{t("invite.done")}</p>
          <Link to="/" className="underline">
            {t("notFound.back")}
          </Link>
        </>
      ) : session.data ? (
        <button type="button" className={primary} onClick={accept}>
          {t("invite.accept")}
        </button>
      ) : (
        <>
          <form onSubmit={create} className="flex flex-col gap-2">
            <input className={field} value={info.data.email} readOnly aria-label="email" />
            <input
              className={field}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("invite.name")}
              aria-label={t("invite.name")}
            />
            <input
              className={field}
              type="password"
              autoComplete="new-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t("invite.password")}
              aria-label={t("invite.password")}
            />
            <button type="submit" className={primary}>
              {t("invite.create")}
            </button>
          </form>
          <Link
            to="/login"
            search={{ next: `/invite?token=${encodeURIComponent(token)}` }}
            className={`${muted} underline`}
          >
            {t("invite.signIn")}
          </Link>
        </>
      )}
      {error && <p role="alert">{error}</p>}
    </main>
  );
}
