import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { useComposeRecipients } from "@/hooks/useComposeRecipients";
import type { Account, Message } from "@/lib/ipc-types";

afterEach(cleanup);
it("uses the original mailbox when replying from the combined inbox", () => {
  const accounts: Account[] = [
    { id: "a", email: "a@example.invalid", display_name: "A", provider: "gmail", created_at: 1, updated_at: 1 },
    { id: "b", email: "b@example.invalid", display_name: "B", provider: "gmail", created_at: 1, updated_at: 1 },
  ];
  const message: Message = {
    id: "b:m", account_id: "b", remote_id: "m", message_id_header: "m@example.invalid", in_reply_to: null, references_header: null, thread_id: "opaque",
    subject: "topic", snippet: "", from_address: "sender@example.invalid", from_name: "Sender", to_list: [{ name: null, address: "b@example.invalid" }],
    cc_list: [{ name: null, address: "cc@example.invalid" }], bcc_list: [], body_text: "body", body_html_raw: "", has_attachments: false,
    is_read: true, is_starred: false, is_draft: false, date: 1, remote_version: null, is_deleted: false, deleted_at: null, created_at: 1, updated_at: 1,
  };
  const { result } = renderHook(() => useComposeRecipients({ composeMode: "reply-all", composeReplyTo: message, accounts, activeAccountId: null, restoredDraft: null }));
  expect(result.current.fromAccountId).toBe("b");
  expect(result.current.to).toEqual(["sender@example.invalid"]);
  expect(result.current.cc).toEqual(["cc@example.invalid"]);
});
