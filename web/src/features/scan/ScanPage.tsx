import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useLiveQuery } from "dexie-react-hooks";
import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { normalizeIsbn } from "../../lib/isbn";
import { dbForUser } from "../../offline/db";
import {
  enqueueScan,
  flushOutbox,
  ownedLocally,
  reconcileLibraries,
  syncIndex,
} from "../../offline/sync";
import { useSession } from "../auth/session";
import { BulkIsbns } from "./BulkIsbns";
import { createDetector } from "./scanner";

export function ScanPage() {
  const userId = useSession().data?.user.id;
  return userId ? <UserScanPage key={userId} userId={userId} /> : null;
}

function UserScanPage({ userId }: { userId: string }) {
  const db = useMemo(() => dbForUser(userId), [userId]);
  const { t } = useTranslation();
  const { libraryId } = useParams({ strict: false }) as { libraryId: string };
  const search = useSearch({ strict: false }) as { place?: string };
  const [placeId, setPlaceId] = useState<string | undefined>(search.place);
  const videoRef = useRef<HTMLVideoElement>(null);
  const [manual, setManual] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [cameraError, setCameraError] = useState(false);
  const lastSeen = useRef<{ code: string; at: number }>({ code: "", at: 0 });
  const outbox = useLiveQuery(
    () => db.outbox.orderBy("createdAt").reverse().limit(30).toArray(),
    [db],
  );

  const sync = useCallback(async () => {
    try {
      await reconcileLibraries(db);
      await flushOutbox(db);
      await syncIndex(libraryId, db);
    } catch {
      /* offline: retried on the next trigger */
    }
  }, [db, libraryId]);

  const handleCode = useCallback(
    async (raw: string) => {
      // A place label (QR code) sets the current place for the books scanned next.
      const label = /\/p\/([0-9a-f-]{36})$/i.exec(raw.trim());
      if (label?.[1]) {
        setPlaceId(label[1]);
        setMessage(t("scan.placeSet"));
        return;
      }
      const isbn = normalizeIsbn(raw);
      if (!isbn) {
        setMessage(t("scan.notIsbn"));
        return;
      }
      const owned = await ownedLocally(isbn, db);
      if (owned.length > 0) {
        setMessage(t("scan.alreadyOwned", { title: owned[0]?.title ?? isbn }));
        return;
      }
      await enqueueScan(libraryId, isbn, false, db, placeId);
      setMessage(t("scan.queued", { isbn }));
      void sync();
    },
    [db, libraryId, placeId, sync, t],
  );

  useEffect(() => {
    void sync();
    window.addEventListener("online", sync);
    return () => window.removeEventListener("online", sync);
  }, [sync]);

  useEffect(() => {
    let stopped = false;
    let stream: MediaStream | null = null;
    const detector = createDetector();
    async function start() {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: "environment" },
        });
      } catch {
        setCameraError(true);
        return;
      }
      const video = videoRef.current;
      if (!video || stopped) return;
      video.srcObject = stream;
      await video.play();
      const tick = async () => {
        if (stopped) return;
        try {
          const codes = await detector.detect(video);
          const code = codes[0];
          const now = Date.now();
          // The same barcode is reported many times per second: act once per 4 seconds.
          if (code && (code !== lastSeen.current.code || now - lastSeen.current.at > 4000)) {
            lastSeen.current = { code, at: now };
            await handleCode(code);
          }
        } catch {
          /* a frame failed to decode: try the next one */
        }
        setTimeout(tick, 250);
      };
      void tick();
    }
    void start();
    return () => {
      stopped = true;
      for (const track of stream?.getTracks() ?? []) track.stop();
    };
  }, [handleCode]);

  function submitManual(e: FormEvent) {
    e.preventDefault();
    void handleCode(manual);
    setManual("");
  }

  return (
    <main className="mx-auto flex min-h-dvh max-w-xl flex-col gap-4 p-4">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">{t("scan.title")}</h1>
        <Link to="/" className="underline">
          {t("scan.back")}
        </Link>
      </header>
      {cameraError ? (
        <p role="alert">{t("scan.cameraError")}</p>
      ) : (
        <video
          ref={videoRef}
          playsInline
          muted
          className="aspect-video w-full rounded bg-black object-cover"
          aria-label={t("scan.cameraLabel")}
        />
      )}
      <form onSubmit={submitManual} className="flex gap-2">
        <input
          className="w-full rounded border border-slate-400 bg-transparent px-3 py-2"
          inputMode="numeric"
          placeholder={t("scan.manualPlaceholder")}
          aria-label={t("scan.manualPlaceholder")}
          value={manual}
          onChange={(e) => setManual(e.target.value)}
        />
        <button
          type="submit"
          className="rounded bg-slate-800 px-4 py-2 text-white dark:bg-slate-200 dark:text-slate-900"
        >
          {t("scan.add")}
        </button>
      </form>
      {message && (
        <p role="status" className="rounded bg-slate-200 p-2 dark:bg-slate-800">
          {message}
        </p>
      )}
      <BulkIsbns db={db} libraryId={libraryId} {...(placeId ? { placeId } : {})} />
      <section>
        <h2 className="font-semibold">{t("scan.outbox")}</h2>
        <ul className="mt-2 flex flex-col gap-1 text-sm">
          {outbox?.map((o) => (
            <li key={o.key} className="flex justify-between gap-2">
              <span>{o.title ?? o.isbn13}</span>
              <span>{t(`scan.status.${o.status}`)}</span>
            </li>
          ))}
        </ul>
      </section>
    </main>
  );
}
