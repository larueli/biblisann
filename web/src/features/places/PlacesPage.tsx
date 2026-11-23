import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";

type Place = Schemas["PlaceBody"];

/** Label content: a link into the app, so any phone camera can open it and the scanner can read it. */
export function placeUrl(id: string): string {
  return `${window.location.origin}/p/${id}`;
}

function QrLabel({ place }: { place: Place }) {
  const [svg, setSvg] = useState("");
  useEffect(() => {
    void QRCode.toString(placeUrl(place.id), {
      type: "svg",
      margin: 1,
      errorCorrectionLevel: "M",
    }).then(setSvg);
  }, [place.id]);
  return (
    <figure className="inline-flex flex-col items-center gap-1 border border-slate-400 p-2 print:break-inside-avoid">
      {/* biome-ignore lint/security/noDangerouslySetInnerHtml: SVG generated locally by the qrcode library from a UUID URL */}
      <div className="h-28 w-28" dangerouslySetInnerHTML={{ __html: svg }} />
      <figcaption className="text-center text-sm font-medium">{place.name}</figcaption>
    </figure>
  );
}

function Tree({
  places,
  parent,
  depth,
  onPick,
}: {
  places: Place[];
  parent: string | null;
  depth: number;
  onPick: (p: Place) => void;
}) {
  const children = places.filter((p) => (p.parent_id ?? null) === parent);
  return (
    <ul className={depth ? "ml-4" : ""}>
      {children.map((p) => (
        <li key={p.id}>
          <button
            type="button"
            className="underline-offset-2 hover:underline"
            onClick={() => onPick(p)}
          >
            {p.name}
          </button>
          <Tree places={places} parent={p.id} depth={depth + 1} onPick={onPick} />
        </li>
      ))}
    </ul>
  );
}

export function PlacesPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const libraries = useLibraries();
  const libraryId = libraries.data?.[0]?.id ?? "";
  const places = useQuery({
    queryKey: ["places", libraryId],
    enabled: libraryId !== "",
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries/{id}/places", {
        params: { path: { id: libraryId } },
      });
      if (!data) throw new Error("places failed");
      return data;
    },
  });
  const [name, setName] = useState("");
  const [parent, setParent] = useState<string>("");
  const [selected, setSelected] = useState<Place | null>(null);
  const [scanned, setScanned] = useState("");
  const create = useMutation({
    mutationFn: async () => {
      await api.POST("/v1/libraries/{id}/places", {
        params: { path: { id: libraryId } },
        body: { name, ...(parent ? { parent_id: parent } : {}) },
      });
    },
    onSuccess: async () => {
      setName("");
      await qc.invalidateQueries({ queryKey: ["places", libraryId] });
    },
  });
  const inventory = useMutation({
    mutationFn: async (placeId: string) => {
      const isbns = scanned.split(/[\s,;]+/).filter(Boolean);
      const { data } = await api.POST("/v1/places/{id}/inventory", {
        params: { path: { id: placeId } },
        body: { isbns },
      });
      return data;
    },
  });
  return (
    <main className="flex flex-col gap-6">
      <section>
        <h2 className="text-lg font-semibold">{t("places.title")}</h2>
        {places.data && <Tree places={places.data} parent={null} depth={0} onPick={setSelected} />}
        {places.data?.length === 0 && <p className={muted}>{t("places.none")}</p>}
      </section>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <input
          className={`${field} flex-1`}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("places.name")}
          aria-label={t("places.name")}
        />
        <select
          className={field}
          value={parent}
          onChange={(e) => setParent(e.target.value)}
          aria-label={t("places.parent")}
        >
          <option value="">{t("places.noParent")}</option>
          {places.data?.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <button type="submit" className={primary}>
          {t("places.create")}
        </button>
      </form>

      {selected && (
        <section className="flex flex-col gap-3 rounded border border-slate-400 p-3">
          <h3 className="font-semibold">{selected.name}</h3>
          <QrLabel place={selected} />
          <button
            type="button"
            className={`${secondary} self-start print:hidden`}
            onClick={() => window.print()}
          >
            {t("places.print")}
          </button>
          <h4 className="font-semibold">{t("places.inventory")}</h4>
          <p className={muted}>{t("places.inventoryHelp")}</p>
          <textarea
            className={field}
            rows={3}
            value={scanned}
            onChange={(e) => setScanned(e.target.value)}
            aria-label={t("places.scannedIsbns")}
          />
          <button
            type="button"
            className={`${primary} self-start`}
            onClick={() => inventory.mutate(selected.id)}
          >
            {t("places.check")}
          </button>
          {inventory.data && (
            <div className="text-sm">
              <p>{t("places.found", { count: inventory.data.found.length })}</p>
              {inventory.data.missing.length > 0 && (
                <p className="font-semibold">
                  {t("places.missing")}: {inventory.data.missing.join("; ")}
                </p>
              )}
              {inventory.data.unexpected.length > 0 && (
                <p>
                  {t("places.unexpected")}: {inventory.data.unexpected.join(", ")}
                </p>
              )}
              {inventory.data.unscannable.length > 0 && (
                <p>
                  {t("places.unscannable")}: {inventory.data.unscannable.join("; ")}
                </p>
              )}
            </div>
          )}
        </section>
      )}
    </main>
  );
}
