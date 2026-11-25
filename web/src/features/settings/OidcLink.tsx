import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { muted, secondary } from "../../app/ui";

export function OidcLink() {
  const { t } = useTranslation();
  const oidc = useQuery({
    queryKey: ["oidc-info"],
    queryFn: async () => (await api.GET("/v1/auth/oidc")).data,
  });
  const link = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/auth/oidc/link");
      if (!data) throw new Error(problemMessage(error));
      return data.url;
    },
    onSuccess: (url) => window.location.assign(url),
  });
  if (!oidc.data?.enabled) return null;
  const result = new URLSearchParams(window.location.search).get("oidc_link");
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("oidcLink.title")}</h2>
      <p className={muted}>{t("oidcLink.help")}</p>
      <button
        type="button"
        className={`${secondary} self-start`}
        onClick={() => link.mutate()}
        disabled={link.isPending}
      >
        {t("oidcLink.link", { provider: oidc.data.label || t("oidcLink.provider") })}
      </button>
      {result === "ok" && <p role="status">{t("oidcLink.success")}</p>}
      {(result === "failed" || link.isError) && <p role="alert">{t("oidcLink.failed")}</p>}
    </section>
  );
}
