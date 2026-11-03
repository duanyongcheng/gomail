import { Reply, ReplyAll, Forward, Star, Archive, Trash2, LayoutGrid, RotateCcw } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useKanbanStore } from "@/stores/kanban.store";
import { useToastStore } from "@/stores/toast.store";
import { useTranslation } from "react-i18next";
import { useUpdateFlagsMutation } from "@/hooks/mutations/useUpdateFlagsMutation";
import { useComposeStore } from "@/stores/compose.store";
import { executeMembershipAction } from "@/hooks/mutations/mailActionsMutation";
import type { Account, Message } from "@/lib/api";
import { useEffect, useRef, useState } from "react";
import ConfirmDialog from "./ConfirmDialog";
import { isFeatureAvailable, unavailableReason } from "@/lib/availability";
import { canModifyMessage } from "@/lib/mailCapabilities";

interface Props {
  message: Message;
  folderRole?: string | null;
  onBack: () => void;
  /**
   * `vertical` stacks the actions, for themes that park the toolbar in a
   * details sidebar. Spacing lives in the stylesheet so themes can adjust it.
   */
  orientation?: "horizontal" | "vertical";
}

export default function MessageActionToolbar({
  message,
  folderRole,
  onBack,
  orientation = "horizontal",
}: Props) {
  const { t } = useTranslation();
  const openCompose = useComposeStore((s) => s.openCompose);
  const queryClient = useQueryClient();
  const flagsMutation = useUpdateFlagsMutation();
  const canModify = canModifyMessage(message, queryClient.getQueryData<Account[]>(["accounts"]) ?? []);
  const inKanban = useKanbanStore((s) => s.cardIdSet.has(message.id));
  const [showKanbanPicker, setShowKanbanPicker] = useState(false);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);
  const [membershipPending, setMembershipPending] = useState(false);
  const membershipBusy = useRef(false);
  const currentMessageId = useRef(message.id);
  currentMessageId.current = message.id;
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);

  function backAfterReceipt(id: string) {
    if (mounted.current && currentMessageId.current === id) onBack();
  }
  // Folder context is only a compatibility fallback for older/synthetic DTOs.
  // Search, Starred and cross-folder readers must use the selected message itself.
  const inTrash = message.in_trash ?? folderRole === "trash";
  const deleteUnavailable = inTrash && !isFeatureAvailable("permanentDelete");

  useEffect(() => {
    if (!showKanbanPicker) return;
    function handleClick() { setShowKanbanPicker(false); }
    // Delay to avoid catching the same click that opened the picker
    const timer = setTimeout(() => {
      document.addEventListener("click", handleClick);
    }, 0);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("click", handleClick);
    };
  }, [showKanbanPicker]);

  async function handleAddToKanban(column: "todo" | "waiting" | "done") {
    if (!isFeatureAvailable("kanban")) return;
    setShowKanbanPicker(false);
    try {
      await useKanbanStore.getState().addCard(message.id, column);
      useToastStore.getState().addToast({
        message: t("messageActions.kanbanSuccess", "Added to kanban board"),
        type: "success",
      });
    } catch {
      useToastStore.getState().addToast({
        message: t("messageActions.kanbanFailed", "Failed to add to kanban"),
        type: "error",
      });
    }
  }

  const actions: Array<{
    icon: React.ComponentType<{ size?: number }>;
    label: string;
    action: () => void;
    active?: boolean;
    disabled?: boolean;
  }> = [
    { icon: Reply, label: t("messageActions.reply"), action: () => openCompose("reply", message) },
    { icon: ReplyAll, label: t("messageActions.replyAll"), action: () => openCompose("reply-all", message) },
    { icon: Forward, label: t("messageActions.forward"), action: () => openCompose("forward", message) },
    {
      icon: Star,
      label: message.is_starred ? t("messageActions.unstar") : t("messageActions.star"),
      action: () => {
        flagsMutation.mutate(
          { messageId: message.id, toggleStarred: true },
        );
      },
      active: message.is_starred,
      disabled: !canModify,
    },
    {
      icon: folderRole === "archive" ? RotateCcw : Archive,
      label: folderRole === "archive"
        ? t("messageActions.unarchive", "Unarchive")
        : t("messageActions.archive"),
      disabled: membershipPending || !canModify,
      action: async () => {
        if (membershipBusy.current) return;
        membershipBusy.current = true;
        setMembershipPending(true);
        const id = message.id;
        try {
          const result = await executeMembershipAction(queryClient, { action: "archive", messageId: id });
          if (result === "skipped") return;
          if (result === "unarchived") {
            useToastStore.getState().addToast({
              message: t("messageActions.unarchiveSuccess", "Message moved to inbox"),
              type: "success",
            });
          } else {
            useToastStore.getState().addToast({
              message: t("messageActions.archiveSuccess", "Message archived"),
              type: "success",
            });
          }
          backAfterReceipt(id);
        } catch {
          useToastStore.getState().addToast({
            message: folderRole === "archive"
              ? t("messageActions.unarchiveFailed", "Failed to unarchive")
              : t("messageActions.archiveFailed", "Failed to archive message"),
            type: "error",
          });
        } finally {
          membershipBusy.current = false;
          if (mounted.current) setMembershipPending(false);
        }
      },
    },
    ...(inTrash ? [{
      icon: RotateCcw,
      label: t("messageActions.restore", "Restore"),
      disabled: membershipPending || !canModify,
      action: async () => {
        if (membershipBusy.current) return;
        membershipBusy.current = true;
        setMembershipPending(true);
        const id = message.id;
        try {
          await executeMembershipAction(queryClient, { action: "restore", messageId: id });
          useToastStore.getState().addToast({
            message: t("messageActions.restoreSuccess", "Message restored to inbox"),
            type: "success",
          });
          backAfterReceipt(id);
        } catch {
          useToastStore.getState().addToast({
            message: t("messageActions.restoreFailed", "Failed to restore message"),
            type: "error",
          });
        } finally {
          membershipBusy.current = false;
          if (mounted.current) setMembershipPending(false);
        }
      },
    }] : []),
    {
      icon: Trash2,
      label: t("messageActions.delete"),
      action: () => {
        if (deleteUnavailable) return;
        setDeleteTargetId(message.id);
      },
      disabled: deleteUnavailable || membershipPending || !canModify,
    },
  ];

  return (
    <>
      <div
        className="message-action-toolbar"
        data-orientation={orientation}
        style={{ display: "flex", gap: "2px", position: "relative" }}
      >
        {actions.map(({ icon: Icon, label, action, active, disabled }, i) => (
          <button
            key={i}
            onClick={disabled ? undefined : action}
            disabled={disabled}
            title={disabled ? unavailableReason() : label}
            aria-label={label}
            style={{
              background: "none",
              border: "none",
              cursor: disabled ? "default" : "pointer",
              padding: "6px 8px",
              borderRadius: "4px",
              color: active ? "var(--color-accent)" : "var(--color-text-secondary)",
              display: "flex",
              alignItems: "center",
              transition: "background-color 0.12s ease, color 0.12s ease",
              opacity: disabled ? 0.35 : 1,
            }}
            onMouseEnter={disabled ? undefined : (e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = "var(--color-bg-hover)"; }}
            onMouseLeave={disabled ? undefined : (e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = "transparent"; }}
          >
            <Icon size={16} />
          </button>
        ))}
        {/* Kanban button + picker */}
        <div style={{ position: "relative" }}>
          <button
            disabled={!isFeatureAvailable("kanban")}
            onClick={() => { if (isFeatureAvailable("kanban")) setShowKanbanPicker(true); }}
            title={isFeatureAvailable("kanban") ? t("messageActions.addToKanban") : unavailableReason()}
            aria-label={t("messageActions.addToKanban")}
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              padding: "6px 8px",
              borderRadius: "4px",
              color: inKanban ? "var(--color-accent)" : "var(--color-text-secondary)",
              display: "flex",
              alignItems: "center",
              transition: "background-color 0.12s ease, color 0.12s ease",
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = "var(--color-bg-hover)"; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = "transparent"; }}
          >
            <LayoutGrid size={16} />
          </button>
          {isFeatureAvailable("kanban") && showKanbanPicker && (
            <div
              onClick={(e) => e.stopPropagation()}
              style={{
                position: "absolute",
                right: "0px",
                top: "100%",
                marginTop: "4px",
                backgroundColor: "var(--color-bg)",
                border: "1px solid var(--color-border)",
                borderRadius: "8px",
                boxShadow: "0 4px 12px rgba(0,0,0,0.12)",
                padding: "4px",
                zIndex: 10,
                minWidth: "140px",
              }}
            >
              {([
                { col: "todo" as const, label: t("kanban.todo", "To Do") },
                { col: "waiting" as const, label: t("kanban.waiting", "Waiting") },
                { col: "done" as const, label: t("kanban.done", "Done") },
              ]).map(({ col, label }) => (
                <button
                  key={col}
                  onClick={() => handleAddToKanban(col)}
                  style={{
                    display: "block",
                    width: "100%",
                    padding: "8px 12px",
                    border: "none",
                    background: "transparent",
                    cursor: "pointer",
                    fontSize: "13px",
                    color: "var(--color-text-primary)",
                    borderRadius: "4px",
                    textAlign: "left",
                  }}
                  onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = "var(--color-bg-hover)"; }}
                  onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = "transparent"; }}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {deleteTargetId === message.id && !deleteUnavailable && (
        <ConfirmDialog
          title={t("messageActions.delete", "Delete")}
          message={t("messageActions.deleteConfirm", "Move this message to trash?")}
          confirmLabel={t("common.delete", "Delete")}
          destructive
          onCancel={() => setDeleteTargetId(null)}
          onConfirm={async () => {
            if (deleteTargetId !== message.id || deleteUnavailable || membershipBusy.current) return;
            setDeleteTargetId(null);
            membershipBusy.current = true;
            setMembershipPending(true);
            const id = message.id;
            try {
              await executeMembershipAction(queryClient, { action: "delete", messageId: id });
              useToastStore.getState().addToast({
                message: t("messageActions.deleteSuccess", "Message deleted"),
                type: "success",
              });
              backAfterReceipt(id);
            } catch {
              useToastStore.getState().addToast({
                message: t("messageActions.deleteFailed", "Failed to delete message"),
                type: "error",
              });
            } finally {
              membershipBusy.current = false;
              if (mounted.current) setMembershipPending(false);
            }
          }}
        />
      )}
    </>
  );
}
