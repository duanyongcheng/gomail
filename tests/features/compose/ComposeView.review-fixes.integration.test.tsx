import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, type ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ComposeView from "@/features/compose/ComposeView";
import LocalDrafts from "@/features/compose/LocalDrafts";
import OutboxPanel from "@/features/compose/OutboxPanel";
import i18n from "@/lib/i18n";
import { useComposeStore } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";
import { useMailStore } from "@/stores/mail.store";
import { useToastStore } from "@/stores/toast.store";
import type { Account, DraftSummary, LocalDraft, OutboxItem, SaveDraftRequest, SendRequest } from "@/mygo";

// Real components, editor/draft hooks, translations, API adapters and generated
// bindings. Only the native call/event transport is synthetic. Responses below
// are fixtures of backend contracts, not an implementation of draft persistence.
const { native, listeners } = vi.hoisted(() => ({
  native: vi.fn<(method: string, request: unknown) => Promise<unknown>>(),
  listeners: new Map<string, Set<(payload: unknown) => void>>(),
}));
vi.mock("mygo-runtime", () => ({
  call: native, event: vi.fn(),
  on: (name: string, handler: (payload: unknown) => void) => {
    const handlers = listeners.get(name) ?? new Set();
    handlers.add(handler); listeners.set(name, handlers);
    return () => { handlers.delete(handler); };
  },
}));

const accounts: Account[] = ["a", "b"].map(id => ({
  id, email: `${id}@example.com`, display_name: id, provider: "gmail", color: null,
  account_label: null, provider_display_name: null, created_at: 1, updated_at: 1,
  sort_order: 0, capabilities: { read: true, modify_flags: true, send: true, html: true },
}));
const html = '<p>Imported body</p><table><tbody><tr><td>Keep table</td></tr></tbody></table><p><img src="cid:logo" alt="Logo"></p>';
const quote = "<blockquote>Original quote once</blockquote>";
const importedDraft: LocalDraft = {
  id: "linked-a", accountId: "a", subject: "Imported draft", to: ["to@example.com"],
  cc: ["cc@example.com"], bcc: ["hidden@example.com"], bodyText: "Imported body\nKeep table",
  bodyHtml: html, rawSource: html, editorMode: "html", quotedReplyHtml: quote,
  inReplyTo: "parent@example.com", replyMessageId: "local-parent",
  attachments: [{ name: "logo.png", path: "compose-attachment:logo", size: 12 }, { name: "note.txt", path: "compose-attachment:note", size: 4 }], updatedAt: 1,
};
const protectedRow: DraftSummary = {
  id: "linked-a", accountId: "a", subject: "Retained local draft", updatedAt: 1,
  attachmentCount: 2, cloudAvailable: true, cloudState: "sending",
};
const queuedItem: OutboxItem = {
  id: "send-a", accountId: "a", subject: "Queued snapshot", status: "queued",
  attempts: 0, nextRetryAt: null, lastError: "", createdAt: 1, cloudDraft: true,
};
const legacyError = "cannot cancel this older linked send: its local draft is unavailable; queued content was preserved";
let client: QueryClient;
let draft: LocalDraft;
let rows: DraftSummary[];
let outboxItems: OutboxItem[];
let listRows: (account: string) => Promise<DraftSummary[]>;
let outboxAction: () => Promise<unknown>;

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(res => { resolve = res; });
  return { promise, resolve };
}
function calls<T>(method: string): T[] {
  return native.mock.calls.filter(([name]) => name === method).map(([, request]) => request as T);
}
function emitOutboxChanged() {
  for (const handler of listeners.get("mail:outbox-changed") ?? []) handler({});
}
function ComposeHost() {
  const key = useComposeStore(s => s.composeKey);
  const mode = useComposeStore(s => s.composeMode);
  return mode ? <ComposeView key={key} /> : <div>Compose closed</div>;
}
function wrap(children: ReactNode) {
  return <StrictMode><QueryClientProvider client={client}>{children}</QueryClientProvider></StrictMode>;
}
function panels(account = "a") {
  return <><LocalDrafts accountId={account} disabled={false} /><OutboxPanel accountId={account} /></>;
}
async function ready() {
  await waitFor(() => expect(screen.getByRole("button", { name: i18n.t("compose.saveAndClose") })).toBeEnabled());
}
async function openLocal() {
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.localDrafts") }));
  return screen.findByRole("button", { name: "Retained local draft" });
}
async function openOutbox() {
  fireEvent.click(screen.getByText(new RegExp(`^${i18n.t("outbox.title")} \\(`)));
  await screen.findByText("Queued snapshot");
}
async function confirmCancel() {
  fireEvent.click(screen.getByRole("button", { name: i18n.t("outbox.cancel") }));
  expect(useConfirmStore.getState().message).toBe(i18n.t("outbox.cloudCancelWarning"));
  expect(calls("OutboxService.Action")).toHaveLength(0);
  await act(async () => { useConfirmStore.getState().handleConfirm(); });
}
async function importIntoCompose() {
  render(wrap(<ComposeHost />)); await ready();
  fireEvent.click(screen.getByRole("button", { name: i18n.t("cloudDrafts.browse") }));
  fireEvent.click(await screen.findByRole("button", { name: `${i18n.t("cloudDrafts.import")}: Imported draft` }));
  await screen.findByDisplayValue(draft.rawSource);
  await ready();
  expect(native).toHaveBeenCalledWith("ComposeService.CloudImport", { accountId: "a", remoteDraftId: "remote-draft", expectedMessageId: "remote-message" });
}

