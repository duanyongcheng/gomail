import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import { assertCachedMailMutation, canModifyMessage } from "@/lib/mailCapabilities";
import { executeMembershipAction, executeBatchMailAction, executeMarkAllRead } from "@/hooks/mutations/mailActionsMutation";
import type { Account } from "@/lib/api";

it("honors explicit protocol limits while preserving older DTO compatibility", () => {
  expect(canModifyMessage({ account_id: "a" })).toBe(true);
  expect(canModifyMessage({ account_id: "a", capabilities: { modify_flags: false } })).toBe(false);
  expect(canModifyMessage({ account_id: "a" }, [{ id: "a", capabilities: { modify_flags: false } } as Account])).toBe(false);
});
it("blocks read-only cached rows across detail, list and search shapes", () => {
  for (const [key, value] of [
    [["message", "p"], { id: "p", account_id: "a", capabilities: { modify_flags: false } }],
    [["messages", "inbox"], { pages: [[{ id: "p", account_id: "a", capabilities: { modify_flags: false } }]] }],
    [["search", "term"], [{ id: "p", account_id: "a", capabilities: { modify_flags: false } }]],
  ] as const) {
    const client = new QueryClient(); client.setQueryData(key, value);
    expect(() => assertCachedMailMutation(client, ["p"])).toThrow("does not support");
  }
});
it("all central action paths reject POP3 limits before native mutation", async () => {
  const client = new QueryClient();
  client.setQueryData(["accounts"], [{ id: "a", capabilities: { modify_flags: false } }]);
  client.setQueryData(["message", "p"], { id: "p", account_id: "a" });
  await expect(executeMembershipAction(client, { action: "archive", messageId: "p" })).rejects.toThrow("does not support");
  await expect(executeBatchMailAction(client, "delete", ["p"])).rejects.toThrow("does not support");
  await expect(executeMarkAllRead(client, { accountId: "a" })).rejects.toThrow("does not support");
});
