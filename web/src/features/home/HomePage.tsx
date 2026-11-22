import { useTranslation } from "react-i18next";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { useServerVersion } from "./useServerVersion";

export function HomePage() {
  const { t } = useTranslation();
  const version = useServerVersion();

  let status = t("home.serverLoading");
  if (version.isError) status = t("home.serverError");
  if (version.data) status = t("home.server", { version: version.data });

  return (
    <main className="mx-auto flex min-h-dvh max-w-xl flex-col gap-6 p-6">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold">{t("app.name")}</h1>
          <p className="text-slate-600 dark:text-slate-300">{t("app.tagline")}</p>
        </div>
        <LanguageSwitcher />
      </header>
      <section>
        <h2 className="text-xl font-semibold">{t("home.welcome")}</h2>
        <p className="mt-2">{t("home.intro")}</p>
      </section>
      <p role="status" className="text-sm text-slate-600 dark:text-slate-300">
        {status}
      </p>
    </main>
  );
}
