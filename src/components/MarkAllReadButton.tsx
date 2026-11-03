import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { Loader, MailOpen } from "lucide-react";
import { executeMarkAllRead } from "@/hooks/mutations/mailActionsMutation";
import { useToastStore } from "@/stores/toast.store";
import type { Account } from "@/lib/api";

interface Props {
  accountId: string;
  /** Shown in tooltips and the success toast, e.g. "Work (me@example.com)". */
  accountLabel: string;
  /** Unread mail in this mailbox; 0 disables the action. */
  unread: number;
  /**
   * `icon` for toolbars and the sidebar, `labelled` for settings rows where
   * there is room to spell the action out.
   */
  variant?: "icon" | "labelled";
  style?: React.CSSProperties;
}

/**
 * One-click "mark every unread message in this mailbox as read".
 *
 * The work happens in the backend (which also refreshes the app icon badge);
 * this component only reports the outcome and refreshes the cached views. It is
 * disabled while the local durable write is in flight. Remote replay does not
 * block acknowledgement.
 */
export default function MarkAllReadButton({
  accountId,
  accountLabel,
  unread,
  variant = "icon",
  style,
}: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const addToast = useToastStore((s) => s.addToast);
  const [marking, setMarking] = useState(false);
  const busy = useRef(false);

  const label = t("messageActions.markAllRead", "Mark all as read in {{account}}", {
    account: accountLabel,
  });
  const disabled = marking || unread === 0 || queryClient.getQueryData<Account[]>(["accounts"])?.find((account) => account.id === accountId)?.capabilities?.modify_flags === false;

  async function handleClick() {
    if (disabled || busy.current) return;
    busy.current = true;
    setMarking(true);
    try {
      const count = await executeMarkAllRead(queryClient, { accountId });
      addToast({
        message: t(
          "messageActions.markAllReadSuccess",
          "Marked {{count}} messages as read in {{account}}",
          { count, account: accountLabel },
        ),
        type: "success",
      });
    } catch {
      addToast({
        message: t("messageActions.markAllReadFailed", "Failed to mark messages as read"),
        type: "error",
      });
    } finally {
      busy.current = false;
      setMarking(false);
    }
  }

  const baseStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    gap: "6px",
    padding: variant === "labelled" ? "6px 10px" : "6px",
    borderRadius: "6px",
    border: "none",
    backgroundColor: "transparent",
    color: "var(--color-text-secondary)",
    fontSize: "12px",
    fontWeight: variant === "labelled" ? 600 : undefined,
    cursor: disabled ? "default" : "pointer",
    opacity: disabled ? 0.45 : 1,
    flexShrink: 0,
    ...style,
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={disabled}
      title={label}
      aria-label={label}
      data-testid={`mark-all-read-${accountId}`}
      style={baseStyle}
      onMouseEnter={(e) => {
        if (!disabled) e.currentTarget.style.color = "var(--color-accent)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.color = "var(--color-text-secondary)";
      }}
    >
      {marking ? (
        <Loader size={14} className="spinner" />
      ) : (
        <MailOpen size={14} />
      )}
      {variant === "labelled" && (
        <span>
          {marking
            ? t("messageActions.markAllReadInProgress", "Marking as read...")
            : t("messageActions.markAllReadShort", "Mark all as read")}
        </span>
      )}
    </button>
  );
}
