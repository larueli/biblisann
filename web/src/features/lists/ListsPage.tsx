import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";

function useLists() {
  return useQuery({
    queryKey: ["lists"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/lists");
      if (!data) throw new Error("lists failed");
      return data;
    },
  });
}

function ListItems({ listId }: { listId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const items = useQuery({
    queryKey: ["list-items", listId],
    queryFn: async () => {
      const { data } = await api.GET("/v1/lists/{id}/items", { params: { path: { id: listId } } });
      if (!data) throw new Error("items failed");
      return data;
    },
  });
  const [title, setTitle] = useState("");
  const add = useMutation({
    mutationFn: async () => {
      const { error } = await api.POST("/v1/lists/{id}/items", {
        params: { path: { id: listId } },
        body: { title },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setTitle("");
      await qc.invalidateQueries({ queryKey: ["list-items", listId] });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/list-items/{id}", { params: { path: { id } } });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["list-items", listId] }),
  });
  return (
    <div className="mt-2 flex flex-col gap-2">
      <ul className="flex flex-col gap-1">
        {items.data?.map((i) => (
          <li key={i.id} className="flex items-center justify-between gap-2">
            <span>
              {i.title} <span className={muted}>{i.authors.join(", ")}</span>
              {i.in_library && (
                <span className="ml-2 rounded bg-green-200 px-1 text-xs text-green-900">
                  {t("lists.owned")}
                </span>
              )}
            </span>
            <button
              type="button"
              className={secondary}
              onClick={() => remove.mutate(i.id)}
              aria-label={t("lists.remove")}
            >
              ✕
            </button>
          </li>
        ))}
        {items.data?.length === 0 && <li className={muted}>{t("lists.emptyList")}</li>}
      </ul>
      <form
        className="flex gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          add.mutate();
        }}
      >
        <input
          className={field}
          required
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={t("lists.itemPlaceholder")}
          aria-label={t("lists.itemPlaceholder")}
        />
        <button type="submit" className={primary}>
          {t("lists.add")}
        </button>
      </form>
    </div>
  );
}

function MissingVolumes({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const lists = useLists();
  const qc = useQueryClient();
  const series = useQuery({
    queryKey: ["series", libraryId],
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries/{id}/series", {
        params: { path: { id: libraryId } },
      });
      if (!data) throw new Error("series failed");
      return data;
    },
  });
  const wishlist = lists.data?.find((l) => l.kind === "wishlist");
  const addMissing = useMutation({
    mutationFn: async (name: string) => {
      if (!wishlist) return;
      await api.POST("/v1/libraries/{id}/series/missing-to-list", {
        params: { path: { id: libraryId } },
        body: { series: name, list_id: wishlist.id },
      });
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["list-items"] });
    },
  });
  const incomplete = series.data?.filter((s) => s.missing.length > 0) ?? [];
  if (incomplete.length === 0) return null;
  return (
    <section>
      <h2 className="text-lg font-semibold">{t("lists.missing")}</h2>
      <ul className="mt-2 flex flex-col gap-1">
        {incomplete.map((s) => (
          <li key={s.name} className="flex items-center justify-between gap-2">
            <span>
              {s.name}:{" "}
              <span className={muted}>
                {t("lists.missingVolumes", { volumes: s.missing.join(", ") })}
              </span>
            </span>
            <button type="button" className={secondary} onClick={() => addMissing.mutate(s.name)}>
              {t("lists.addMissing")}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function ListsPage() {
  const { t } = useTranslation();
  const lists = useLists();
  const libraries = useLibraries();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: async () => {
      const { error } = await api.POST("/v1/lists", { body: { name } });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setName("");
      await qc.invalidateQueries({ queryKey: ["lists"] });
    },
  });
  const firstLibrary = libraries.data?.[0]?.id;
  return (
    <main className="flex flex-col gap-6">
      {lists.data?.map((l) => (
        <section key={l.id}>
          <h2 className="text-lg font-semibold">
            {l.kind === "wishlist" ? t("lists.wishlist") : l.name}
          </h2>
          <ListItems listId={l.id} />
        </section>
      ))}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <input
          className={field}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("lists.newList")}
          aria-label={t("lists.newList")}
        />
        <button type="submit" className={primary}>
          {t("lists.create")}
        </button>
      </form>
      {firstLibrary && <MissingVolumes libraryId={firstLibrary} />}
    </main>
  );
}
