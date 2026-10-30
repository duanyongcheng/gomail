import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Editor } from "@tiptap/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ComposeView from "@/features/compose/ComposeView";
import { useComposeStore } from "@/stores/compose.store";
import { useMailStore } from "@/stores/mail.store";
import { useUIStore } from "@/stores/ui.store";
import type { Account, LocalDraft, SaveDraftRequest } from "@/mygo";

// Keep ComposeView, LocalDrafts, TipTap, recipients, draft persistence, command
// adapters and generated bindings real. Only native transport is synthetic.
const { native } = vi.hoisted(() => ({ native: vi.fn<(method: string, request: unknown) => Promise<unknown>>() }));
vi.mock("mygo-runtime", () => ({ call: native, event: vi.fn(), on: () => () => {} }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));

const accounts: Account[] = ["a", "b"].map(id => ({
  id, email: `${id}@example.com`, display_name: id, provider: "gmail", color: null,
  account_label: null, provider_display_name: null, created_at: 1, updated_at: 1,
  sort_order: 0, capabilities: { read: true, modify_flags: true, send: true, html: true },
}));
const savedDraft: LocalDraft = {
  id: "saved", accountId: "a", subject: "Saved draft", to: ["recipient@example.com"], cc: [], bcc: [],
  bodyText: "Saved body", bodyHtml: '<p>Saved body</p><p data-pebble-signature="auto">Old saved signature</p>',
  rawSource: "", editorMode: "rich", inReplyTo: "", quotedReplyHtml: "<blockquote>Saved quote</blockquote>",
  attachments: [{ name: "saved.txt", size: 5, path: "compose-attachment:saved" }], updatedAt: 1,
};
let saves: SaveDraftRequest[];
let readSignature: (account: string) => Promise<string>;
let readDraft: () => Promise<LocalDraft>;
let stageAttachment: () => Promise<string>;
let failSave: boolean;
let client: QueryClient;

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}
function ComposeHost() {
  const key = useComposeStore(s => s.composeKey);
  const mode = useComposeStore(s => s.composeMode);
  // This is the production Layout key/remount boundary, not a mocked restore.
  return mode ? <ComposeView key={key} /> : <div>Compose closed</div>;
}
function mount() {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["accounts"], accounts);
  return render(<StrictMode><QueryClientProvider client={client}><ComposeHost /></QueryClientProvider></StrictMode>);
}
async function ready() {
  await waitFor(() => expect(screen.getByRole("button", { name: "Save and close" })).toBeEnabled());
}
function editor(): Editor {
  // TipTap exposes the real editor on its mounted DOM root.
  const element = document.querySelector(".tiptap") as HTMLElement & { editor: Editor };
  return element.editor;
}
async function expandDrafts() {
  fireEvent.click(screen.getByRole("button", { name: "Local drafts" }));
  return screen.findByRole("button", { name: "Saved draft" });
}
function expectNoReplacement() {
  fireEvent.click(screen.getByRole("button", { name: "Saved draft" }));
  expect(native.mock.calls.filter(([method]) => method === "ComposeService.Get")).toHaveLength(0);
}
async function autosave() {
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 3100)); });
}

beforeEach(() => {
  native.mockReset();
  localStorage.clear();
  saves = [];
  failSave = false;
  readSignature = async account => `Signature ${account}`;
  readDraft = async () => savedDraft;
  stageAttachment = async () => "compose-attachment:staged";
  native.mockImplementation(async (method, request) => {
    const args = request as { accountId: string };
    switch (method) {
      case "AccountService.Signature": return readSignature(args.accountId);
      case "AccountService.List": return accounts;
      case "OutboxService.List": return [];
      case "ComposeService.List": return [{ id: "saved", accountId: args.accountId, subject: "Saved draft", updatedAt: 1, attachmentCount: 1, cloudState: "synced", cloudAvailable: true }];
      case "ComposeService.Get": return readDraft();
      case "ComposeService.Cloud": return { state: "synced" };
      case "ComposeService.Stage": return stageAttachment();
      case "ComposeService.Cleanup": return {};
      case "ComposeService.Save": {
        const save = request as SaveDraftRequest;
        saves.push(save);
        if (failSave) throw new Error("Synthetic save failure");
        return save.existingDraftId || "auto-only";
      }
      default: throw new Error(`Unexpected native call: ${method}`);
    }
  });
  useMailStore.setState({ activeAccountId: "a" });
  useUIStore.setState({ activeView: "inbox", previousView: "inbox" });
  useComposeStore.getState().openCompose("new");
});
afterEach(() => { cleanup(); client?.clear(); vi.useRealTimers(); });

