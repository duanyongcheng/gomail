import { useEffect, useRef } from "react";
import { useUIStore } from "@/stores/ui.store";
import { useToastStore } from "@/stores/toast.store";
import { getProfileStorageNamespace } from "@/lib/profileStorage";
import { setClosePreference } from "@/lib/platform/window";
import i18n from "@/lib/i18n";

/** Native Go events own close/restore. Bootstrap imports the UI store only after
 * resolving the profile namespace, so this sends the hydrated preference, never
 * an unscoped startup default. Until then the backend safely hides on close.
 * Focus scheduling is already handled by the native scheduler callbacks. */
export function useCloseToBackground() {
  const keepRunning = useUIStore((s) => s.keepRunningInBackground);
  const lastRequest = useRef<string | null>(null);
  const pending = useRef(Promise.resolve());

  useEffect(() => {
    const namespace = getProfileStorageNamespace();
    if (!namespace) return;
    const key = JSON.stringify([namespace, keepRunning]);
    if (lastRequest.current === key) return; // StrictMode must not send twice.
    lastRequest.current = key;
    // Bound methods run concurrently. Serialize preference changes so a slow
    // previous RPC cannot overwrite the latest user choice on the backend.
    pending.current = pending.current
      .then(() => setClosePreference(namespace, keepRunning))
      .catch(() => {
        useToastStore.getState().addToast({
          type: "error",
          message: i18n.t("settings.closePreferenceFailed", "Could not apply the window close preference. Try changing it again."),
        });
      });
  }, [keepRunning]);
}
