import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage, type Schemas } from "../../api/client";
import { field, muted, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";
import { spine } from "./spine";

type Item = Schemas["InstanceListItem"];
type Place = Schemas["PlaceBody"];

function useInstances(libraryId: string, placeId: string | null) {
  return useQuery({
    queryKey: ["shelf", libraryId, placeId],
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries/{id}/instances", {
        params: {
          path: { id: libraryId },
          query: placeId ? { place_id: placeId, limit: 200 } : { unplaced: true, limit: 200 },
        },
      });
      return data?.items ?? [];
    },
  });
}

/** One board of a shelf: the books standing on it, as spines. It accepts dropped books. */
function Board({
  libraryId,
  placeId,
  label,
  onSelect,
  onDropBook,
  selected,
}: {
  libraryId: string;
  placeId: string | null;
  label: string;
  onSelect: (i: Item) => void;
  onDropBook: (instanceId: string, placeId: string | null) => void;
  selected: string | null;
}) {
  const { t } = useTranslation();
  const items = useInstances(libraryId, placeId);
  return (
    <section
      aria-label={label}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        const id = e.dataTransfer.getData("text/plain");
        if (id) onDropBook(id, placeId);
      }}
    >
      <h3 className={`${muted} mb-1`}>{label}</h3>
      <div className="flex min-h-36 items-end gap-px overflow-x-auto border-b-[10px] border-amber-800 px-2 dark:border-amber-900">
        {items.data?.map((i) => {
          const s = spine(i.id, i.title, i.pages);
          return (
            <button
              key={i.id}
              type="button"
              draggable
              onDragStart={(e) => e.dataTransfer.setData("text/plain", i.id)}
              onClick={() => onSelect(i)}
              aria-label={i.title}
              aria-pressed={selected === i.id}
              title={`${i.title}${i.authors.length ? ` — ${i.authors.join(", ")}` : ""}`}
              style={{ width: s.width, height: s.height, background: `hsl(${s.hue} 45% 36%)` }}
              className={`shrink-0 overflow-hidden rounded-t-sm border-x border-black/30 text-[11px] leading-none text-white ${
                selected === i.id ? "outline outline-2 outline-offset-1 outline-sky-400" : ""
              }`}
            >
              <span
                className="block max-h-full overflow-hidden whitespace-nowrap p-1"
                style={{ writingMode: "vertical-rl" }}
              >
                {i.title}
              </span>
            </button>
          );
        })}
        {items.data?.length === 0 && (
          <span className={`${muted} self-center p-2`}>{t("shelves.empty")}</span>
        )}
      </div>
    </section>
  );
}

export function ShelvesPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const libraries = useLibraries();
  const libraryId = libraries.data?.[0]?.id ?? "";
  const places = useQuery({
    queryKey: ["places", libraryId],
    enabled: libraryId !== "",
    queryFn: async () =>
      (await api.GET("/v1/libraries/{id}/places", { params: { path: { id: libraryId } } })).data ??
      [],
  });
  const [placeId, setPlaceId] = useState<string>("");
  const [selected, setSelected] = useState<Item | null>(null);
  const all: Place[] = places.data ?? [];
  const current = all.find((p) => p.id === placeId) ?? all.find((p) => !p.parent_id);
  const layers = current ? all.filter((p) => p.parent_id === current.id) : [];

  const move = useMutation({
    mutationFn: async ({ item, to }: { item: Item; to: string | null }) => {
      const { error, response } = await api.PUT("/v1/instances/{id}", {
        params: { path: { id: item.id } },
        body: {
          edition_id: item.edition_id,
          kind: item.kind,
          condition: item.condition,
          notes: item.notes,
          ...(to ? { place_id: to } : {}),
          ...(item.acquired_on ? { acquired_on: item.acquired_on } : {}),
        },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setSelected(null);
      await qc.invalidateQueries({ queryKey: ["shelf", libraryId] });
    },
  });
  const byId = new Map<string, Item>();
  for (const q of qc.getQueriesData<Item[]>({ queryKey: ["shelf", libraryId] }))
    for (const i of q[1] ?? []) byId.set(i.id, i);
  const drop = (instanceId: string, to: string | null) => {
    const item = byId.get(instanceId);
    if (item && item.place_id !== (to ?? undefined)) move.mutate({ item, to });
  };
  const placeName = (id: string | undefined) =>
    all.find((p) => p.id === id)?.name ?? t("shelves.unplaced");

  return (
    <main className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">{t("shelves.title")}</h2>
      {all.length === 0 && <p className={muted}>{t("shelves.noPlaces")}</p>}
      {all.length > 0 && (
        <label className="flex items-center gap-2">
          <span>{t("shelves.choose")}</span>
          <select
            className={field}
            value={current?.id ?? ""}
            onChange={(e) => setPlaceId(e.target.value)}
          >
            {all.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <p className={muted}>{t("shelves.hint")}</p>
      {libraryId && current && (
        <div className="flex flex-col gap-4">
          {layers.map((l) => (
            <Board
              key={l.id}
              libraryId={libraryId}
              placeId={l.id}
              label={l.name}
              onSelect={setSelected}
              onDropBook={drop}
              selected={selected?.id ?? null}
            />
          ))}
          <Board
            libraryId={libraryId}
            placeId={current.id}
            label={layers.length > 0 ? t("shelves.onTop", { name: current.name }) : current.name}
            onSelect={setSelected}
            onDropBook={drop}
            selected={selected?.id ?? null}
          />
        </div>
      )}
      {libraryId && (
        <Board
          libraryId={libraryId}
          placeId={null}
          label={t("shelves.unplaced")}
          onSelect={setSelected}
          onDropBook={drop}
          selected={selected?.id ?? null}
        />
      )}
      {selected && (
        <section
          className="flex flex-col gap-2 rounded border border-slate-400 p-3"
          aria-live="polite"
        >
          <h3 className="font-semibold">{selected.title}</h3>
          <p className={muted}>
            {selected.authors.join(", ")} · {placeName(selected.place_id)}
          </p>
          <label className="flex items-center gap-2">
            <span>{t("shelves.moveTo")}</span>
            <select
              className={field}
              value={selected.place_id ?? ""}
              onChange={(e) => move.mutate({ item: selected, to: e.target.value || null })}
            >
              <option value="">{t("shelves.unplaced")}</option>
              {all.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className={`${secondary} self-start`}
            onClick={() => setSelected(null)}
          >
            {t("shelves.close")}
          </button>
        </section>
      )}
      {move.isError && <p role="alert">{move.error.message}</p>}
    </main>
  );
}