it("restores a saved draft with a nonempty signature, preserves the saved signature/attachments and saves the same ID", async () => {
  mount();
  await ready();
  expect(editor().getText()).toContain("Signature a");
  const restore = await expandDrafts();
  expect(restore).toBeEnabled();
  expect(screen.getByRole("button", { name: "Update Gmail copy" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Check Gmail copy" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Delete: Saved draft" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Check Gmail copy" }));
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Cloud", { accountId: "a", draftId: "saved", action: "check" }));
  await waitFor(() => expect(restore).toBeEnabled());
  const signatureReads = native.mock.calls.filter(([method]) => method === "AccountService.Signature").length;
  fireEvent.click(restore);
  await waitFor(() => expect(editor().getText()).toContain("Saved body"));
  expect(editor().getText()).toContain("Old saved signature");
  expect(editor().getText()).not.toContain("Signature a");
  expect(native.mock.calls.filter(([method]) => method === "AccountService.Signature")).toHaveLength(signatureReads);
  expect(screen.getByText("saved.txt")).toBeInTheDocument();
  expect(screen.getByText("recipient@example.com")).toBeInTheDocument();
  expect(await expandDrafts()).toBeDisabled();
  act(() => { editor().commands.insertContentAt(1, "Edited "); });
  fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
  await screen.findByText("Compose closed");
  const lastSave = saves[saves.length - 1];
  expect(lastSave).toMatchObject({ accountId: "a", existingDraftId: "saved", to: ["recipient@example.com"], attachmentPaths: ["compose-attachment:saved"], quotedReplyHtml: "<blockquote>Saved quote</blockquote>" });
  expect(lastSave.bodyHtml).toContain("Edited Saved body");
  expect(lastSave.bodyHtml).toContain("Old saved signature");
});

it.each(["body", "recipient", "staging"])("does not replace a %s edit made while a saved draft is loading", async kind => {
  const loading = deferred<LocalDraft>();
  const staging = deferred<string>();
  readDraft = () => loading.promise;
  stageAttachment = () => staging.promise;
  mount(); await ready();
  fireEvent.click(await expandDrafts());
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Get", { accountId: "a", draftId: "saved" }));
  const originalKey = useComposeStore.getState().composeKey;
  if (kind === "body") {
    act(() => { editor().commands.insertContentAt(1, "Unsaved during restore"); });
  } else if (kind === "recipient") {
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "pending@example.com" } });
  } else {
    const file = new File(["content"], "in-flight.txt");
    Object.defineProperty(file, "arrayBuffer", { value: async () => new TextEncoder().encode("content").buffer });
    fireEvent.change(document.querySelector('input[type="file"]')!, { target: { files: [file] } });
    await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Stage", expect.objectContaining({ filename: "in-flight.txt" })));
  }
  await act(async () => { loading.resolve(savedDraft); });
  expect(useComposeStore.getState().composeKey).toBe(originalKey);
  expect(editor().getText()).not.toContain("Saved body");
  if (kind === "body") expect(editor().getText()).toContain("Unsaved during restore");
  if (kind === "recipient") expect(screen.getByLabelText("To")).toHaveValue("pending@example.com");
  if (kind === "staging") {
    await act(async () => { staging.resolve("compose-attachment:staged"); });
    expect(screen.getByText("in-flight.txt")).toBeInTheDocument();
  }
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
});

