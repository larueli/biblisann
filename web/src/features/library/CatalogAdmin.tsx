import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, secondary } from "../../app/ui";

/** Administrators fix the shared catalog by hand, refresh it, or shield it from providers. */
export function CatalogAdmin({ editionId }: { editionId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const edition = useQuery({
    queryKey: ["edition", editionId],
    queryFn: async () => {
      const { data } = await api.GET("/v1/editions/{id}", { params: { path: { id: editionId } } });
      if (!data) throw new Error("edition failed");
      return data;
    },
  });
  const [title, setTitle] = useState("");
  const [authors, setAuthors] = useState("");
  const refresh = async () => {
    await qc.invalidateQueries({ queryKey: ["edition", editionId] });
    await qc.invalidateQueries({ queryKey: ["instances"] });
  };
  const params = { params: { path: { id: editionId } } };
  const save = useMutation({
    mutationFn: async () => {
      const { error } = await api.PATCH("/v1/editions/{id}", {
        ...params,
        body: {
          ...(title ? { title } : {}),
          ...(authors
            ? {
                authors: authors
                  .split(",")
                  .map((a) => a.trim())
                  .filter(Boolean),
              }
            : {}),
        },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setTitle("");
      setAuthors("");
      await refresh();
    },
  });
  const providers = useMutation({
    mutationFn: async () => {
      const { error } = await api.POST("/v1/editions/{id}/refresh", params);
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  const exclude = useMutation({
    mutationFn: async (excluded: boolean) => {
      const { error } = await api.PUT("/v1/editions/{id}/sync-excluded", {
        ...params,
        body: { excluded },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  const err = save.error ?? providers.error ?? exclude.error;

  return (
    <fieldset className="flex flex-col gap-2 rounded border border-slate-400 p-2">
      <legend className={muted}>{t("catalogAdmin.title")}</legend>
      <form
        className="flex flex-col gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <input
          className={field}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={edition.data?.title ?? t("catalogAdmin.editTitle")}
          aria-label={t("catalogAdmin.editTitle")}
        />
        <input
          className={field}
          value={authors}
          onChange={(e) => setAuthors(e.target.value)}
          placeholder={edition.data?.authors.join(", ") || t("catalogAdmin.editAuthors")}
          aria-label={t("catalogAdmin.editAuthors")}
        />
        <button type="submit" className={`${secondary} self-start`}>
          {t("catalogAdmin.save")}
        </button>
      </form>
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          className={secondary}
          disabled={edition.data?.sync_excluded === true}
          onClick={() => providers.mutate()}
        >
          {t("catalogAdmin.refresh")}
        </button>
        <label className="flex items-center gap-1">
          <input
            type="checkbox"
            checked={edition.data?.sync_excluded === true}
            onChange={(e) => exclude.mutate(e.target.checked)}
          />
          {t("catalogAdmin.exclude")}
        </label>
      </div>
      {err && <p role="alert">{err.message}</p>}
    </fieldset>
  );
}
