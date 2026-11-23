import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";

function Members({ groupId }: { groupId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const members = useQuery({
    queryKey: ["group-members", groupId],
    queryFn: async () =>
      (await api.GET("/v1/groups/{id}/members", { params: { path: { id: groupId } } })).data ?? [],
  });
  const [email, setEmail] = useState("");
  const set = useMutation({
    mutationFn: async (v: { email: string; member: boolean }) => {
      const { response, error } = await api.PUT("/v1/groups/{id}/members", {
        params: { path: { id: groupId } },
        body: v,
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setEmail("");
      await qc.invalidateQueries({ queryKey: ["group-members", groupId] });
    },
  });
  return (
    <div className="ml-4 mt-1 flex flex-col gap-1">
      <ul>
        {members.data?.map((m) => (
          <li key={m.user_id} className="flex items-center justify-between gap-2 text-sm">
            {m.email}
            <button
              type="button"
              className={secondary}
              onClick={() => set.mutate({ email: m.email, member: false })}
            >
              {t("settings.revoke")}
            </button>
          </li>
        ))}
        {members.data?.length === 0 && <li className={muted}>{t("groups.empty")}</li>}
      </ul>
      <form
        className="flex gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          set.mutate({ email, member: true });
        }}
      >
        <input
          className={field}
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder={t("groups.memberEmail")}
          aria-label={t("groups.memberEmail")}
        />
        <button type="submit" className={primary}>
          {t("groups.addMember")}
        </button>
      </form>
      {set.isError && <p role="alert">{set.error.message}</p>}
    </div>
  );
}

/** Instance administrators manage user groups here (groups are then used to share libraries). */
export function AdminGroups() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const groups = useQuery({
    queryKey: ["groups"],
    queryFn: async () => (await api.GET("/v1/groups")).data ?? [],
  });
  const [name, setName] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["groups"] });
  const create = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/groups", { body: { name } });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setName("");
      await refresh();
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/groups/{id}", { params: { path: { id } } });
    },
    onSuccess: refresh,
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("groups.title")}</h2>
      <p className={muted}>{t("groups.help")}</p>
      <ul className="flex flex-col gap-1">
        {groups.data?.map((g) => (
          <li key={g.id}>
            <div className="flex items-center justify-between gap-2">
              <button
                type="button"
                className="underline"
                onClick={() => setOpen(open === g.id ? null : g.id)}
              >
                {g.name}
              </button>
              <button
                type="button"
                className={secondary}
                onClick={() => {
                  if (window.confirm(t("groups.confirmDelete", { name: g.name })))
                    remove.mutate(g.id);
                }}
              >
                {t("admin.delete")}
              </button>
            </div>
            {open === g.id && <Members groupId={g.id} />}
          </li>
        ))}
      </ul>
      <form
        className="flex gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <input
          className={field}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("groups.name")}
          aria-label={t("groups.name")}
        />
        <button type="submit" className={primary}>
          {t("groups.create")}
        </button>
      </form>
      {create.isError && <p role="alert">{create.error.message}</p>}
    </section>
  );
}
