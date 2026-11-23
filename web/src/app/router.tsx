import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Link,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, setCsrfToken } from "../api/client";
import { LoginPage } from "../features/auth/LoginPage";
import { LibraryPage } from "../features/library/LibraryPage";
import { ScanPage } from "../features/scan/ScanPage";

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

interface RouterContext {
  queryClient: QueryClient;
}

async function requireSession(queryClient: QueryClient) {
  const session = await queryClient.fetchQuery({
    queryKey: ["session"],
    queryFn: async () => {
      const { data, response } = await api.GET("/v1/auth/session");
      if (response.status === 401 || !data) return null;
      setCsrfToken(data.csrf_token);
      return data;
    },
    staleTime: 5 * 60 * 1000,
  });
  if (!session) throw redirect({ to: "/login" });
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
});
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
});
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: ({ context }) => requireSession(context.queryClient),
  component: LibraryPage,
});
const scanRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/scan/$libraryId",
  beforeLoad: ({ context }) => requireSession(context.queryClient),
  component: ScanPage,
});

const routeTree = rootRoute.addChildren([loginRoute, indexRoute, scanRoute]);

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient } });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
