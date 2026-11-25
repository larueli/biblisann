import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { muted, secondary } from "../../app/ui";

/** Members who may edit a library can ask the providers again about its books (not readers). */
export function canRefresh(role: string | undefined): boolean {
  return role !== undefined && role !== "read";
}

export function RefreshLibraryButton({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const start = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.POST("/v1/libraries/{id}/catalog/refresh", {
        params: { path: { id: libraryId } },
        body: {},
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
  });
  return (
    <div className="flex flex-col gap-1">
      <button type="button" className={`${secondary} self-start`} onClick={() => start.mutate()}>
        {t("refresh.library")}
      </button>
      {start.isSuccess && (
        <p role="status" className={muted}>
          {t("refresh.started")}
        </p>
      )}
      {start.isError && <p role="alert">{start.error.message}</p>}
    </div>
  );
}

export function RefreshBookButton({ editionId }: { editionId: string }) {
  const { t } = useTranslation();
  const refresh = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/editions/{id}/refresh", {
        params: { path: { id: editionId } },
      });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
  });
  return (
    <div className="flex flex-col gap-1">
      <button type="button" className={`${secondary} self-start`} onClick={() => refresh.mutate()}>
        {t("refresh.book")}
      </button>
      {refresh.data?.sync_status && (
        <p role="status" className={muted}>
          {t(`catalogAdmin.status.${refresh.data.sync_status}`)}
        </p>
      )}
      {refresh.isError && <p role="alert">{refresh.error.message}</p>}
    </div>
  );
}
