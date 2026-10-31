import type { MutationOptions, QueryClient } from "@tanstack/react-query";
import {
  archiveMessage, batchArchive, batchDelete, batchMarkRead, batchStar,
  deleteMessage, markAccountAllRead, markFolderAllRead, moveToFolder, restoreMessage,
} from "@/lib/api";
import { captureMessageFlagReceipt, recordManualUnread } from "@/hooks/queries/messageFlagsCache";
import { cancelMessageFlagViews, messageFlagsMutationKey, settleMessageFlagViews } from "./messageFlagsMutation";

export type BatchMailAction = "archive" | "delete" | "markRead" | "markUnread" | "star" | "unstar";
export type MembershipAction =
  | { action: "archive" | "delete" | "restore"; messageId: string }
  | { action: "move"; messageId: string; targetFolderId: string };
export type MarkAllReadScope = { accountId: string; folderIds?: string[] };

/** Count-only and membership RPCs cannot supply trustworthy optimistic rows.
 * Reuse the flags lifecycle, but let authoritative reads apply their effects
 * after acknowledgement. Never infer modified IDs from an aggregate receipt.
 * Errors/toasts belong to the invoking control, preserving its static labels.
 */
function receiptOptions<T, V>(client: QueryClient, mutationFn: (variables: V) => Promise<T>, changesUnreadCounts: boolean,
  prepare?: (variables: V) => void, captureReceipt?: (variables: V) => () => void): MutationOptions<T, Error, V, (() => void) | undefined> {
  return {
    mutationKey: messageFlagsMutationKey,
    networkMode: "always", // SQLite + durable queue, not a Gmail write.
    mutationFn,
    onMutate: async (variables) => {
      prepare?.(variables);
      await cancelMessageFlagViews(client);
      return captureReceipt?.(variables);
    },
    onSuccess: (data, _variables, receipt) => {
      if (typeof data !== "number" || data > 0) receipt?.();
    },
    onSettled: () => settleMessageFlagViews(client, changesUnreadCounts),
  };
}

export function executeMembershipAction(client: QueryClient, params: MembershipAction) {
  const options = receiptOptions<string | void, MembershipAction>(client, (variables) => {
    switch (variables.action) {
      case "archive": return archiveMessage(variables.messageId);
      case "delete": return deleteMessage(variables.messageId);
      case "restore": return restoreMessage(variables.messageId);
      case "move": return moveToFolder(variables.messageId, variables.targetFolderId);
    }
  }, true);
  return client.getMutationCache().build(client, options).execute({ ...params });
}

export function executeBatchMailAction(client: QueryClient, action: BatchMailAction, messageIds: string[]) {
  const options = receiptOptions(client, (ids: string[]) => {
    switch (action) {
      case "archive": return batchArchive(ids);
      case "delete": return batchDelete(ids);
      case "markRead": return batchMarkRead(ids, true);
      case "markUnread": return batchMarkRead(ids, false);
      case "star": return batchStar(ids, true);
      case "unstar": return batchStar(ids, false);
    }
  }, action !== "star" && action !== "unstar", action === "markUnread"
    ? (ids) => ids.forEach((id) => recordManualUnread(client, id)) : undefined,
  action === "archive" || action === "delete" ? undefined : (ids) => {
    const requested = new Set(ids);
    const patch = action === "star" || action === "unstar" ? { is_starred: action === "star" } : { is_read: action === "markRead" };
    return captureMessageFlagReceipt(client, patch, (message) => requested.has(message.id));
  });
  return client.getMutationCache().build(client, options).execute([...new Set(messageIds)]);
}

export function executeMarkAllRead(client: QueryClient, scope: MarkAllReadScope) {
  const options = receiptOptions(client, (variables: MarkAllReadScope) => variables.folderIds === undefined
    ? markAccountAllRead(variables.accountId)
    : markFolderAllRead(variables.accountId, variables.folderIds), true, undefined,
  (variables) => captureMessageFlagReceipt(client, { is_read: true }, (message) => message.account_id === variables.accountId));
  return client.getMutationCache().build(client, options).execute({ ...scope, ...(scope.folderIds && { folderIds: [...scope.folderIds] }) });
}
