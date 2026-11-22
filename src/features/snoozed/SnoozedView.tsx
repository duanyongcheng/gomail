import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Clock, Bell } from "lucide-react";
import { extractErrorMessage } from "@/lib/extractErrorMessage";
import type { SnoozedMessage, Message } from "@/lib/api";
import { listSnoozed, unsnoozeMessage, getMessagesBatch } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";
import { useMailStore } from "@/stores/mail.store";
import { useToastStore } from "@/stores/toast.store";

interface SnoozedEntry {
  snooze: SnoozedMessage;
  message: Message | null;
}

export default function SnoozedView() {
  const { t } = useTranslation();
  const [entries, setEntries] = useState<SnoozedEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const openMessageInInbox = useUIStore((s) => s.openMessageInInbox);
  const activeAccountId = useMailStore((s) => s.activeAccountId);

  // Reload whenever the sidebar account selection changes so the list only ever
  // shows snoozes belonging to the selected mailbox.
  useEffect(() => {
    loadSnoozed();
  }, [activeAccountId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function loadSnoozed() {
    setLoading(true);
    setError(null);
    try {
      const snoozed = await listSnoozed(activeAccountId ?? undefined);
      const ids = snoozed.map((s) => s.message_id);
      const messages = ids.length > 0 ? await getMessagesBatch(ids) : [];
      const messageMap = new Map(messages.map((m) => [m.id, m]));
      const withMessages = snoozed.map((s) => ({
        snooze: s,
        message: messageMap.get(s.message_id) ?? null,
      }));
      setEntries(withMessages);
    } catch (err) {
      setError(extractErrorMessage(err));
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }

  async function handleUnsnooze(messageId: string) {
    try {
      await unsnoozeMessage(messageId);
      setEntries((prev) => prev.filter((e) => e.snooze.message_id !== messageId));
    } catch (err) {
      console.error("Failed to unsnooze:", err);
      useToastStore.getState().addToast({ message: t("snoozed.unsnoozeFailed", "Failed to unsnooze message"), type: "error" });
    }
  }

  function handleOpen(messageId: string) {
    openMessageInInbox(messageId);
  }

  function formatSnoozeTime(timestamp: number): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleString();
  }

  if (loading) {
    return (
      <div className="mail-state-center">
        <div style={{ display: "inline-flex", alignItems: "center", gap: "8px", fontSize: "13px" }}>
          <Clock size={18} className="spin" />
          <span>{t("common.loading", "Loading...")}</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="fade-in mail-state-center">
        <Clock size={36} strokeWidth={1.3} />
        <p className="mail-state-error">
          {t("snoozed.loadError", "Failed to load snoozed messages")}
        </p>
        <p className="mail-state-detail">{error}</p>
        <button
          type="button"
          className="mail-state-secondary-btn"
          onClick={loadSnoozed}
        >
          {t("common.retry", "Retry")}
        </button>
      </div>
    );
  }

  if (entries.length === 0) {
    return (
      <div className="fade-in mail-state-center">
        <Clock size={36} strokeWidth={1.3} />
        <p className="mail-state-detail">{t("snoozed.empty", "No snoozed messages")}</p>
      </div>
    );
  }

  return (
    <div className="fade-in snoozed-workspace">
      <div className="secondary-mail-heading">
        <Clock size={16} aria-hidden="true" />
        <span>{t("snoozed.title", "Snoozed Messages")}</span>
        <span className="secondary-mail-count">
          {entries.length}
        </span>
      </div>

      <div className="scroll-region snoozed-list-scroll" style={{ flex: 1, overflowY: "auto" }}>
        {entries.map((entry) => (
          <div
            key={entry.snooze.message_id}
            className="snoozed-row"
            onClick={() => handleOpen(entry.snooze.message_id)}
          >
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{
                fontSize: "13.5px", fontWeight: 600, color: "var(--color-text-primary)",
                overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
              }}>
                {entry.message?.subject || entry.snooze.message_id}
              </div>
              <div style={{ fontSize: "12.5px", color: "var(--color-text-secondary)", marginTop: "2px" }}>
                {entry.message?.from_address || ""}
              </div>
              <div style={{ fontSize: "11.5px", color: "var(--color-text-tertiary)", marginTop: "4px", display: "flex", alignItems: "center", gap: "5px" }}>
                <Clock size={12} />
                {t("snoozed.wakeAt", "Wake at")} {formatSnoozeTime(entry.snooze.unsnoozed_at)}
              </div>
            </div>

            <button
              type="button"
              className="settings-button"
              onClick={(e) => {
                e.stopPropagation();
                handleUnsnooze(entry.snooze.message_id);
              }}
              title={t("snoozed.unsnooze", "Unsnooze")}
            >
              <Bell size={13} />
              {t("snoozed.unsnooze", "Unsnooze")}
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
