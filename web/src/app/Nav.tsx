import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useAuthActions, useSession } from "../features/auth/session";
import { LanguageSwitcher } from "../features/home/LanguageSwitcher";
import { Footer } from "./Footer";
import { muted } from "./ui";

export function Shell({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  const session = useSession();
  const { logout } = useAuthActions();
  const link =
    "px-2 py-1 underline-offset-4 hover:underline [&.active]:font-semibold [&.active]:underline";
  return (
    <div className="mx-auto flex min-h-dvh max-w-2xl flex-col gap-4 p-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <Link to="/" className="text-2xl font-bold">
          {t("app.name")}
        </Link>
        <div className="flex items-center gap-3">
          <LanguageSwitcher />
          <span className={muted}>{session.data?.user.email}</span>
          <button type="button" className="text-sm underline" onClick={() => logout.mutate()}>
            {t("auth.signOut")}
          </button>
        </div>
      </header>
      <nav
        aria-label={t("nav.label")}
        className="flex flex-wrap gap-1 border-b border-slate-300 pb-2 dark:border-slate-700"
      >
        <Link to="/" className={link}>
          {t("nav.library")}
        </Link>
        <Link to="/lists" className={link}>
          {t("nav.lists")}
        </Link>
        <Link to="/loans" className={link}>
          {t("nav.loans")}
        </Link>
        <Link to="/places" className={link}>
          {t("nav.places")}
        </Link>
        <Link to="/settings" className={link}>
          {t("nav.settings")}
        </Link>
      </nav>
      {children}
      <Footer />
    </div>
  );
}
