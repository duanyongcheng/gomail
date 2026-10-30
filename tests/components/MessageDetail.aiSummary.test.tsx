import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MessageDetail from "../../src/components/MessageDetail";
import { aiSummarizeMessage } from "@/lib/api";

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

vi.mock("../../src/lib/api", () => ({
  trustSender: vi.fn(),
  aiSummarizeMessage: vi.fn(),
}));

vi.mock("../../src/hooks/useMessageLoader", () => ({
  useMessageLoader: () => ({
    message: {
      id: "message-1",
      account_id: "account-1",
      remote_id: "remote-1",
      message_id_header: null,
      in_reply_to: null,
      references_header: null,
      thread_id: null,
      subject: "Quarterly numbers",
      snippet: "Numbers attached",
      from_address: "sender@example.com",
      from_name: "Sender",
      to_list: [],
      cc_list: [],
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
      body_text: "The numbers are in the attachment.",
      body_html_raw: "",
    },
    setMessage: vi.fn(),
    rendered: { html: "<p>Body</p>", images_blocked: 0, trackers_blocked: [] },
    loading: false,
    error: null,
  }),
}));

vi.mock("../../src/hooks/queries", () => ({
  useAccountsQuery: () => ({ data: [] }),
}));

vi.mock("../../src/hooks/useBilingualTranslation", () => ({
  useBilingualTranslation: () => ({
    bilingualMode: false,
    bilingualResult: null,
    bilingualLoading: false,
    bilingualError: null,
    bilingualWarning: null,
    handleBilingualToggle: vi.fn(),
    resetBilingual: vi.fn(),
  }),
}));

vi.mock("../../src/components/MessageActionToolbar", () => ({
  default: () => <div>message actions</div>,
}));

vi.mock("../../src/components/PrivacyBanner", () => ({
  default: () => <div>privacy banner</div>,
}));

vi.mock("../../src/components/ShadowDomEmail", () => ({
  ShadowDomEmail: ({ html }: { html: string }) => <div>{html}</div>,
}));

vi.mock("../../src/features/translate/TranslatePopover", () => ({
  default: () => <div>translate popover</div>,
}));

vi.mock("../../src/components/ContactAddressAction", () => ({
  default: ({ address }: { address: string }) => <span>{address}</span>,
}));

describe("MessageDetail AI summary entry", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("keeps the summary closed until the user asks for it", () => {
    render(<MessageDetail messageId="message-1" onBack={vi.fn()} />);

    expect(screen.queryByText("ai.summaryTitle")).toBeNull();
    expect(screen.getByRole("button", { name: "ai.summarize" })).toBeTruthy();
  });

  it("disables the summary button rather than opening a nonworking card", () => {
    render(<MessageDetail messageId="message-1" onBack={vi.fn()} />);
    const button = screen.getByRole("button", { name: "ai.summarize" });

    fireEvent.click(button);
    expect(screen.queryByText("ai.summaryTitle")).toBeNull();
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(button.getAttribute("title")).toBeTruthy();

    fireEvent.click(button);
    expect(screen.queryByText("ai.summaryTitle")).toBeNull();
    expect(button.getAttribute("aria-pressed")).toBe("false");
  });

  it("guards direct legacy keyboard events for AI and translation", () => {
    render(<MessageDetail messageId="message-1" onBack={vi.fn()} />);

    act(() => {
      document.dispatchEvent(new CustomEvent("pebble:ai-summarize"));
      document.dispatchEvent(new CustomEvent("pebble:toggle-bilingual"));
      document.dispatchEvent(new CustomEvent("pebble:translate-selection"));
    });

    expect(screen.queryByText("ai.summaryTitle")).toBeNull();
    expect(screen.queryByText("translate popover")).toBeNull();
    expect(aiSummarizeMessage).not.toHaveBeenCalled();
    expect((screen.getByRole("button", { name: "Toggle bilingual view" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Snooze message" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("never renders the summary inside the message body", () => {
    render(<MessageDetail messageId="message-1" onBack={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "ai.summarize" }));

    const body = screen.getByRole("region", { name: "Message body" });
    expect(body.textContent).not.toContain("ai.summaryTitle");
  });
});
