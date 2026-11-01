import { memo, useState, type CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { Star, Paperclip, Archive, LayoutGrid, ShieldAlert, RotateCcw } from "lucide-react";
import type { EmailAddress, Folder, Label, MessageSummary } from "@/lib/api";
import type { AccountBadgeInfo } from "@/lib/accountIdentity";
import { executeMessageFlags } from "@/hooks/mutations/messageFlagsMutation";
import { executeMembershipAction } from "@/hooks/mutations/mailActionsMutation";
import { useKanbanStore } from "@/stores/kanban.store";
import { useToastStore } from "@/stores/toast.store";
import AccountBadge from "./AccountBadge";
import { isFeatureAvailable, unavailableReason } from "@/lib/availability";
import { deriveAccountColor } from "@/lib/accountColors";

interface Props {
  message: MessageSummary;
  labels?: Label[];
  isSelected: boolean;
  onClick: () => void;
  batchMode?: boolean;
  batchSelected?: boolean;
  onToggleBatchSelect?: (messageId: string) => void;
  spamFolderId?: string;
  folderRole?: Folder["role"];
  /** Only supplied by the combined inbox, where rows come from many mailboxes. */
  accountBadge?: AccountBadgeInfo;
  rowIndex?: number;
}

function formatDate(timestamp: number): string {
  const date = new Date(timestamp * 1000);
  const now = new Date();
  const isToday =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();

  if (isToday) {
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  return date.toLocaleDateString([], { month: "short", day: "numeric" });
}

function addressLabel(address: EmailAddress): string {
  return address.name?.trim() || address.address;
}

function recipientLabel(addresses: EmailAddress[]): string {
  return addresses.map(addressLabel).filter(Boolean).join(", ");
}

function MessageItem({ message, labels = [], isSelected, onClick, batchMode, batchSelected, onToggleBatchSelect, spamFolderId, folderRole, accountBadge, rowIndex }: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [showActions, setShowActions] = useState(false);
  const inKanban = useKanbanStore((s) => s.cardIdSet.has(message.id));
  const archiveActionLabel = folderRole === "archive"
    ? t("messageActions.unarchive", "Unarchive")
    : t("messageActions.archive", "Archive");
  const ArchiveActionIcon = folderRole === "archive" ? RotateCcw : Archive;
  const primaryContact = folderRole === "sent" && message.to_list.length > 0
    ? recipientLabel(message.to_list)
    : message.from_name || message.from_address;

  return (
    <div
      className={`mail-row message-list-row${message.is_read ? "" : " message-list-row--unread"}`}
      onClick={onClick}
      tabIndex={0}
      role="option"
      aria-selected={isSelected}
      onMouseEnter={() => {
        setShowActions(true);
      }}
      onMouseLeave={() => {
        setShowActions(false);
      }}
      onFocus={() => {
        setShowActions(true);
      }}
      onBlur={(e) => {
        // Only hide if focus leaves this element entirely (not moving to a child)
        if (!e.currentTarget.contains(e.relatedTarget as Node)) {
          setShowActions(false);
        }
      }}
      onKeyDown={(e) => {
        if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          onClick();
        }
      }}
    >
      {/* Decorative companion to the badge below: a colour to scan down the
          list by, while the badge spells the mailbox out. Hidden from
          assistive tech so the row is announced once, not twice. */}
      {accountBadge && (
        <span
          aria-hidden="true"
          data-testid="account-color-bar"
          style={{
            position: "absolute",
            left: 0,
            top: "10px",
            bottom: "10px",
            width: "3px",
            borderRadius: "0 3px 3px 0",
            backgroundColor: accountBadge.color,
          }}
        />
      )}
      {rowIndex !== undefined && <span className="mail-row-index" aria-hidden="true">{String(rowIndex + 1).padStart(2, "0")}</span>}
      <span
        className="mail-row-avatar"
        aria-hidden="true"
        style={{ "--sender-color": deriveAccountColor(message.from_address) } as CSSProperties}
      >
        {Array.from(primaryContact.trim())[0]?.toLocaleUpperCase() || "?"}
      </span>
      <div className="mail-row-main">
        <div className="message-row-head">
          {batchMode && (
            <input
              type="checkbox"
              checked={batchSelected}
              aria-label={t("batch.selectMessage", "Select message")}
              className="batch-checkbox message-row-checkbox"
              onChange={(e) => {
                e.stopPropagation();
                onToggleBatchSelect?.(message.id);
              }}
              onClick={(e) => e.stopPropagation()}
            />
          )}
          <span className="mail-row-contact">
            {accountBadge && <AccountBadge badge={accountBadge} />}
            <span className="message-row-sender-name">
              {primaryContact}
            </span>
          </span>
          <div className="mail-row-flags">
            {isFeatureAvailable("kanban") && inKanban && (
              <LayoutGrid size={13} color="var(--color-accent)" />
            )}
            {message.is_starred && (
              <Star size={13} fill="#f59e0b" color="#f59e0b" />
            )}
            {message.has_attachments && (
              <Paperclip size={13} color="var(--color-text-secondary)" />
            )}
            <span className="message-row-date">
              {formatDate(message.date)}
            </span>
          </div>
          {/* Marks the row as unread in the list's own gutter, beside the sender
              rather than after the name, so the markers form a column. */}
          {!message.is_read && <span className="message-row-unread-dot" aria-hidden="true" />}
        </div>
        <div className="message-row-subject">
          {message.subject || t("inbox.noSubject")}
        </div>
        <div className="mail-row-snippet">
          {message.snippet}
          {isFeatureAvailable("localLabels") && labels.length > 0 && labels.map((l) => (
            <span
              key={l.id}
              style={{
                display: "inline-block",
                fontSize: "10px",
                padding: "0 5px",
                borderRadius: "3px",
                backgroundColor: l.color + "22",
                color: l.color,
                border: `1px solid ${l.color}44`,
                marginLeft: "6px",
                verticalAlign: "middle",
                lineHeight: "16px",
                fontWeight: 500,
              }}
            >
              {l.name}
            </span>
          ))}
        </div>
      </div>
      {showActions && (
        <div
          className="mail-row-actions"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            onClick={(e) => {
              e.stopPropagation();
              executeMembershipAction(queryClient, { action: "archive", messageId: message.id })
                .then((result) => {
                  if (result === "skipped") return;
                  const msg = result === "unarchived"
                    ? t("messageActions.unarchiveSuccess", "Message moved to inbox")
                    : t("messageActions.archiveSuccess", "Message archived");
                  useToastStore.getState().addToast({ message: msg, type: "success" });
                })
                .catch(() => {
                  const msg = folderRole === "archive"
                    ? t("messageActions.unarchiveFailed", "Failed to unarchive")
                    : t("messageActions.archiveFailed", "Failed to archive");
                  useToastStore.getState().addToast({ message: msg, type: "error" });
                });
            }}
            aria-label={archiveActionLabel}
            title={archiveActionLabel}
            className="mail-row-action"
          >
            <ArchiveActionIcon size={14} />
          </button>
          {spamFolderId && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                executeMembershipAction(queryClient, { action: "move", messageId: message.id, targetFolderId: spamFolderId })
                  .then(() => {
                    useToastStore.getState().addToast({ message: t("messageActions.spamSuccess", "Marked as spam"), type: "success" });
                  })
                  .catch(() => {
                    useToastStore.getState().addToast({ message: t("messageActions.spamFailed", "Failed to mark as spam"), type: "error" });
                  });
              }}
              aria-label={t("messageActions.reportSpam", "Report spam")}
              title={t("messageActions.reportSpam", "Report spam")}
              className="mail-row-action"
            >
              <ShieldAlert size={14} />
            </button>
          )}
          <button
            onClick={(e) => {
              e.stopPropagation();
              if (!isFeatureAvailable("kanban")) return;
              useKanbanStore.getState().addCard(message.id, "todo")
                .then(() => {
                  useToastStore.getState().addToast({ message: t("messageActions.kanbanSuccess", "Added to kanban board"), type: "success" });
                })
                .catch(() => {
                  useToastStore.getState().addToast({ message: t("messageActions.kanbanFailed", "Failed to add to kanban"), type: "error" });
                });
            }}
            aria-label={t("messageActions.addToKanban")}
            disabled={!isFeatureAvailable("kanban")}
            title={isFeatureAvailable("kanban") ? t("messageActions.addToKanban") : unavailableReason()}
            className="mail-row-action"
          >
            <LayoutGrid size={14} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              void executeMessageFlags(queryClient, { messageId: message.id, toggleStarred: true }, t).catch(() => {});
            }}
            aria-label={message.is_starred ? t("messageActions.unstar") : t("messageActions.star")}
            aria-pressed={message.is_starred}
            title={message.is_starred ? t("messageActions.unstar") : t("messageActions.star")}
            className="mail-row-action"
            style={{ color: message.is_starred ? "#f59e0b" : undefined }}
          >
            <Star
              size={14}
              fill={message.is_starred ? "#f59e0b" : "none"}
              color={message.is_starred ? "#f59e0b" : "currentColor"}
            />
          </button>
        </div>
      )}
    </div>
  );
}

export default memo(MessageItem);
