import type { MutationOptions, QueryClient } from "@tanstack/react-query";
import { updateMessageFlags } from "@/lib/api";
import {
  findMessageFlags,
  messageFlagsQueryRoots,
  patchMessageFlags,
  recordManualUnread,
} from "@/hooks/queries/messageFlagsCache";
import { useToastStore } from "@/stores/toast.store";

export interface UpdateFlagsParams {
  messageId: string;
  isRead?: boolean;
  isStarred?: boolean;
  toggleStarred?: boolean;
}

export const messageFlagsMutationKey = ["message-flags"] as const;
const deferredRefreshes = new WeakMap<QueryClient, () => void>();

/** Events and mutation receipts share this barrier: never refetch over a provisional flag. */
export function refreshMessageFlagViews(client: QueryClient) {
  if (client.isMutating({ mutationKey: messageFlagsMutationKey })) {
    if (!deferredRefreshes.has(client)) {
      const unsubscribe = client.getMutationCache().subscribe(() => {
        if (client.isMutating({ mutationKey: messageFlagsMutationKey })) return;
        unsubscribe();
        deferredRefreshes.delete(client);
        refreshMessageFlagViews(client);
      });
      deferredRefreshes.set(client, unsubscribe);
    }
    return;
  }
  for (const root of [...messageFlagsQueryRoots, "threads", "search"]) {
    void client.invalidateQueries({ queryKey: [root] });
  }
}

/** All supported local mail producers cancel interfering reads on entry. */
export async function cancelMessageFlagViews(client: QueryClient, messageId?: string) {
  await Promise.all([...messageFlagsQueryRoots, "threads", "search"].map((root) =>
    client.cancelQueries({ queryKey: root === "message" && messageId ? [root, messageId] : [root] }),
  ));
}

/** Counts acknowledge each local receipt; message data waits for final settlement. */
export function settleMessageFlagViews(client: QueryClient, changesUnreadCounts: boolean) {
  if (changesUnreadCounts) {
    void client.invalidateQueries({ queryKey: ["folder-unread-counts"] });
    void client.invalidateQueries({ queryKey: ["account-unread-counts"] });
  }
  void client.invalidateQueries({ queryKey: ["pendingMailOps"] });
  void client.invalidateQueries({ queryKey: ["pendingMailOpsList"] });
  refreshMessageFlagViews(client);
}

export function messageFlagsMutationOptions(
  client: QueryClient,
  t: (key: string, fallback: string) => string,
): MutationOptions<void, Error, UpdateFlagsParams, ReturnType<typeof patchMessageFlags>> {
  return {
    mutationKey: messageFlagsMutationKey,
    // This RPC durably queues a local SQLite mutation; Gmail replay owns network
    // availability. Pausing it offline would show an unpersisted optimistic flag.
    networkMode: "always",
    mutationFn: (params) => updateMessageFlags(params.messageId, params.isRead, params.isStarred),
    onMutate: async (params) => {
      if (params.isRead === false) recordManualUnread(client, params.messageId);
      await cancelMessageFlagViews(client, params.messageId);
      // Resolve the intent only after cancellation, immediately before patching.
      // Each execution owns its variables, so rapid shortcuts see the preceding
      // optimistic toggle rather than a stale render/event-handler snapshot.
      if (params.toggleStarred) params.isStarred = !findMessageFlags(client, params.messageId)?.is_starred;
      return patchMessageFlags(client, params.messageId, {
        ...(params.isRead !== undefined && { is_read: params.isRead }),
        ...(params.isStarred !== undefined && { is_starred: params.isStarred }),
      });
    },
    onError: (_err, _params, context) => {
      context?.rollback();
      useToastStore.getState().addToast({ message: t("thread.actionFailed", "Could not update or open this message."), type: "error" });
    },
    onSettled: (_data, _err, params, context) => {
      context?.finish();
      settleMessageFlagViews(client, params.isRead !== undefined);
    },
  };
}

/** Imperative actions still participate in useMutation's cache/key/lifecycle. */
export function executeMessageFlags(
  client: QueryClient,
  params: UpdateFlagsParams,
  t: (key: string, fallback: string) => string,
) {
  return client.getMutationCache().build(client, messageFlagsMutationOptions(client, t)).execute({ ...params });
}