beforeEach(async () => {
  native.mockReset(); listeners.clear(); localStorage.clear();
  await i18n.changeLanguage("en");
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["accounts"], accounts);
  draft = importedDraft; rows = [protectedRow]; outboxItems = [queuedItem];
  listRows = async account => rows.filter(row => row.accountId === account);
  outboxAction = async () => ({});
  useMailStore.setState({ activeAccountId: "a" });
  useComposeStore.getState().openCompose("new");
  useConfirmStore.setState({ isOpen: false, resolve: null });
  useToastStore.setState({ toasts: [] });
  native.mockImplementation(async (method, request) => {
    const args = request as { accountId: string };
    switch (method) {
      case "AccountService.List": return accounts;
      case "AccountService.Signature": return "";
      case "ComposeService.List": return listRows(args.accountId);
      case "ComposeService.Get": return draft;
      case "ComposeService.CloudList": return { items: [{ id: "remote-draft", messageId: "remote-message", subject: "Imported draft", updatedAt: 1, localDraftId: "" }], nextPageToken: "" };
      case "ComposeService.CloudImport": return draft;
      case "ComposeService.Cloud": return { state: "synced" };
      case "ComposeService.Save": return (request as SaveDraftRequest).existingDraftId || "new-local";
      case "ComposeService.Delete":
      case "ComposeService.Cleanup":
      case "OutboxService.Queue": return {};
      case "OutboxService.List": return outboxItems.filter(item => item.accountId === args.accountId);
      case "OutboxService.Action": return outboxAction();
      default: throw new Error(`Unexpected synthetic transport: ${method}`);
    }
  });
});
afterEach(async () => { cleanup(); client.clear(); vi.useRealTimers(); await i18n.changeLanguage("en"); });

it.each(["en", "zh"])("keeps imported HTML intact after rejected rich/Markdown conversions when saving/uploading or sending (%s)", async language => {
  await i18n.changeLanguage(language);
  await importIntoCompose();
  for (const mode of ["rich", "markdown"]) {
    fireEvent.click(screen.getByRole("button", { name: i18n.t(`compose.mode.${mode}`) }));
    expect(screen.getByRole("alert")).toHaveTextContent(i18n.t("compose.mode.conversionBlocked"));
    expect(screen.getByRole("button", { name: "HTML" })).toHaveClass("is-active");
    expect(screen.getByDisplayValue(html)).toBeEnabled();
    expect(screen.getByRole("button", { name: i18n.t("compose.send") })).toBeEnabled();
    expect(screen.getByRole("button", { name: i18n.t("compose.saveAndClose") })).toBeEnabled();
    expect(screen.getByText("logo.png")).toBeInTheDocument();
    expect(calls("ComposeService.Save")).toHaveLength(0);
  }
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.saveAndClose") }));
  await screen.findByText("Compose closed");
  const expected = {
    accountId: "a", existingDraftId: "linked-a", editorMode: "html", rawSource: html,
    bodyHtml: html, bodyText: "Imported body\nKeep table", quotedReplyHtml: quote, attachmentPaths: ["compose-attachment:logo", "compose-attachment:note"],
    bcc: ["hidden@example.com"], inReplyTo: "parent@example.com", replyMessageId: "local-parent",
  };
  expect(calls<SaveDraftRequest>("ComposeService.Save")).toEqual([expect.objectContaining(expected)]);
  expect(calls("OutboxService.Queue")).toHaveLength(0);

  // Reopen via the native Get fixture, then explicitly upload the saved ID.
  // Backend tests, not this transport fixture, prove the bytes persist in SQLite.
  rows = [{ ...protectedRow, cloudState: "changed" }];
  act(() => useComposeStore.getState().openCompose("new")); await ready();
  await openLocal();
  fireEvent.click(screen.getByRole("button", { name: i18n.t("cloudDrafts.update") }));
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Cloud", { accountId: "a", draftId: "linked-a", action: "upload" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Retained local draft" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Retained local draft" }));
  await screen.findByDisplayValue(html); await ready();
  for (const mode of ["rich", "markdown"]) {
    fireEvent.click(screen.getByRole("button", { name: i18n.t(`compose.mode.${mode}`) }));
    expect(screen.getByRole("alert")).toHaveTextContent(i18n.t("compose.mode.conversionBlocked"));
    expect(screen.getByDisplayValue(html)).toHaveValue(html);
  }
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.send") }));
  await screen.findByText("Compose closed");
  expect(calls<SaveDraftRequest>("ComposeService.Save")).toEqual([expect.objectContaining(expected), expect.objectContaining(expected)]);
  expect(calls<SendRequest>("OutboxService.Queue")).toEqual([expect.objectContaining({
    accountId: "a", localDraftId: "linked-a", bodyHtml: `${html}<br/><br/>${quote}`, bodyText: expected.bodyText,
    attachmentPaths: expected.attachmentPaths, bcc: expected.bcc, inReplyTo: expected.inReplyTo,
  })]);
  expect(calls<SendRequest>("OutboxService.Queue")[0].bodyHtml.match(/Original quote once/g)).toHaveLength(1);
  const methods = native.mock.calls.map(([method]) => method);
  expect(methods.indexOf("OutboxService.Queue")).toBeLessThan(methods.indexOf("ComposeService.Delete"));
  expect(methods.lastIndexOf("ComposeService.Delete")).toBeLessThan(methods.lastIndexOf("ComposeService.Cleanup"));
  expect(calls("ComposeService.Cloud")).toEqual([{ accountId: "a", draftId: "linked-a", action: "upload" }]);
});

