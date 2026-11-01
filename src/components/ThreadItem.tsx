import { memo, type CSSProperties } from "react";
import { Star, Paperclip } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ThreadSummary } from "@/lib/api";
import type { AccountBadgeInfo } from "@/lib/accountIdentity";
import AccountBadge from "./AccountBadge";
import { deriveAccountColor } from "@/lib/accountColors";

interface Props {
  thread: ThreadSummary;
  isSelected: boolean;
  onClick: () => void;
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

function ThreadItem({ thread, isSelected, onClick, accountBadge, rowIndex }: Props) {
  const { t } = useTranslation();
  const hasUnread = thread.unread_count > 0;
  const participantText = thread.participants.slice(0, 3).join(", ") +
    (thread.participants.length > 3 ? ` +${thread.participants.length - 3}` : "");

  return (
    <div
      className={`mail-row thread-list-row${hasUnread ? " thread-list-row--unread" : ""}`}
      role="option"
      aria-selected={isSelected}
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(); } }}
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
        style={{ "--sender-color": deriveAccountColor(thread.participants[0] ?? thread.thread_id) } as CSSProperties}
      >
        {Array.from((thread.participants[0] ?? "?").trim())[0]?.toLocaleUpperCase() || "?"}
      </span>
      <div className="mail-row-main">
        <div className="thread-row-head">
          <span className="mail-row-contact">
            {accountBadge && <AccountBadge badge={accountBadge} />}
            <span className="thread-row-participants">
              {participantText}
              {thread.message_count > 1 && (
                <span style={{ color: "var(--color-text-secondary)", fontWeight: "normal", marginLeft: "4px" }}>
                  ({thread.message_count})
                </span>
              )}
            </span>
            {/* How much of this thread is unread. A dot says "something"; the count
                says whether opening it is worth the interruption. */}
            {hasUnread && (
              <span className="thread-row-unread-count" aria-hidden="true">
                {thread.unread_count > 99 ? "99+" : thread.unread_count}
              </span>
            )}
          </span>
          <div className="mail-row-flags">
            {thread.is_starred && <Star size={13} fill="#f59e0b" color="#f59e0b" />}
            {thread.has_attachments && <Paperclip size={13} color="var(--color-text-secondary)" />}
            <span className="thread-row-date">
              {formatDate(thread.last_date)}
            </span>
          </div>
          {hasUnread && <span className="thread-row-unread-dot" aria-hidden="true" />}
        </div>
        <div className="thread-row-subject">
          {thread.subject || t("inbox.noSubject")}
        </div>
        <div className="mail-row-snippet">
          {thread.snippet}
        </div>
      </div>
    </div>
  );
}

export default memo(ThreadItem);
