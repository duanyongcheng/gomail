import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";
import { initializeProfileStorageNamespace } from "@/lib/profileStorage";
import { logStartupTiming } from "@/lib/startupTiming";
import "./styles/index.css";

async function bootstrap() {
  logStartupTiming("frontend entry loaded");
  await initializeProfileStorageNamespace();
  await import("@/lib/i18n");
  // Go creates the only window visible. A late frontend Show would undo a
  // user's close-to-background during asynchronous profile/App bootstrap.
  const { default: App } = await import("./App");

  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </StrictMode>,
  );
}

void bootstrap();
