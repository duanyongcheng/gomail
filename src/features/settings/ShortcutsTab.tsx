import { useCallback, useEffect, useState } from "react";
import { Keyboard, RotateCcw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { isActionAvailable } from "@/lib/availability";
import { useShortcutStore } from "@/stores/shortcut.store";
import { eventToKeyString } from "@/hooks/useKeyboard";

const ACTION_I18N_MAP: Record<string, string> = {
  "command-palette": "shortcuts.openCommandPalette",
  "close-modal": "shortcuts.closeModal",
  "next-message": "shortcuts.nextMessage",
  "prev-message": "shortcuts.prevMessage",
  "open-message": "shortcuts.openMessage",
  "toggle-star": "shortcuts.toggleStar",
  "archive-message": "shortcuts.archiveMessage",
  "toggle-view-inbox": "shortcuts.toggleView",
  "toggle-view-kanban": "shortcuts.moveToKanban",
  "compose-new": "shortcuts.composeNew",
  "reply": "shortcuts.reply",
  "reply-all": "shortcuts.replyAll",
  "forward": "shortcuts.forward",
  "open-search": "shortcuts.openSearch",
  "focus-search": "shortcuts.focusSearch",
  "open-cloud-settings": "shortcuts.openCloudSettings",
  "toggle-notifications": "shortcuts.toggleNotifications",
  "translate-selection": "shortcuts.translateSelection",
  "toggle-bilingual": "shortcuts.toggleBilingual",
  "summarize-message": "shortcuts.summarizeMessage",
};

const SHORTCUT_GROUPS = [
  { categoryKey: "shortcuts.general", actions: ["command-palette", "close-modal", "open-cloud-settings", "toggle-notifications"] },
  { categoryKey: "shortcuts.navigation", actions: ["next-message", "prev-message", "open-message", "open-search", "focus-search"] },
  { categoryKey: "shortcuts.mailActions", actions: ["compose-new", "reply", "reply-all", "forward", "toggle-star", "archive-message", "toggle-view-inbox", "toggle-view-kanban"] },
  { categoryKey: "shortcuts.translate", actions: ["translate-selection", "toggle-bilingual"] },
  { categoryKey: "shortcuts.ai", actions: ["summarize-message"] },
];

function ShortcutRow({ actionId }: { actionId: string }) {
  const { t } = useTranslation();
  const bindings = useShortcutStore((s) => s.bindings);
  const recording = useShortcutStore((s) => s.recording);
  const startRecording = useShortcutStore((s) => s.startRecording);
  const stopRecording = useShortcutStore((s) => s.stopRecording);
  const updateShortcut = useShortcutStore((s) => s.updateShortcut);
  const detectConflict = useShortcutStore((s) => s.detectConflict);

  const [conflict, setConflict] = useState<string | null>(null);
  const isRecording = recording === actionId;
  const currentKeys = bindings[actionId] ?? "";

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!isRecording) return;
      e.preventDefault();
      e.stopPropagation();

      // Allow Escape to cancel recording
      if (e.key === "Escape") {
        stopRecording();
        setConflict(null);
        return;
      }

      // Ignore bare modifier presses
      if (["Control", "Meta", "Shift", "Alt"].includes(e.key)) return;

      const keyString = eventToKeyString(e);
      const conflicting = detectConflict(keyString, actionId);

      if (conflicting) {
        const conflictLabel = t(ACTION_I18N_MAP[conflicting] ?? conflicting);
        setConflict(conflictLabel);
        return;
      }

      setConflict(null);
      updateShortcut(actionId, keyString);
    },
    [isRecording, actionId, stopRecording, detectConflict, updateShortcut, t],
  );

  useEffect(() => {
    if (isRecording) {
      document.addEventListener("keydown", handleKeyDown, true);
      return () => document.removeEventListener("keydown", handleKeyDown, true);
    }
  }, [isRecording, handleKeyDown]);

  // Close recording on outside click
  useEffect(() => {
    if (!isRecording) return;
    const handleClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest(`[data-action="${actionId}"]`)) {
        stopRecording();
        setConflict(null);
      }
    };
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [isRecording, actionId, stopRecording]);

  return (
    <div data-action={actionId}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "10px 16px",
          backgroundColor: "var(--color-bg)",
        }}
      >
        <span
          id={`shortcut-label-${actionId}`}
          style={{ fontSize: "13px", color: "var(--color-text-primary)" }}
        >
          {t(ACTION_I18N_MAP[actionId] ?? actionId)}
        </span>
        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
          <span
            role="status"
            aria-live="polite"
            style={{
              fontSize: "11px",
              color: "var(--color-error)",
              minWidth: conflict ? undefined : 0,
            }}
          >
            {conflict ? t("shortcuts.conflict", { action: conflict }) : ""}
          </span>
          <button
            type="button"
            aria-pressed={isRecording}
            aria-labelledby={`shortcut-label-${actionId}`}
            aria-label={
              isRecording
                ? t("shortcuts.recording")
                : t("shortcuts.editBinding", { keys: currentKeys || t("shortcuts.unbound", "Unbound") })
            }
            onClick={() => {
              if (isRecording) {
                stopRecording();
                setConflict(null);
              } else {
                setConflict(null);
                startRecording(actionId);
              }
            }}
            title={t("shortcuts.edit")}
            style={{
              padding: "3px 9px",
              borderRadius: "var(--ui-radius-sm)",
              border: isRecording
                ? "1px solid var(--color-accent)"
                : "1px solid var(--color-border)",
              backgroundColor: isRecording
                ? "var(--color-accent-soft)"
                : "var(--color-bg-secondary)",
              fontSize: "12px",
              fontFamily: "var(--font-mono)",
              color: isRecording
                ? "var(--color-accent)"
                : "var(--color-text-secondary)",
              cursor: "pointer",
              minWidth: "60px",
              textAlign: "center",
              userSelect: "none",
            }}
          >
            {isRecording ? t("shortcuts.recording") : currentKeys}
          </button>
        </div>
      </div>
    </div>
  );
}

