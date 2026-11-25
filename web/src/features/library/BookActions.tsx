import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage, type Schemas } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";
import { useSession } from "../auth/session";
import { CatalogAdmin } from "./CatalogAdmin";
import { canRefresh, RefreshBookButton } from "./RefreshButtons";
import { useLibraries } from "./useLibraries";

type Item = Schemas["InstanceListItem"];
const statuses = ["to_read", "reading", "read", "abandoned"] as const;
type Status = (typeof statuses)[number];

export function BookActions({ item }: { item: Item }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const state = useQuery({
    queryKey: ["work-state", item.work_id],
    queryFn: async () => {
      const { data } = await api.GET("/v1/works/{id}/me", {
        params: { path: { id: item.work_id } },
      });
      if (!data) throw new Error("state failed");
      return data;
    },
  });
  const setStatus = useMutation({
    mutationFn: async (status: Status) => {
      const { error } = await api.PUT("/v1/works/{id}/reading", {
        params: { path: { id: item.work_id } },
        body: { status },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["work-state", item.work_id] }),
  });
  const [grade, setGrade] = useState("");
  const [comment, setComment] = useState("");
  const saveGrade = useMutation({
    mutationFn: async () => {
      const { error } = await api.PUT("/v1/works/{id}/grade", {
        params: { path: { id: item.work_id } },
        body: { comment, ...(grade ? { grade: Number(grade) } : {}) },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["work-state", item.work_id] }),
  });
  const [who, setWho] = useState("");
  const lend = useMutation({
    mutationFn: async () => {
      const { error } = await api.POST("/v1/instances/{id}/lend", {
        params: { path: { id: item.id } },
        body: { counterparty: who },
      });
      if (error) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setWho("");
      await qc.invalidateQueries({ queryKey: ["loans"] });
    },
  });

  const isAdmin = useSession().data?.user.is_admin === true;
  const role = useLibraries().data?.find((l) => l.id === item.library_id)?.role;
  const current = state.data?.reading?.status ?? "";
  return (
    <details className="mt-1 text-sm">
      <summary className="cursor-pointer">{t("book.actions")}</summary>
      <div className="mt-2 flex flex-col gap-3">
        <label className="flex items-center gap-2">
          <span>{t("book.status")}</span>
          <select
            className={field}
            value={current}
            onChange={(e) => setStatus.mutate(e.target.value as Status)}
          >
            <option value="" disabled>
              —
            </option>
            {statuses.map((s) => (
              <option key={s} value={s}>
                {t(`book.statuses.${s}`)}
              </option>
            ))}
          </select>
        </label>
        <form
          className="flex flex-col gap-2"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            saveGrade.mutate();
          }}
        >
          <div className="flex items-center gap-2">
            <label htmlFor={`grade-${item.id}`}>{t("book.grade")}</label>
            <input
              id={`grade-${item.id}`}
              type="number"
              min={1}
              max={10}
              className={`${field} w-20`}
              value={grade}
              onChange={(e) => setGrade(e.target.value)}
              placeholder={String(state.data?.grade?.grade ?? "")}
            />
          </div>
          <textarea
            className={field}
            rows={2}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder={state.data?.grade?.comment || t("book.comment")}
            aria-label={t("book.comment")}
          />
          <button type="submit" className={`${secondary} self-start`}>
            {t("book.save")}
          </button>
          {state.data && state.data.external_grades.length > 0 && (
            <p className={muted}>
              {state.data.external_grades
                .map((g) => `${g.source}: ${g.grade}/${g.scale}`)
                .join(" · ")}
            </p>
          )}
        </form>
        <form
          className="flex gap-2"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            lend.mutate();
          }}
        >
          <input
            className={field}
            required
            value={who}
            onChange={(e) => setWho(e.target.value)}
            placeholder={t("book.lendTo")}
            aria-label={t("book.lendTo")}
          />
          <button type="submit" className={primary}>
            {t("book.lend")}
          </button>
        </form>
        {isAdmin ? (
          <CatalogAdmin editionId={item.edition_id} />
        ) : (
          canRefresh(role) && <RefreshBookButton editionId={item.edition_id} />
        )}
        {lend.isError && <p role="alert">{lend.error.message}</p>}
        {lend.isSuccess && <p role="status">{t("book.lent")}</p>}
      </div>
    </details>
  );
}