it("keeps pristine mode/account conversions available without deleting an auto-only saved snapshot", async () => {
  mount(); await ready();
  await autosave();
  expect(saves).toHaveLength(1);
  expect(saves[0].bodyHtml).toContain("Signature a");
  await expandDrafts();
  for (const mode of ["Markdown", "HTML", "Rich Text"]) {
    fireEvent.click(screen.getByRole("button", { name: mode }));
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled();
  }
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  expect(editor().getText()).toContain("Signature b");
  await waitFor(() => expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled());
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "a" } });
  await ready();
  fireEvent.click(await screen.findByRole("button", { name: "Saved draft" }));
  await waitFor(() => expect(editor().getText()).toContain("Saved body"));
  expect(native.mock.calls.filter(([method]) => method === "ComposeService.Delete")).toHaveLength(0);
  expect(saves[0].bodyHtml).toContain("Signature a");
});

it.each(["body", "signature", "divider"])("protects a real %s edit, including marker-preserving and non-text changes", async kind => {
  mount(); await ready(); await expandDrafts();
  act(() => {
    const current = editor();
    if (kind === "divider") current.commands.insertContentAt(0, { type: "horizontalRule" });
    else if (kind === "body") current.commands.insertContentAt(1, "Signature a is also my body");
    else current.commands.insertContentAt(current.state.doc.content.size - 1, " edited signature");
  });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
  await autosave();
  expect(saves).toHaveLength(1);
  if (kind === "signature") {
    expect(saves[0].bodyHtml).toContain("edited signature");
    expect(saves[0].bodyHtml).toContain('data-pebble-signature="auto"');
  } else if (kind === "divider") expect(saves[0].bodyHtml).toContain("<hr>");
  else expect(saves[0].bodyHtml).toContain("Signature a is also my body");
  for (const mode of ["Markdown", "HTML", "Rich Text"]) {
    fireEvent.click(screen.getByRole("button", { name: mode }));
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  }
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  await waitFor(() => expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled());
  expectNoReplacement();
});

it.each(["Markdown", "HTML"])("protects edited %s source through account and mode switches", async mode => {
  mount(); await ready(); await expandDrafts();
  fireEvent.click(screen.getByRole("button", { name: mode }));
  const source = screen.getByRole("textbox", { name: "" }) as HTMLTextAreaElement;
  const edited = source.value.replace("Signature a", "User edited signature");
  fireEvent.change(source, { target: { value: edited } });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
  await autosave();
  expect(saves[0].rawSource).toBe(edited);
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Rich Text" }));
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
});

it.each(["To", "Cc", "Bcc", "Subject"])("protects pending %s input and persists it rather than replacing it", async field => {
  mount(); await ready(); await expandDrafts();
  if (field === "Cc" || field === "Bcc") fireEvent.click(screen.getByRole("button", { name: field }));
  fireEvent.change(screen.getByLabelText(field), { target: { value: "unfinished" } });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
  await autosave();
  expect(saves[0]).toMatchObject({ [field.toLowerCase()]: field === "Subject" ? "unfinished" : ["unfinished"] });
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  expect(screen.getByLabelText(field)).toHaveValue("unfinished");
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
});

it("protects pending attachment staging and retains staged references in local saves", async () => {
  const staged = deferred<string>(); stageAttachment = () => staged.promise;
  mount(); await ready(); await expandDrafts();
  const file = new File(["content"], "note.txt");
  Object.defineProperty(file, "arrayBuffer", { value: async () => new TextEncoder().encode("content").buffer });
  fireEvent.change(document.querySelector('input[type="file"]')!, { target: { files: [file] } });
  await waitFor(() => expect(native).toHaveBeenCalledWith("ComposeService.Stage", expect.objectContaining({ filename: "note.txt" })));
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
  await act(async () => { staged.resolve("compose-attachment:staged"); });
  expect(screen.getByText("note.txt")).toBeInTheDocument();
  // Remove the auto-filled subject so the attachment itself must keep the gate.
  fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "" } });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  await autosave();
  expect(saves[0].attachmentPaths).toEqual(["compose-attachment:staged"]);
  expect(saves[0].bodyHtml).toContain("Signature a");
});

