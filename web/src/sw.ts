/// <reference lib="webworker" />
import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
} from "workbox-precaching";
import { NavigationRoute, registerRoute } from "workbox-routing";

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ url: string; revision: string | null }>;
};

// App shell: precached at build time (the list is injected), served offline.
precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

// Server routes must never be answered with the app shell.
registerRoute(
  new NavigationRoute(createHandlerBoundToURL("/index.html"), {
    denylist: [
      /^\/v1\//,
      /^\/openapi/,
      /^\/docs/,
      /^\/mcp/,
      /^\/oauth\/(token|register)/,
      /^\/\.well-known\//,
      /^\/scim\//,
      /^\/opds\//,
      /^\/(healthz|livez|readyz|startupz|health|metrics)$/,
    ],
  }),
);

self.addEventListener("message", (event) => {
  if (event.data?.type === "SKIP_WAITING") void self.skipWaiting();
});

interface PushPayload {
  title?: string;
  body?: string;
  event?: string;
}

// Web Push: the server sends {title, body, event}. Loan reminders open the loans page.
self.addEventListener("push", (event) => {
  let payload: PushPayload = {};
  try {
    payload = (event.data?.json() ?? {}) as PushPayload;
  } catch {
    payload = { body: event.data?.text() ?? "" };
  }
  const url = payload.event?.startsWith("loan.") ? "/loans" : "/";
  event.waitUntil(
    self.registration.showNotification(payload.title || "Biblisann", {
      body: payload.body ?? "",
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      tag: payload.event ?? "biblisann",
      data: { url },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = (event.notification.data as { url?: string } | undefined)?.url ?? "/";
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
      for (const w of windows) {
        if (new URL(w.url).origin === self.location.origin && "focus" in w) {
          void w.navigate(url);
          return w.focus();
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});
