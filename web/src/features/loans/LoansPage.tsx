import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { useLibraries } from "../library/useLibraries";

export function LoansPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const libraries = useLibraries();
  const loans = useQuery({
    queryKey: ["loans"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/loans", { params: { query: {} } });
      if (!data) throw new Error("loans failed");
      return data;
    },
  });
  const [title, setTitle] = useState("");
  const [who, setWho] = useState("");
  const [due, setDue] = useState("");
  const libraryId = libraries.data?.[0]?.id;
  const borrow = useMutation({
    mutationFn: async () => {
      if (!libraryId) return;
      const { error } = await api.POST("/v1/libraries/{id}/borrowed", {
        params: { path: { id: libraryId } },
        body: { title, counterparty: who, ...(due ? { due_on: due } : {}) },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setTitle("");
      setWho("");
      setDue("");
      await qc.invalidateQueries({ queryKey: ["loans"] });
    },
  });
  const giveBack = useMutation({
    mutationFn: async (id: string) => {
      await api.POST("/v1/loans/{id}/return", { params: { path: { id } }, body: {} });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["loans"] }),
  });
  const today = new Date().toISOString().slice(0, 10);
  return (
    <main className="flex flex-col gap-6">
      <section>
        <h2 className="text-lg font-semibold">{t("loans.open")}</h2>
        {loans.data?.length === 0 && <p className={muted}>{t("loans.none")}</p>}
        <ul className="mt-2 flex flex-col divide-y divide-slate-300 dark:divide-slate-700">
          {loans.data?.map((l) => (
            <li key={l.id} className="flex items-center justify-between gap-2 py-2">
              <div>
                <p className="font-medium">{l.title}</p>
                <p className={muted}>
                  {t(l.direction === "lent" ? "loans.lentTo" : "loans.borrowedFrom", {
                    name: l.counterparty,
                  })}
                  {l.due_on && (
                    <span
                      className={
                        l.due_on < today
                          ? "ml-2 font-semibold text-red-700 dark:text-red-400"
                          : "ml-2"
                      }
                    >
                      {t("loans.due", { date: l.due_on })}
                    </span>
                  )}
                </p>
              </div>
              <button type="button" className={secondary} onClick={() => giveBack.mutate(l.id)}>
                {t("loans.returned")}
              </button>
            </li>
          ))}
        </ul>
      </section>
      <form
        className="flex flex-col gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          borrow.mutate();
        }}
      >
        <h2 className="text-lg font-semibold">{t("loans.borrowTitle")}</h2>
        <input
          className={field}
          required
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={t("loans.bookTitle")}
          aria-label={t("loans.bookTitle")}
        />
        <input
          className={field}
          required
          value={who}
          onChange={(e) => setWho(e.target.value)}
          placeholder={t("loans.from")}
          aria-label={t("loans.from")}
        />
        <label className="flex items-center gap-2">
          <span>{t("loans.dueDate")}</span>
          <input
            type="date"
            className={field}
            value={due}
            onChange={(e) => setDue(e.target.value)}
          />
        </label>
        <button type="submit" className={primary} disabled={!libraryId}>
          {t("loans.record")}
        </button>
        {borrow.isError && <p role="alert">{borrow.error.message}</p>}
      </form>
      <p className={muted}>{t("loans.lendHint")}</p>
    </main>
  );
}
