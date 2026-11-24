import { registerSW } from "virtual:pwa-register";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createAppRouter } from "./app/router";
import { StepUpDialog } from "./features/auth/StepUpDialog";
import "./i18n";
import "./index.css";

const queryClient = new QueryClient();
const router = createAppRouter(queryClient);

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <StepUpDialog />
    </QueryClientProvider>
  </StrictMode>,
);

// "prompt" mode: the new service worker waits; an update prompt UI comes with Slice 1.
registerSW({ immediate: true });