it("keeps signature read errors blocked through retry and cannot save or switch a pending editor", async () => {
  const pending = deferred<string>(); readSignature = () => pending.promise;
  mount(); await expandDrafts();
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save and close" })).toBeDisabled();
  expectNoReplacement();
  await act(async () => { pending.reject(new Error("Synthetic locked signature")); });
  expect(await screen.findByText("Could not load the signature. Retry before editing or sending.")).toBeInTheDocument();
  await autosave(); expect(saves).toHaveLength(0);
  readSignature = async () => "Recovered signature";
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await ready();
  expect(editor().getText()).toContain("Recovered signature");
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled();
});

it("does not classify prefilled text as automatic even when it matches the signature", async () => {
  useComposeStore.getState().openCompose("new", null, { body: "Signature a" });
  mount(); await ready();
  expect(await expandDrafts()).toBeDisabled();
  expect(editor().getText().match(/Signature a/g)).toHaveLength(2);
  expectNoReplacement();
});

it("protects a restored signature-only draft, including after From and mode changes", async () => {
  useComposeStore.getState().openCompose("new", null, { localDraft: {
    ...savedDraft, to: [], subject: "", attachments: [], quotedReplyHtml: "",
    bodyHtml: '<p></p><p data-pebble-signature="auto">Signature a</p>',
  } });
  mount(); await ready();
  expect(await expandDrafts()).toBeDisabled();
  expect(native.mock.calls.some(([method]) => method === "AccountService.Signature")).toBe(false);
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Markdown" }));
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
});

it("keeps legacy restored content protected until it is explicitly saved", async () => {
  localStorage.setItem("pebble-compose-draft", JSON.stringify({
    accountId: "a", to: [], cc: [], bcc: [], subject: "", rawSource: "",
    richTextHtml: '<p data-pebble-signature="auto">Legacy signature</p>',
    attachments: [], editorMode: "rich", savedAt: Date.now(),
  }));
  mount(); await ready();
  expect(await expandDrafts()).toBeDisabled();
  expect(editor().getText()).toContain("Legacy signature");
  expectNoReplacement();
  fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
  await screen.findByText("Compose closed");
  expect(saves[0].bodyHtml).toContain("Legacy signature");
  expect(localStorage.getItem("pebble-compose-draft")).toBeNull();
});

it("keeps an empty-signature baseline usable across account changes and an exact undo", async () => {
  readSignature = async account => account === "b" ? "" : "Signature a";
  mount(); await ready(); await expandDrafts();
  const original = editor().getHTML();
  act(() => { editor().commands.insertContentAt(1, "Temporary body"); });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  act(() => { editor().commands.setContent(original); });
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled();
  for (const mode of ["Markdown", "HTML", "Rich Text"]) {
    fireEvent.click(screen.getByRole("button", { name: mode }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
    await ready();
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled();
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "a" } });
    await ready();
    expect(screen.getByRole("button", { name: "Saved draft" })).toBeEnabled();
  }
});

it("does not bypass save failure or discard an edited signature when save-and-close fails", async () => {
  mount(); await ready(); await expandDrafts();
  act(() => { const current = editor(); current.commands.insertContentAt(current.state.doc.content.size - 1, " retain me"); });
  failSave = true;
  vi.useFakeTimers();
  fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
  await act(async () => { await vi.advanceTimersByTimeAsync(7100); });
  vi.useRealTimers();
  await waitFor(() => expect(screen.getByRole("button", { name: "Save and close" })).toBeEnabled());
  expect(editor().getText()).toContain("retain me");
  expect(screen.getByRole("button", { name: "Saved draft" })).toBeDisabled();
  expectNoReplacement();
  expect(saves.length).toBeGreaterThanOrEqual(4);
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Delete" || method === "ComposeService.Cleanup")).toBe(false);
});
