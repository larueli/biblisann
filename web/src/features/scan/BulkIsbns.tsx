import { useLiveQuery } from "dexie-react-hooks";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { field, muted, primary } from "../../app/ui";
import { normalizeIsbn } from "../../lib/isbn";
import type { BiblisannDB } from "../../offline/db";
import { enqueueScan, flushOutbox, ownedLocally } from "../../offline/sync";

/** Splits pasted text into candidate ISBNs: whitespace, commas and semicolons separate them. */
export function parseIsbnList(text: string): { valid: string[]; invalid: string[] } {
  const seen = new Set<string>();
  const valid: string[] = [];
  const invalid: string[] = [];
  for (const raw of text.split(/[\s,;]+/).filter(Boolean)) {
    const isbn = normalizeIsbn(raw);
    if (!isbn) invalid.push(raw);
    else if (!seen.has(isbn)) {
      seen.add(isbn);
      valid.push(isbn);
    }
  }
  return { valid, invalid };
}

/**
 * Adds many ISBNs at once. They go through the offline outbox like scans: sent one by one, safe
 * to replay, and paced by the server's request quota (the loader says when it slows down).
 */
export function BulkIsbns({
  db,
  libraryId,
  placeId,
}: {
  db: BiblisannDB;
  libraryId: string;
  placeId?: string;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState("");
  const [summary, setSummary] = useState<{ queued: number; owned: number; invalid: number } | null>(
    null,
  );
  const [waitMs, setWaitMs] = useState(0);
  const [running, setRunning] = useState(false);
  const pending = useLiveQuery(
    () => db.outbox.where("status").anyOf("pending", "sending").count(),
    [db],
    0,
  );

  async function submit(e: FormEvent) {
    e.preventDefault();
    const { valid, invalid } = parseIsbnList(text);
    let owned = 0;
    let queued = 0;
    for (const isbn of valid) {
      if ((await ownedLocally(isbn, db)).length > 0) {
        owned++;
        continue;
      }
      await enqueueScan(libraryId, isbn, false, db, placeId);
      queued++;
    }
    setSummary({ queued, owned, invalid: invalid.length });
    setText("");
    setRunning(true);
    try {
      await flushOutbox(db, { onWait: setWaitMs });
    } catch {
      /* offline: the outbox keeps the rest and the scan page retries when the network is back */
    } finally {
      setRunning(false);
      setWaitMs(0);
    }
  }

  return (
    <section className="flex flex-col gap-2">
      <h2 className="font-semibold">{t("bulk.title")}</h2>
      <form onSubmit={submit} className="flex flex-col gap-2">
        <textarea
          className={field}
          rows={4}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t("bulk.placeholder")}
          aria-label={t("bulk.placeholder")}
        />
        <button
          type="submit"
          className={`${primary} self-start`}
          disabled={running || !text.trim()}
        >
          {t("bulk.add")}
        </button>
      </form>
      {summary && (
        <p role="status" className={muted}>
          {t("bulk.summary", summary)}
        </p>
      )}
      {(running || pending > 0) && (
        <p role="status" aria-busy="true">
          {t("bulk.remaining", { count: pending })}
          {waitMs > 0 && ` ${t("bulk.slowing", { seconds: Math.ceil(waitMs / 1000) })}`}
        </p>
      )}
    </section>
  );
}