export default function ShortcutsTab() {
  const { t } = useTranslation();
  const resetToDefaults = useShortcutStore((s) => s.resetToDefaults);
  const [resetMessage, setResetMessage] = useState(false);

  const handleReset = () => {
    resetToDefaults();
    setResetMessage(true);
    setTimeout(() => setResetMessage(false), 2000);
  };

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: "20px",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
          <Keyboard size={18} />
          <h3 style={{ margin: 0, fontSize: "14px", fontWeight: 600 }}>
            {t("shortcuts.title")}
          </h3>
        </div>
        <button
          type="button"
          className="settings-button"
          onClick={handleReset}
          style={{ padding: "5px 11px", fontSize: "12px" }}
        >
          <RotateCcw size={12} />
          {t("shortcuts.resetDefaults")}
        </button>
      </div>

      <div role="status" aria-live="polite">
        {resetMessage && (
          <div
            className="settings-callout"
            style={{
              marginBottom: "16px",
              color: "var(--color-accent)",
              fontSize: "12.5px",
            }}
          >
            {t("shortcuts.resetConfirm")}
          </div>
        )}
      </div>

      {SHORTCUT_GROUPS.map((group) => ({ ...group, actions: group.actions.filter(isActionAvailable) })).filter((group) => group.actions.length > 0).map((group) => (
        <div key={group.categoryKey} style={{ marginBottom: "24px" }}>
          <h4
            style={{
              fontSize: "12px",
              fontWeight: 600,
              textTransform: "uppercase",
              letterSpacing: "0.05em",
              color: "var(--color-text-secondary)",
              marginBottom: "10px",
            }}
          >
            {t(group.categoryKey)}
          </h4>
          <div
            style={{
              borderRadius: "8px",
              border: "1px solid var(--color-border)",
              overflow: "hidden",
            }}
          >
            {group.actions.map((actionId, index) => (
              <div
                key={actionId}
                style={{
                  borderTop: index > 0 ? "1px solid var(--color-border)" : "none",
                }}
              >
                <ShortcutRow actionId={actionId} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
