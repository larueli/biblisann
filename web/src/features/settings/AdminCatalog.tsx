import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";

/** Administrators ask the providers again about books they did not know (a weekly run does it too). */
export function AdminCatalog() {
  const { t } = useTranslation();
  const libraries = useLibraries();
  const [libraryId, setLibraryId] = useState("");
  const [all, setAll] = useState(false);
  const start = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/admin/catalog/refresh", {
        body: { ...(libraryId ? { library_id: libraryId } : {}), all },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
  });
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{t("adminCatalog.title")}</h2>
      <p className={muted}>{t("adminCatalog.help")}</p>
      <div className="flex flex-wrap items-center gap-2">
        <select
          className={field}
          value={libraryId}
          onChange={(e) => setLibraryId(e.target.value)}
          aria-label={t("adminCatalog.scope")}
        >
          <option value="">{t("adminCatalog.wholeInstance")}</option>
          {libraries.data?.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
          {t("adminCatalog.all")}
        </label>
        <button type="button" className={secondary} onClick={() => start.mutate()}>
          {t("adminCatalog.start")}
        </button>
      </div>
      {start.isSuccess && <p role="status">{t("adminCatalog.started")}</p>}
      {start.isError && <p role="alert">{start.error.message}</p>}
    </section>
  );
}
