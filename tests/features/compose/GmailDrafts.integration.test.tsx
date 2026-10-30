import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import LocalDrafts from "@/features/compose/LocalDrafts";
import { useComposeStore } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";
import { useUIStore } from "@/stores/ui.store";
import type { LocalDraft } from "@/mygo";

const { native } = vi.hoisted(() => ({ native: vi.fn<(method: string, request: unknown) => Promise<unknown>>() }));
vi.mock("mygo-runtime", () => ({ call: native, event: vi.fn(), on: () => () => {} }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));

const draft: LocalDraft = {
  id: "imported-local", accountId: "account-a", subject: "Web draft",
  to: ["to@example.com"], cc: ["cc@example.com"], bcc: ["hidden@example.com"],
  bodyText: "Keep text", bodyHtml: "<p>Keep text</p>", inReplyTo: "parent@example.com",
  quotedReplyHtml: "", editorMode: "rich", rawSource: "",
  attachments: [{ name: "note.txt", path: "compose-attachment:opaque", size: 4 }], updatedAt: 1,
};
let cloudState: string;

beforeEach(() => {
  native.mockReset();
  cloudState = "synced";
  useUIStore.setState({ activeView: "compose", previousView: "inbox" });
  useComposeStore.setState({ composeMode: "new", composePrefill: null, composeDirty: false, composeKey: 0 });
  useConfirmStore.setState({ isOpen: false, resolve: null });
  native.mockImplementation(async (method, request) => {
    const args = request as { accountId: string; pageToken?: string; action?: string };
    switch (method) {
      case "ComposeService.List": return [{ id: "original-local", accountId: args.accountId, subject: "Local content", updatedAt: 1, attachmentCount: 0, cloudAvailable: true, cloudState }];
      case "ComposeService.CloudList": return {
        items: [{ id: args.pageToken ? "remote-2" : "remote-1", messageId: args.pageToken ? "message-2" : "message-1", subject: args.pageToken ? "Other web draft" : "Web draft", updatedAt: 1, localDraftId: "" }],
        nextPageToken: args.pageToken ? "" : "page-2",
      };
      case "ComposeService.CloudImport": return draft;
      case "ComposeService.Get": return draft;
      case "ComposeService.Cloud":
        if (args.action === "delete") { cloudState = "local"; return { state: "local" }; }
        if (args.action === "import-remote") return { state: "synced", draftId: "imported-local" };
        return { state: cloudState };
      default: throw new Error(`Unexpected native method: ${method}`);
    }
  });
});
afterEach(cleanup);

it("keeps local browsing offline, then uses typed list/import services and restores all imported content", async () => {
  render(<LocalDrafts accountId="account-a" disabled={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
  await screen.findByRole("button", { name: "Local content" });
  expect(native.mock.calls.map(([method]) => method)).toEqual(["ComposeService.List"]);
  fireEvent.click(screen.getByRole("button", { name: "Gmail drafts" }));
  fireEvent.click(await screen.findByRole("button", { name: "Load more drafts" }));
  await screen.findByRole("button", { name: "Import draft: Other web draft" });
  expect(native).toHaveBeenCalledWith("ComposeService.CloudList", { accountId: "account-a", pageToken: "page-2" });
  fireEvent.click(screen.getByRole("button", { name: "Import draft: Web draft" }));
  await waitFor(() => expect(useComposeStore.getState().composePrefill?.localDraft).toEqual(draft));
  expect(native).toHaveBeenCalledWith("ComposeService.CloudImport", { accountId: "account-a", remoteDraftId: "remote-1", expectedMessageId: "message-1" });
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Cloud" || method === "ComposeService.Delete")).toBe(false);
});

it("sends permanent-delete intent only after the real confirmation resolves and leaves local delete untouched", async () => {
  render(<LocalDrafts accountId="account-a" disabled={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
  fireEvent.click(await screen.findByRole("button", { name: "Delete Gmail copy" }));
  expect(useConfirmStore.getState()).toMatchObject({ isOpen: true, destructive: true, title: "Delete Gmail draft permanently?" });
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Cloud")).toBe(false);
  act(() => useConfirmStore.getState().handleConfirm());
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Cloud", { accountId: "account-a", draftId: "original-local", action: "delete" }));
  await screen.findByRole("button", { name: "Upload to Gmail" });
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Delete")).toBe(false);
});

it("uses an explicit non-overwrite recovery action and fetches the backend's newly preserved local copy", async () => {
  cloudState = "conflict";
  render(<LocalDrafts accountId="account-a" disabled={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
  fireEvent.click(await screen.findByRole("button", { name: "Keep both versions" }));
  expect(useConfirmStore.getState().message).toContain("Nothing in Gmail is overwritten");
  act(() => useConfirmStore.getState().handleConfirm());
  await waitFor(() => expect(useComposeStore.getState().composePrefill?.localDraft).toEqual(draft));
  expect(native).toHaveBeenCalledWith("ComposeService.Cloud", { accountId: "account-a", draftId: "original-local", action: "import-remote" });
  expect(native).toHaveBeenCalledWith("ComposeService.Get", { accountId: "account-a", draftId: "imported-local" });
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Delete")).toBe(false);
});

it("opens a Drafts-folder request once under StrictMode and does not browse another account on selection change", async () => {
  const view = render(<StrictMode><LocalDrafts accountId="account-a" disabled={false} initialGmailAccountId="account-a" /></StrictMode>);
  await screen.findByRole("button", { name: "Import draft: Web draft" });
  expect(native.mock.calls.filter(([method]) => method === "ComposeService.CloudList")).toEqual([["ComposeService.CloudList", { accountId: "account-a", pageToken: "" }]]);
  view.rerender(<StrictMode><LocalDrafts accountId="account-b" disabled={false} initialGmailAccountId="account-a" /></StrictMode>);
  expect(screen.queryByText("Web draft")).toBeNull();
  expect(native.mock.calls.filter(([method]) => method === "ComposeService.CloudList")).toHaveLength(1);
});
