import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BookActions } from "./BookActions";
import { canRefresh, RefreshLibraryButton } from "./RefreshButtons";
import { useCreateLibrary, useInstances, useLibraries } from "./useLibraries";

function Shelf({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const role = useLibraries().data?.find((l) => l.id === libraryId)?.role;
  const [q, setQ] = useState("");
  const instances = useInstances(libraryId, q);
  return (
    <section className="flex flex-col gap-3">
      <div className="flex gap-2">
        <input
          type="search"
          className="w-full rounded border border-slate-400 bg-transparent px-3 py-2"
          placeholder={t("library.search")}
          aria-label={t("library.search")}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <Link
          to="/scan/$libraryId"
          params={{ libraryId }}
          className="whitespace-nowrap rounded bg-slate-800 px-4 py-2 text-white dark:bg-slate-200 dark:text-slate-900"
        >
          {t("library.scan")}
        </Link>
      </div>
      {canRefresh(role) && <RefreshLibraryButton libraryId={libraryId} />}
      {instances.isPending && <p>{t("common.loading")}</p>}
      {instances.data?.length === 0 && <p>{t(q ? "library.noResults" : "library.empty")}</p>}
      <ul className="flex flex-col divide-y divide-slate-300 dark:divide-slate-700">
        {instances.data?.map((i) => (
          <li key={i.id} className="flex gap-3 py-2">
            {i.cover_id ? (
              <img
                src={`/v1/covers/${i.cover_id}?thumb=1`}
                alt=""
                loading="lazy"
                className="h-16 w-11 rounded object-cover"
              />
            ) : (
              <div
                className="h-16 w-11 rounded bg-slate-300 dark:bg-slate-700"
                aria-hidden="true"
              />
            )}
            <div>
              <p className="font-medium">{i.title}</p>
              <p className="text-sm text-slate-600 dark:text-slate-300">{i.authors.join(", ")}</p>
              {i.isbn13 && <p className="text-xs text-slate-500">{i.isbn13}</p>}
              <BookActions item={i} />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function LibraryPage() {
  const { t } = useTranslation();
  const libraries = useLibraries();
  const create = useCreateLibrary();
  const [selected, setSelected] = useState<string | null>(null);
  const [newName, setNewName] = useState("");

  const current = selected ?? libraries.data?.[0]?.id ?? null;
  return (
    <main className="flex flex-col gap-4">
      {libraries.data && libraries.data.length > 0 && (
        <select
          aria-label={t("library.choose")}
          className="rounded border border-slate-400 bg-transparent px-2 py-2"
          value={current ?? ""}
          onChange={(e) => setSelected(e.target.value)}
        >
          {libraries.data.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
      )}

      {libraries.data?.length === 0 && (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(newName);
          }}
        >
          <p>{t("library.firstRun")}</p>
          <input
            className="rounded border border-slate-400 bg-transparent px-3 py-2"
            required
            placeholder={t("library.namePlaceholder")}
            aria-label={t("library.name")}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
          />
          <button
            type="submit"
            className="rounded bg-slate-800 px-4 py-2 text-white dark:bg-slate-200 dark:text-slate-900"
          >
            {t("library.create")}
          </button>
          {create.isError && <p role="alert">{create.error.message}</p>}
        </form>
      )}

      {current && <Shelf key={current} libraryId={current} />}
    </main>
  );
}
