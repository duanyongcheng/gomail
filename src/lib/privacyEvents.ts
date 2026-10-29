import { listen } from "@tauri-apps/api/event";

export const PRIVACY_SETTINGS_CHANGED = "pebble:privacy-settings-changed";
export interface PrivacyChange { accountId?: string; email?: string; settings?: boolean }

// Register before requesting rendered content. Callers invalidate their request
// generation synchronously here so old permissive HTML cannot win a response race.
export function subscribePrivacyChanges(callback: (change: PrivacyChange) => void): () => void {
  let active = true;
  const unlisten = listen<PrivacyChange>("mail:trusted-senders-changed", ({ payload }) => { if (active) callback(payload); })
    .catch(() => { console.warn("Privacy change listener unavailable"); return () => {}; });
  const settings = () => { if (active) callback({ settings: true }); };
  window.addEventListener(PRIVACY_SETTINGS_CHANGED, settings);
  return () => {
    active = false;
    window.removeEventListener(PRIVACY_SETTINGS_CHANGED, settings);
    void unlisten.then((off) => off()).catch(() => {});
  };
}
