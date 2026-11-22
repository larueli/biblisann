import { createRootRoute, createRoute, createRouter, Link, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { HomePage } from "../features/home/HomePage";

function NotFound() {
  const { t } = useTranslation();
  return (
    <main className="mx-auto max-w-xl p-6">
      <h1 className="text-2xl font-bold">{t("notFound.title")}</h1>
      <Link to="/" className="mt-4 inline-block underline">
        {t("notFound.back")}
      </Link>
    </main>
  );
}

const rootRoute = createRootRoute({ component: Outlet, notFoundComponent: NotFound });
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: HomePage });

export const router = createRouter({ routeTree: rootRoute.addChildren([indexRoute]) });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