it("autosaves the untouched source after a refused conversion without scheduling another save for its alert", async () => {
  await importIntoCompose();
  vi.useFakeTimers();
  fireEvent.change(screen.getByLabelText(i18n.t("compose.subject")), { target: { value: "Latest subject" } });
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.mode.rich") }));
  await act(async () => { await vi.advanceTimersByTimeAsync(3100); });
  expect(calls<SaveDraftRequest>("ComposeService.Save")).toEqual([expect.objectContaining({
    subject: "Latest subject", rawSource: html, bodyHtml: html, editorMode: "html",
    quotedReplyHtml: quote, attachmentPaths: ["compose-attachment:logo", "compose-attachment:note"],
  })]);
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.mode.markdown") }));
  await act(async () => { await vi.advanceTimersByTimeAsync(6100); });
  expect(calls("ComposeService.Save")).toHaveLength(1);
  expect(screen.getByDisplayValue(html)).toHaveValue(html);
  expect(screen.getByRole("alert")).toHaveTextContent(i18n.t("compose.mode.conversionBlocked"));
  expect(calls("OutboxService.Queue")).toHaveLength(0);
});

it("still converts supported imported HTML through rich text, Markdown and HTML", async () => {
  const simple = "<p>Hello <strong>world</strong></p>";
  draft = { ...importedDraft, rawSource: simple, bodyHtml: simple };
  await importIntoCompose();
  for (const mode of ["rich", "markdown", "html"]) {
    const button = screen.getByRole("button", { name: i18n.t(`compose.mode.${mode}`) });
    fireEvent.click(button);
    expect(button).toHaveClass("is-active");
    expect(screen.queryByRole("alert")).toBeNull();
  }
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.saveAndClose") }));
  await screen.findByText("Compose closed");
  expect(calls<SaveDraftRequest>("ComposeService.Save")[0]).toMatchObject({ editorMode: "html", bodyHtml: expect.stringContaining(simple), quotedReplyHtml: quote });
});

