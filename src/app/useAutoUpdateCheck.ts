import { useEffect } from "react";
import { listen } from "@tauri-apps/api/event";
import type { UpdateProgress, UpdateStatus } from "@/mygo";
import { useUIStore } from "@/stores/ui.store";
import {
  AUTO_UPDATE_CHECK_COOLDOWN_MS,
  useUpdateStore,
} from "@/stores/update.store";

const INITIAL_CHECK_DELAY_MS = 12_000;
const PERIODIC_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;

/**
 * Keeps the frontend update store synced with Go updater events, responds to
 * native menu "Check for Updates…" actions, and performs throttled background
 * update checks when automatic checks are enabled and the app is online.
 */
export function useAutoUpdateCheck() {
  const autoCheckUpdates = useUpdateStore((s) => s.autoCheckUpdates);
  const networkStatus = useUIStore((s) => s.networkStatus);

  useEffect(() => {
    void useUpdateStore.getState().hydrateStatus();

    const unlistenStatus = listen<UpdateStatus>("app:update-status", (event) => {
      useUpdateStore.getState().applyServerStatus(event.payload);
    });
    const unlistenProgress = listen<UpdateProgress>("app:update-progress", (event) => {
      useUpdateStore.getState().applyProgress(event.payload);
    });
    const unlistenOpen = listen("app:open-updates", () => {
      const ui = useUIStore.getState();
      ui.setSettingsTab("about");
      ui.setActiveView("settings");
      void useUpdateStore.getState().checkForUpdates({ manual: true });
    });

    return () => {
      void unlistenStatus.then((off) => off());
      void unlistenProgress.then((off) => off());
      void unlistenOpen.then((off) => off());
    };
  }, []);

  useEffect(() => {
    if (!autoCheckUpdates || networkStatus !== "online") return;

    function maybeCheckInBackground() {
      const store = useUpdateStore.getState();
      if (!store.autoCheckUpdates || useUIStore.getState().networkStatus !== "online") return;
      if (store.state === "downloading" || store.state === "installed" || store.state === "checking") return;
      if (store.lastCheckedAt && Date.now() - store.lastCheckedAt < AUTO_UPDATE_CHECK_COOLDOWN_MS) return;
      void store.checkForUpdates({ manual: false });
    }

    const startupTimer = window.setTimeout(maybeCheckInBackground, INITIAL_CHECK_DELAY_MS);
    const intervalTimer = window.setInterval(maybeCheckInBackground, PERIODIC_CHECK_INTERVAL_MS);

    return () => {
      window.clearTimeout(startupTimer);
      window.clearInterval(intervalTimer);
    };
  }, [autoCheckUpdates, networkStatus]);
}
