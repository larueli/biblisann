import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { muted } from "./ui";

/** AGPL-3.0 section 13: users of the service are offered the source of the running version. */
export function Footer() {
  const { t } = useTranslation();
  const about = useQuery({
    queryKey: ["about"],
    staleTime: Number.POSITIVE_INFINITY,
    queryFn: async () => (await api.GET("/v1/about")).data,
  });
  if (!about.data) return null;
  return (
    <footer
      className={`${muted} mt-auto flex flex-wrap gap-x-3 gap-y-1 border-t border-slate-300 pt-2 dark:border-slate-700`}
    >
      <span>
        Biblisann {about.data.version} · {about.data.license}
      </span>
      <a className="underline" href={about.data.source_url} rel="noreferrer">
        {t("footer.source")}
      </a>
      <a className="underline" href={about.data.docs_url}>
        {t("footer.api")}
      </a>
    </footer>
  );
}
