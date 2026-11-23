import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Link,
  Navigate,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, setCsrfToken } from "../api/client";
import { LoginPage } from "../features/auth/LoginPage";
import { LibraryPage } from "../features/library/LibraryPage";
import { useLibraries } from "../features/library/useLibraries";
import { ListsPage } from "../features/lists/ListsPage";
import { LoansPage } from "../features/loans/LoansPage";
import { PlacesPage } from "../features/places/PlacesPage";
import { ScanPage } from "../features/scan/ScanPage";
import { SettingsPage } from "../features/settings/SettingsPage";
import { Shell } from "./Nav";

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
  component: inShell(LibraryPage),
});
function inShell(Page: () => React.JSX.Element) {
  return function Wrapped() {
    return (
      <Shell>
        <Page />
      </Shell>
    );
  };
}

const guarded = <P extends string>(path: P, component: () => React.JSX.Element) =>
  createRoute({
    getParentRoute: () => rootRoute,
    path,
    beforeLoad: ({ context }) => requireSession(context.queryClient),
    component,
  });
const listsRoute = guarded("/lists", inShell(ListsPage));
const loansRoute = guarded("/loans", inShell(LoansPage));
const placesRoute = guarded("/places", inShell(PlacesPage));
const settingsRoute = guarded("/settings", inShell(SettingsPage));

const scanRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/scan/$libraryId",
  validateSearch: (s: Record<string, unknown>): { place?: string } =>
    typeof s.place === "string" ? { place: s.place } : {},
  beforeLoad: ({ context }) => requireSession(context.queryClient),
  component: ScanPage,
});

// Opening a place label with the phone camera lands here: go on to scanning in that place.
function PlaceRedirect() {
  const { placeId } = placeRedirectRoute.useParams();
  const libraries = useLibraries();
  const first = libraries.data?.[0]?.id;
  if (first)
    return (
      <Navigate to="/scan/$libraryId" params={{ libraryId: first }} search={{ place: placeId }} />
    );
  return <p aria-busy="true" />;
}
const placeRedirectRoute = guarded("/p/$placeId", PlaceRedirect);

const routeTree = rootRoute.addChildren([
  loginRoute,
  indexRoute,
  scanRoute,
  listsRoute,
  loansRoute,
  placesRoute,
  settingsRoute,
]);

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient } });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