it.each(["queued", "failed"])("refreshes the open local list from the native cancellation event for a %s linked send", async status => {
  outboxItems = [{ ...queuedItem, status }];
  const pending = deferred<unknown>(); outboxAction = () => pending.promise;
  render(wrap(panels()));
  expect(await openLocal()).toBeDisabled(); await openOutbox();
  await confirmCancel();
  expect(native).toHaveBeenCalledWith("OutboxService.Action", { accountId: "a", id: "send-a", action: "cancel" });
  expect(screen.getByRole("button", { name: "Retained local draft" })).toBeDisabled();
  const reads = calls("ComposeService.List").length;
  // Supply the post-transaction responses, then deliver the actual transport
  // event (no mocked service side effects or React Query local-draft shortcut).
  rows = [{ ...protectedRow, cloudState: "changed" }]; outboxItems = [];
  await act(async () => { emitOutboxChanged(); pending.resolve({}); });
  await waitFor(() => expect(screen.getByRole("button", { name: "Retained local draft" })).toBeEnabled());
  expect(calls("ComposeService.List")).toHaveLength(reads + 1);
  expect(screen.getByRole("button", { name: "Update Gmail copy" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Retained local draft" }));
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Get", { accountId: "a", draftId: "linked-a" }));
  expect(useComposeStore.getState().composePrefill?.localDraft).toEqual(importedDraft);
  expect(calls("ComposeService.Cloud")).toHaveLength(0);
});

it("removes accepted sends from the open local list after the native settlement event", async () => {
  render(wrap(panels())); expect(await openLocal()).toBeDisabled();
  rows = []; outboxItems = [{ ...queuedItem, status: "sent" }];
  act(emitOutboxChanged);
  await screen.findByText(i18n.t("compose.noLocalDrafts"));
  expect(screen.queryByRole("button", { name: "Retained local draft" })).toBeNull();
  expect(calls("ComposeService.Get")).toHaveLength(0);
});

it("ignores stale reloads, scopes global events to the current account and cleans up on close/unmount", async () => {
  const view = render(wrap(panels())); expect(await openLocal()).toBeDisabled();
  const stale = deferred<DraftSummary[]>();
  listRows = () => stale.promise;
  act(emitOutboxChanged);
  listRows = async account => account === "a" ? [{ ...protectedRow, cloudState: "changed" }] : [];
  act(emitOutboxChanged);
  await waitFor(() => expect(screen.getByRole("button", { name: "Retained local draft" })).toBeEnabled());
  await act(async () => { stale.resolve([protectedRow]); });
  expect(screen.getByRole("button", { name: "Retained local draft" })).toBeEnabled();

  const oldAccount = deferred<DraftSummary[]>(); listRows = () => oldAccount.promise;
  act(emitOutboxChanged);
  listRows = async () => [];
  view.rerender(wrap(panels("b")));
  await screen.findByText(i18n.t("compose.noLocalDrafts"));
  await act(async () => { oldAccount.resolve([protectedRow]); });
  expect(screen.queryByText("Retained local draft")).toBeNull();
  const beforeEvent = calls("ComposeService.List").length;
  act(emitOutboxChanged);
  await waitFor(() => expect(calls("ComposeService.List")).toHaveLength(beforeEvent + 1));
  expect(calls("ComposeService.List").at(-1)).toEqual({ accountId: "b" });

  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.localDrafts") }));
  await act(async () => {});
  expect(listeners.get("mail:outbox-changed")?.size).toBe(1); // Outbox only
  const closedReads = calls("ComposeService.List").length;
  act(emitOutboxChanged);
  expect(calls("ComposeService.List")).toHaveLength(closedReads);
  const afterUnmount = deferred<DraftSummary[]>(); listRows = () => afterUnmount.promise;
  fireEvent.click(screen.getByRole("button", { name: i18n.t("compose.localDrafts") }));
  const pendingReads = calls("ComposeService.List").length;
  expect(pendingReads).toBe(closedReads + 1);
  view.unmount();
  await act(async () => { afterUnmount.resolve([protectedRow]); });
  expect(listeners.get("mail:outbox-changed")?.size).toBe(0);
  act(emitOutboxChanged);
  expect(calls("ComposeService.List")).toHaveLength(pendingReads);
});

it.each(["en", "zh"])("explains refused legacy cancellation without claiming success (%s)", async language => {
  await i18n.changeLanguage(language);
  outboxAction = async () => { throw new Error(legacyError); };
  render(wrap(panels())); expect(await openLocal()).toBeDisabled(); await openOutbox();
  await confirmCancel();
  expect(await screen.findByRole("alert")).toHaveTextContent(i18n.t("outbox.legacyCancelUnavailable"));
  expect(screen.getByRole("button", { name: "Retained local draft" })).toBeDisabled();
  expect(screen.getByText("Queued snapshot")).toBeInTheDocument();
  expect(useToastStore.getState().toasts).toHaveLength(0);
  expect(calls("ComposeService.Cloud")).toHaveLength(0);
});

it.each([legacyError, { message: legacyError }])("recognizes only the exact legacy message across native error shapes (%j)", async error => {
  outboxAction = async () => { throw error; };
  render(wrap(panels())); await openOutbox(); await confirmCancel();
  expect(await screen.findByRole("alert")).toHaveTextContent(i18n.t("outbox.legacyCancelUnavailable"));
});

it.each([
  new Error("private-token https://private.invalid"),
  `prefix: ${legacyError}`,
  { message: `${legacyError}; private-token` },
  null,
])("keeps nonmatching cancellation errors generic (%j)", async error => {
  outboxAction = async () => { throw error; };
  render(wrap(panels())); await openOutbox(); await confirmCancel();
  expect(await screen.findByRole("alert")).toHaveTextContent(i18n.t("outbox.actionFailed"));
  expect(screen.queryByText(/private-token|private\.invalid|prefix:/)).toBeNull();
  expect(screen.queryByText(i18n.t("outbox.legacyCancelUnavailable"))).toBeNull();
  expect(useToastStore.getState().toasts).toHaveLength(0);
});
