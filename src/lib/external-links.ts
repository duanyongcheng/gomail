import { invoke } from "@tauri-apps/api/core";
import i18n from "@/lib/i18n";
import { useToastStore } from "@/stores/toast.store";

/** Only a deliberate link action calls this. The Go service is the validation
 * boundary. Never show or log the URL or a raw bridge/OS error (query tokens and
 * message data can appear in either). */
export async function openExternalLink(url: string): Promise<void> {
  try {
    await invoke("open_external_url", { url });
  } catch {
    useToastStore.getState().addToast({
      type: "error",
      message: i18n.t("common.externalLinkFailed", "Could not open this link. Only valid HTTP(S) links without credentials can be opened in your default browser."),
    });
  }
}
