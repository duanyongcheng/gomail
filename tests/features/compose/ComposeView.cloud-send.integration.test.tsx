import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ComposeView from "@/features/compose/ComposeView";
import { useComposeStore } from "@/stores/compose.store";
import { useMailStore } from "@/stores/mail.store";
import type { Account, LocalDraft, SaveDraftRequest, SendRequest } from "@/mygo";

const { native } = vi.hoisted(() => ({ native: vi.fn<(method: string, args: unknown) => Promise<unknown>>() }));
vi.mock("mygo-runtime", () => ({ call: native, event: vi.fn(), on: () => () => {} }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));

const accounts: Account[] = ["a", "b"].map(id => ({
  id, email: `${id}@example.com`, display_name: id, provider: "gmail", color: null,
  account_label: null, provider_display_name: null, created_at: 1, updated_at: 1,
  sort_order: 0, capabilities: { read: true, modify_flags: true, send: true, html: true },
}));
const draft: LocalDraft = {
  id: "linked-a", accountId: "a", to: ["to@example.com"], cc: ["cc@example.com"], bcc: ["bcc@example.com"],
  subject: "Linked cloud draft", bodyText: "Saved body", bodyHtml: "<p>Saved body</p>", rawSource: "",
  editorMode: "rich", inReplyTo: "parent@example.com", quotedReplyHtml: "<blockquote>Old quote</blockquote>",
  attachments: [{ name: "attachment.txt", path: "compose-attachment:saved", size: 12 }], updatedAt: 1,
};
let client: QueryClient;
let save: (request: SaveDraftRequest) => Promise<string>;
let queue: (request: SendRequest) => Promise<unknown>;
let saves: SaveDraftRequest[];
let sends: SendRequest[];

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}
function ComposeHost() {
  const key = useComposeStore(s => s.composeKey);
  const mode = useComposeStore(s => s.composeMode);
  return mode ? <ComposeView key={key} /> : <div>Closed after queue</div>;
}
function mount() {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["accounts"], accounts);
  return render(<StrictMode><QueryClientProvider client={client}><ComposeHost /></QueryClientProvider></StrictMode>);
}
async function ready() {
  await waitFor(() => expect(screen.getByRole("button", { name: "Send" })).toBeEnabled());
}
beforeEach(() => {
  native.mockReset();
  localStorage.clear();
  saves = []; sends = [];
  save = async request => request.existingDraftId || `local-${request.accountId}`;
  queue = async () => ({});
  native.mockImplementation(async (method, args) => {
    switch (method) {
      case "AccountService.List": return accounts;
      case "AccountService.Signature": return "";
      case "OutboxService.List": return [];
      case "ComposeService.List": return [];
      case "ComposeService.Save": {
        const request = args as SaveDraftRequest;
        saves.push(request);
        return save(request);
      }
      case "OutboxService.Queue": {
        const request = args as SendRequest;
        sends.push(request);
        return queue(request);
      }
      case "ComposeService.Delete":
      case "ComposeService.Cleanup": return {};
      default: throw new Error(`Unexpected synthetic transport: ${method}`);
    }
  });
  useMailStore.setState({ activeAccountId: "a" });
  useComposeStore.getState().openCompose("new", null, { localDraft: draft });
});
afterEach(() => { cleanup(); client?.clear(); vi.useRealTimers(); });

it("flushes the actual restored draft before queueing its cloud-linked send and never deletes remotely from the UI", async () => {
  const pendingSave = deferred<string>();
  save = () => pendingSave.promise;
  mount(); await ready();
  fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Final subject" } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(saves).toHaveLength(1));
  expect(sends).toHaveLength(0);
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Delete")).toBe(false);
  expect(saves[0]).toMatchObject({
    accountId: "a", existingDraftId: "linked-a", subject: "Final subject",
    bcc: ["bcc@example.com"], attachmentPaths: ["compose-attachment:saved"],
    quotedReplyHtml: "<blockquote>Old quote</blockquote>",
  });
  await act(async () => { pendingSave.resolve("linked-a"); });
  await screen.findByText("Closed after queue");
  expect(sends).toHaveLength(1);
  expect(sends[0]).toMatchObject({
    accountId: "a", localDraftId: "linked-a", subject: "Final subject",
    to: ["to@example.com"], cc: ["cc@example.com"], bcc: ["bcc@example.com"],
    inReplyTo: "parent@example.com", attachmentPaths: ["compose-attachment:saved"],
  });
  expect(sends[0].bodyHtml.match(/Old quote/g)).toHaveLength(1);
  expect(native).toHaveBeenCalledWith("ComposeService.Delete", { accountId: "a", draftId: "linked-a" });
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Cloud")).toBe(false);
});

it("saves and sends rendered Markdown while retaining exact source and the quote separately", async () => {
  const source = "# Heading\n\n**Strong** and [link](https://example.invalid)";
  useComposeStore.getState().openCompose("new", null, { localDraft: {
    ...draft, editorMode: "markdown", rawSource: "Older source", bodyHtml: "",
  } });
  mount(); await ready();
  fireEvent.change(screen.getByPlaceholderText("Write in Markdown..."), { target: { value: source } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByText("Closed after queue");
  expect(saves[0]).toMatchObject({ rawSource: source, renderedSource: source, editorMode: "markdown" });
  expect(saves[0].bodyHtml).toContain("<h1>Heading</h1>");
  expect(saves[0].bodyHtml).toContain("<strong>Strong</strong>");
  expect(saves[0].bodyHtml).not.toContain("Old quote");
  expect(sends[0].bodyHtml).toContain("<strong>Strong</strong>");
  expect(sends[0].bodyHtml.match(/Old quote/g)).toHaveLength(1);
  expect(sends[0].bodyText).not.toContain("**");
  expect(sends[0].localDraftId).toBe("linked-a");
});

it("switches local draft identity when the From account changes", async () => {
  mount(); await ready();
  fireEvent.change(screen.getByLabelText("From"), { target: { value: "b" } });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByText("Closed after queue");
  expect(saves[0]).toMatchObject({ accountId: "b", existingDraftId: "" });
  expect(sends[0]).toMatchObject({ accountId: "b", localDraftId: "local-b" });
  expect(native).not.toHaveBeenCalledWith("ComposeService.Delete", { accountId: "a", draftId: "linked-a" });
});

it("retains the editable source, attachment and saved draft after queue failure", async () => {
  queue = async () => { throw new Error("Synthetic queue failure"); };
  useComposeStore.getState().openCompose("new", null, { localDraft: {
    ...draft, editorMode: "markdown", rawSource: "**Keep source**", bodyHtml: "",
  } });
  mount(); await ready();
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByText("Synthetic queue failure");
  expect(screen.getByPlaceholderText("Write in Markdown...")).toHaveValue("**Keep source**");
  expect(screen.getByText("attachment.txt")).toBeInTheDocument();
  expect(native.mock.calls.some(([method]) => method === "ComposeService.Delete" || method === "ComposeService.Cleanup")).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
  await screen.findByText("Closed after queue");
  expect(saves).toHaveLength(2);
  expect(saves[1]).toMatchObject({ existingDraftId: "linked-a", renderedSource: "**Keep source**" });
  expect(sends).toHaveLength(1);
});
