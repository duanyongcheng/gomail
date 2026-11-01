import { getCurrentWindow } from "@tauri-apps/api/window";
import { useTranslation } from "react-i18next";
import iconUrl from "@/assets/app-icon.png";
import { isComposeDirty } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";
import i18n from "@/lib/i18n";

const isMac = navigator.userAgent.includes("Macintosh");

export default function TitleBar({ viewLabel }: { viewLabel?: string }) {
  const { t } = useTranslation();
  const appWindow = getCurrentWindow();

  async function handleCloseWindow() {
    if (isComposeDirty()) {
      const confirmed = await useConfirmStore.getState().confirm({
        title: i18n.t("compose.discardDraft", "Discard draft"),
        message: i18n.t("compose.discardDraftConfirm", "You have an unsaved draft. Discard and leave?"),
        destructive: true,
      });
      if (!confirmed) return;
    }
    await appWindow.close();
  }

  return (
    <div
      data-tauri-drag-region
      className={`app-titlebar${isMac ? " app-titlebar--mac" : ""}`}
    >
      <div
        data-tauri-drag-region
        className="app-titlebar-brand"
      >
        <img
          data-tauri-drag-region
          src={iconUrl}
          alt=""
          aria-hidden="true"
          draggable={false}
          className="app-titlebar-logo h-5 w-5 shrink-0 bg-transparent object-contain"
        />
        <span
          data-tauri-drag-region
          className="app-titlebar-name"
        >
          Pebble Go
        </span>
        {viewLabel && (
          <span data-tauri-drag-region className="app-titlebar-breadcrumb">
            {viewLabel}
          </span>
        )}
      </div>
      {!isMac && (
        <div className="app-titlebar-controls">
          <button
            type="button"
            onClick={() => appWindow.minimize()}
            className="app-window-button"
            aria-label={t("titleBar.minimize")}
          >
            <svg width="10" height="1" viewBox="0 0 10 1">
              <rect width="10" height="1" fill="currentColor" />
            </svg>
          </button>
          <button
            type="button"
            onClick={() => appWindow.toggleMaximize()}
            className="app-window-button"
            aria-label={t("titleBar.maximize")}
          >
            <svg width="10" height="10" viewBox="0 0 10 10">
              <rect
                width="9"
                height="9"
                x="0.5"
                y="0.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="1"
              />
            </svg>
          </button>
          <button
            type="button"
            onClick={() => void handleCloseWindow()}
            className="app-window-button app-window-button--close"
            aria-label={t("titleBar.close")}
          >
            <svg width="10" height="10" viewBox="0 0 10 10">
              <path d="M1,1 L9,9 M9,1 L1,9" stroke="currentColor" strokeWidth="1.2" />
            </svg>
          </button>
        </div>
      )}
    </div>
  );
}
