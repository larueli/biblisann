import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { muted, secondary } from "../../app/ui";

export function AdminUsers() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const users = useQuery({
    queryKey: ["admin-users"],
    queryFn: async () => (await api.GET("/v1/admin/users", { params: { query: {} } })).data ?? [],
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-users"] });
  const disable = useMutation({
    mutationFn: async (v: { id: string; disabled: boolean }) => {
      const { response, error } = await api.PUT("/v1/admin/users/{id}/disabled", {
        params: { path: { id: v.id } },
        body: { disabled: v.disabled },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  const signOut = useMutation({
    mutationFn: async (id: string) => {
      await api.POST("/v1/admin/users/{id}/revoke-sessions", { params: { path: { id } } });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => {
      const { response, error } = await api.DELETE("/v1/admin/users/{id}", {
        params: { path: { id }, query: { promote_successors: true } },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("admin.users")}</h2>
      <ul className="flex flex-col gap-2">
        {users.data?.map((u) => (
          <li key={u.id} className="flex flex-wrap items-center justify-between gap-2">
            <span>
              {u.email} {u.is_admin && <strong>({t("admin.admin")})</strong>}{" "}
              {u.disabled && <span className={muted}>{t("admin.disabled")}</span>}
            </span>
            <span className="flex gap-1">
              <button
                type="button"
                className={secondary}
                onClick={() => disable.mutate({ id: u.id, disabled: !u.disabled })}
              >
                {t(u.disabled ? "admin.enable" : "admin.disable")}
              </button>
              <button type="button" className={secondary} onClick={() => signOut.mutate(u.id)}>
                {t("admin.signOut")}
              </button>
              <button
                type="button"
                className={secondary}
                onClick={() => {
                  if (window.confirm(t("admin.confirmDelete", { email: u.email })))
                    remove.mutate(u.id);
                }}
              >
                {t("admin.delete")}
              </button>
            </span>
          </li>
        ))}
      </ul>
      {(disable.isError || remove.isError) && (
        <p role="alert">{(disable.error ?? remove.error)?.message}</p>
      )}
    </section>
  );
}
