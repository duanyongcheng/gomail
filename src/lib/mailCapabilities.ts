import type { QueryClient } from "@tanstack/react-query";
import type { Account, MessageSummary } from "@/lib/api";

type MessageCapability = Pick<MessageSummary, "account_id"> & { capabilities?: { modify_flags: boolean } };

/** Explicit provider limits override old fixtures which omit capability data. */
export function canModifyMessage(message: MessageCapability, accounts: Account[] = []): boolean {
  return message.capabilities?.modify_flags !== false && accounts.find((account) => account.id === message.account_id)?.capabilities?.modify_flags !== false;
}

export function assertCachedMailMutation(client: QueryClient, messageIds: string[], accountId?: string) {
  const accounts = client.getQueryData<Account[]>(["accounts"]) ?? [];
  if (accountId && accounts.find((account) => account.id === accountId)?.capabilities?.modify_flags === false) throw new Error("This account does not support remote mail changes");
  const selected = new Set(messageIds);
  // Query shapes include detail, paged lists, thread arrays and search results.
  function inspect(value: unknown): void {
    if (!value || typeof value !== "object") return;
    if (Array.isArray(value)) { value.forEach(inspect); return; }
    const row = value as Record<string, unknown>;
    if (typeof row.id === "string" && selected.has(row.id) && typeof row.account_id === "string" && !canModifyMessage(row as unknown as MessageCapability, accounts)) throw new Error("This message does not support remote mail changes");
    if (Array.isArray(row.pages)) inspect(row.pages);
    if (Array.isArray(row.items)) inspect(row.items);
  }
  for (const [key, value] of client.getQueriesData({ predicate: (query) => ["message", "messages", "starred-messages", "threadMessages", "search"].includes(String(query.queryKey[0])) })) {
    void key; inspect(value);
  }
}
