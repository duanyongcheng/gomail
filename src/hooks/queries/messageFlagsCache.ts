import type { InfiniteData, QueryClient, QueryKey } from "@tanstack/react-query";
import type { Message, MessageSummary } from "@/lib/api";

type Flags = Pick<MessageSummary, "is_read" | "is_starred">;
type Flag = keyof Flags;
type Patch = Partial<Flags>;
const fields: Flag[] = ["is_read", "is_starred"];
const owners = new WeakMap<QueryClient, Map<string, symbol>>();
const manualUnread = new WeakMap<QueryClient, Map<string, symbol>>();

export function recordManualUnread(client: QueryClient, messageId: string) {
  let records = manualUnread.get(client);
  if (!records) { records = new Map(); manualUnread.set(client, records); }
  records.set(messageId, Symbol());
}

export function manualUnreadVersion(client: QueryClient, messageId: string) {
  return manualUnread.get(client)?.get(messageId);
}

export function findMessageFlags(client: QueryClient, messageId: string): Flags | undefined {
  let latest: Flags | undefined;
  let updatedAt = -1;
  let active = false;
  // An inactive detail may predate the currently visible list after sync. Do
  // not privilege a cache root over a newer local/optimistic observation.
  for (const query of client.getQueryCache().findAll({
    predicate: (query) => messageFlagsQueryRoots.some((root) => query.queryKey[0] === root),
  })) {
    if (query.state.dataUpdatedAt < updatedAt || (query.state.dataUpdatedAt === updatedAt && active && !query.isActive())) continue;
    mapMessages(query.state.data, query.queryKey[0], (message) => {
      if (message.id === messageId) {
        latest = message;
        updatedAt = query.state.dataUpdatedAt;
        active = query.isActive();
      }
      return message;
    });
  }
  return latest;
}
export const messageFlagsQueryRoots = ["message", "messages", "starred-messages", "threadMessages"] as const;

function mapMessages(data: unknown, root: unknown, update: (message: MessageSummary) => MessageSummary): unknown {
  if (!data) return data;
  if (root === "message") return update(data as Message);
  if (root === "threadMessages") return (data as Message[]).map(update);
  const pages = data as InfiniteData<MessageSummary[]>;
  return { ...pages, pages: pages.pages.map((page) => page.map(update)) };
}

/** A count-only bulk receipt cannot patch individual rows. It can, however,
 * retire rollback rights captured before that write for its requested fields.
 * Only still-matching owners are retired: a newer explicit action keeps its
 * rights. No private values or invented modified-message IDs are stored.
 */
export function captureMessageFlagReceipt(client: QueryClient, patch: Patch, includes: (message: MessageSummary) => boolean) {
  const currentOwners = owners.get(client);
  const captured = new Map<string, symbol>();
  for (const [key, data] of client.getQueriesData({
    predicate: (query) => messageFlagsQueryRoots.some((root) => query.queryKey[0] === root),
  })) {
    mapMessages(data, key[0], (message) => {
      if (includes(message)) for (const field of fields) {
        const ownerKey = `${message.id}:${field}`;
        const token = currentOwners?.get(ownerKey);
        if (patch[field] !== undefined && token) captured.set(ownerKey, token);
      }
      return message;
    });
  }
  return () => {
    for (const [key, token] of captured) {
      if (currentOwners?.get(key) === token) currentOwners.delete(key);
    }
  };
}

export function patchMessageFlags(client: QueryClient, messageId: string, patch: Patch) {
  const token = Symbol();
  let currentOwners = owners.get(client);
  if (!currentOwners) { currentOwners = new Map(); owners.set(client, currentOwners); }
  for (const field of fields) {
    if (patch[field] !== undefined) currentOwners.set(`${messageId}:${field}`, token);
  }
  const snapshots: Array<{ key: QueryKey; previous: Patch }> = [];
  for (const [key, data] of client.getQueriesData({
    predicate: (query) => messageFlagsQueryRoots.some((root) => query.queryKey[0] === root),
  })) {
    let previous: Patch | undefined;
    const updated = mapMessages(data, key[0], (message) => {
      if (message.id !== messageId) return message;
      previous ??= { is_read: message.is_read, is_starred: message.is_starred };
      return { ...message, ...patch };
    });
    if (previous) {
      snapshots.push({ key, previous });
      // A partial optimistic patch is not a fresh observation of the other
      // flag (or other messages). Keep each query's authoritative timestamp so
      // a read action cannot make stale star metadata win the next toggle.
      client.setQueryData(key, updated, { updatedAt: client.getQueryState(key)?.dataUpdatedAt });
    }
  }
  return {
    rollback() {
      // Restore only this operation's fields and message. A late failure must
      // not erase another message's update or a more recent flag action.
      for (const { key, previous } of snapshots) {
        client.setQueryData(key, (data: unknown) => mapMessages(data, key[0], (message) => {
          if (message.id !== messageId) return message;
          const restore: Patch = {};
          for (const field of fields) {
            if (patch[field] !== undefined && currentOwners.get(`${messageId}:${field}`) === token && message[field] === patch[field]) {
              restore[field] = previous[field];
            }
          }
          return { ...message, ...restore };
        }), { updatedAt: client.getQueryState(key)?.dataUpdatedAt });
      }
    },
    finish() {
      for (const field of fields) {
        const key = `${messageId}:${field}`;
        if (currentOwners.get(key) === token) currentOwners.delete(key);
      }
    },
  };
}
