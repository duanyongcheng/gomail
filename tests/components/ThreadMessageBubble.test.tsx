import { act, cleanup, fireEvent, render as renderRaw, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ThreadMessageBubble from "../../src/components/ThreadMessageBubble";
import type { Message } from "../../src/lib/api";
import { getRenderedHtml, getMessage, updateMessageFlags } from "../../src/lib/api";
import { getMessageTheme } from "../../src/lib/messageThemes";
const mocks = vi.hoisted(() => ({ openCompose: vi.fn(), invalidate: vi.fn(), changed: undefined as undefined | ((event: { payload: { accountId: string; email: string } }) => void) }));
vi.mock("@tauri-apps/api/event", () => ({ listen: (_name: string, callback: typeof mocks.changed) => { mocks.changed = callback; return Promise.resolve(() => {}); } }));
vi.mock("../../src/stores/compose.store", () => ({ useComposeStore: { getState: () => ({ openCompose: mocks.openCompose }) } }));

vi.mock("../../src/components/AttachmentList", () => ({ default: () => <div>Attachments</div> }));

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

vi.mock("@/mygo", () => ({
  AIService: {
    getConfig: vi.fn().mockResolvedValue({ configured: false, enabled: false, endpointHost: "", isLocalEndpoint: false, keyringError: false }),
    getTranslationCache: vi.fn().mockResolvedValue({ found: false, subject: "", segments: {}, model: "", generatedAt: 0 }),
    translateBatch: vi.fn(),
    saveTranslationCache: vi.fn(),
    cancel: vi.fn().mockResolvedValue({}),
  },
}));

vi.mock("../../src/lib/api", () => ({
  getMessage: vi.fn(), updateMessageFlags: vi.fn().mockResolvedValue(undefined),
  getRenderedHtml: vi.fn().mockResolvedValue({
    html: "<p>Rendered</p>",
    trackers_blocked: [],
    images_blocked: 0,
  }),
}));

vi.mock("../../src/components/ShadowDomEmail", () => ({
  ShadowDomEmail: ({ html, theme }: { html: string; theme?: { id: string } }) => <div data-testid="thread-shadow" data-message-template={theme?.id}>{html}</div>,
}));

vi.mock("../../src/components/ContactAddressAction", () => ({
  default: ({ address }: { address: string }) => (
    <span data-testid="contact-address-action">{address}</span>
  ),
}));

const message: Message = {
  id: "message-1",
  account_id: "account-1",
  remote_id: "remote-1",
  message_id_header: null,
  in_reply_to: null,
  references_header: null,
  thread_id: "thread-1",
  subject: "Thread message",
  snippet: "Snippet",
  from_address: "sender@example.com",
  from_name: "Sender",
  to_list: [{ name: null, address: "user@example.com" }],
  cc_list: [{ name: null, address: "cc@example.com" }],
  bcc_list: [],
  has_attachments: false,
  is_read: true,
  is_starred: false,
  is_draft: false,
  date: 1_700_000_000,
  remote_version: null,
  is_deleted: false,
  deleted_at: null,
  created_at: 1_700_000_000,
  updated_at: 1_700_000_000,
  body_text: "Body",
  body_html_raw: "<p>Body</p>",
};

let client: QueryClient;
function render(element: ReactElement) {
  return renderRaw(element, { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
}
beforeEach(() => {
  vi.clearAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  vi.spyOn(client, "invalidateQueries");
  vi.mocked(updateMessageFlags).mockResolvedValue(undefined);
  vi.mocked(getMessage).mockImplementation(async (id) => ({ ...message, id }));
  vi.mocked(getRenderedHtml).mockResolvedValue({ html: "<p>Rendered</p>", trackers_blocked: [], images_blocked: 0 });
});
afterEach(() => { cleanup(); client.clear(); });

describe("ThreadMessageBubble", () => {
  it("passes interface reading typography to expanded messages without changing read state", async () => {
    render(<ThreadMessageBubble message={message} defaultExpanded theme={getMessageTheme("app")} />);
    await screen.findByText("<p>Rendered</p>");
    expect(screen.getByTestId("thread-shadow").getAttribute("data-message-template")).toBe("app");
    expect(document.querySelector<HTMLElement>(".thread-message-bubble")?.style.getPropertyValue("--msg-body-font")).toContain("--font-reading");
    expect(updateMessageFlags).not.toHaveBeenCalled();
  });

  it("shows copied recipients when expanded", async () => {
    render(<ThreadMessageBubble message={message} defaultExpanded />);

    await waitFor(() => expect(document.body.textContent).toContain("Rendered"));
    expect(document.body.textContent).toContain("Cc: cc@example.com");
  });

  it("uses relaxed privacy mode by default when rendering expanded thread messages", async () => {
    localStorage.removeItem("gomail-privacy-mode");

    render(<ThreadMessageBubble message={message} defaultExpanded />);

    await waitFor(() => {
      expect(getRenderedHtml).toHaveBeenCalledWith("message-1", "LoadOnce");
    });
  });

  it("marks only an expanded unread message and fetches full content before replying", async () => {
    vi.mocked(getMessage).mockResolvedValue({ ...message, body_text: "full quote" });
    render(<ThreadMessageBubble message={{ ...message, is_read: false, body_text: "" }} defaultExpanded />);
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith("message-1", true, undefined));
    fireEvent.click(screen.getByRole("button", { name: "messageActions.reply" }));
    await waitFor(() => expect(mocks.openCompose).toHaveBeenCalledWith("reply", expect.objectContaining({ body_text: "full quote" })));
    expect(client.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["threadMessages"] });
  });

  it("shows rendering failures and ignores responses after message changes", async () => {
    let finish!: (value: never) => void;
    vi.mocked(getRenderedHtml).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const view = render(<ThreadMessageBubble message={message} defaultExpanded />);
    vi.mocked(getRenderedHtml).mockRejectedValueOnce(new Error("failed"));
    view.rerender(<ThreadMessageBubble message={{ ...message, id: "other" }} defaultExpanded />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    await act(async () => finish({ html: "STALE", trackers_blocked: [], images_blocked: 0 } as never));
    expect(document.body.textContent).not.toContain("STALE");
  });

  it("re-renders on trust revocation and discards the old response", async () => {
    let old!: (value: never) => void;
    vi.mocked(getRenderedHtml).mockImplementationOnce(() => new Promise((resolve) => { old = resolve; }))
      .mockResolvedValueOnce({ html: "NEW STRICT", trackers_blocked: [], images_blocked: 0 });
    render(<ThreadMessageBubble message={message} defaultExpanded />);
    act(() => mocks.changed?.({ payload: { accountId: "account-1", email: "sender@example.com" } }));
    await screen.findByText("NEW STRICT");
    await act(async () => old({ html: "OLD TRUST", trackers_blocked: [], images_blocked: 0 } as never));
    expect(document.body.textContent).not.toContain("OLD TRUST");
  });

  it("offers contact actions for expanded message participants", async () => {
    render(<ThreadMessageBubble message={message} defaultExpanded />);

    await waitFor(() => expect(document.body.textContent).toContain("Rendered"));
    expect(document.querySelectorAll("[data-testid='contact-address-action']")).toHaveLength(3);
  });

  it("does not undo an explicit mark-unread while the bubble remains open", async () => {
    const view = render(<ThreadMessageBubble message={message} defaultExpanded />);
    await screen.findByText("<p>Rendered</p>");
    vi.mocked(getMessage).mockResolvedValue({ ...message, is_read: false });
    view.rerender(<ThreadMessageBubble message={{ ...message, is_read: false }} defaultExpanded />);
    await act(async () => {});
    expect(updateMessageFlags).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { expanded: true }));
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(message.id, true, undefined));
  });
});
