import { fireEvent, render, screen } from "@testing-library/react";
import { emptyTrash } from "@/lib/api";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Account, Folder } from "../../../src/lib/api";

const mocks = vi.hoisted(() => ({
  mailState: {
    activeAccountId: null as string | null,
    activeFolderId: "all:inbox" as string | null,
    selectedMessageId: null as string | null,
    selectedThreadId: null as string | null,
    threadView: false,
    setSelectedMessage: vi.fn(),
    setSelectedThreadId: vi.fn(),
    toggleThreadView: vi.fn(),
  },
  accounts: [
    { id: "account-1", email: "one@example.com" },
    { id: "account-2", email: "two@example.com" },
  ] as Account[],
  folders: [
    { id: "a1-inbox", account_id: "account-1", role: "inbox", sort_order: 0 },
    { id: "a2-inbox", account_id: "account-2", role: "inbox", sort_order: 0 },
  ] as Folder[],
  useFoldersForAccountsQuery: vi.fn(),
  useMessagesQuery: vi.fn(),
  useThreadsQuery: vi.fn(),
  openCompose: vi.fn(),
}));

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
  }),
}));

vi.mock("../../../src/stores/mail.store", () => ({
  useMailStore: (selector: (state: typeof mocks.mailState) => unknown) => selector(mocks.mailState),
}));
vi.mock("../../../src/stores/compose.store", () => ({
  useComposeStore: { getState: () => ({ openCompose: mocks.openCompose }) },
}));

vi.mock("../../../src/stores/ui.store", () => ({
  useUIStore: (selector: (state: { setActiveView: () => void }) => unknown) =>
    selector({ setActiveView: vi.fn() }),
}));

vi.mock("../../../src/stores/toast.store", () => ({
  useToastStore: (selector: (state: { addToast: () => void }) => unknown) =>
    selector({ addToast: vi.fn() }),
}));

vi.mock("../../../src/hooks/queries", () => ({
  useAccountsQuery: () => ({ data: mocks.accounts }),
  useFoldersQuery: () => ({
    data: mocks.folders.slice(0, 1),
    isFetched: true,
  }),
  useFoldersForAccountsQuery: mocks.useFoldersForAccountsQuery,
  useMessagesQuery: mocks.useMessagesQuery,
  useThreadsQuery: mocks.useThreadsQuery,
  patchMessagesCache: vi.fn(),
}));

vi.mock("../../../src/components/SearchBar", () => ({
  default: () => <div>Search bar</div>,
}));

vi.mock("../../../src/components/MessageList", () => ({
  default: () => <div>Message list</div>,
}));

vi.mock("../../../src/components/MessageDetail", () => ({
  default: () => <div>Message detail</div>,
}));

vi.mock("../../../src/features/inbox/ThreadView", () => ({
  default: () => <div>Thread detail</div>,
}));

vi.mock("../../../src/components/ConfirmDialog", () => ({
  default: () => <div>Confirm dialog</div>,
}));

vi.mock("../../../src/lib/api", () => ({
  emptyTrash: vi.fn(),
  triggerSync: vi.fn(),
}));

import InboxView from "../../../src/features/inbox/InboxView";

describe("InboxView all accounts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.accounts = [
      { id: "account-1", email: "one@example.com", provider: "gmail" },
      { id: "account-2", email: "two@example.com", provider: "gmail" },
    ] as Account[];
    mocks.mailState.activeAccountId = null;
    mocks.mailState.activeFolderId = "all:inbox";
    mocks.mailState.selectedMessageId = null;
    mocks.mailState.selectedThreadId = null;
    mocks.mailState.threadView = false;
    mocks.useFoldersForAccountsQuery.mockReturnValue({
      data: mocks.folders,
      isFetched: true,
    });
    mocks.useMessagesQuery.mockReturnValue({
      data: [],
      isLoading: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage: vi.fn(),
    });
    mocks.useThreadsQuery.mockReturnValue({
      data: [],
      isLoading: false,
    });
  });

  it("blocks empty-trash across all accounts despite an implemented command", () => {
    mocks.mailState.activeFolderId = "all:trash";
    mocks.useFoldersForAccountsQuery.mockReturnValue({ data: mocks.folders.map((folder) => ({ ...folder, role: "trash" })), isFetched: true });
    mocks.useMessagesQuery.mockReturnValue({ data: [{ id: "synthetic" }], isLoading: false });
    render(<InboxView />);
    const button = screen.getByRole("button", { name: "Empty Trash" }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.title).toBeTruthy();
    fireEvent.click(button);
    expect(emptyTrash).not.toHaveBeenCalled();
    expect(screen.queryByText("Confirm dialog")).toBeNull();
  });

  it("queries all matching inbox folders when all accounts are selected", () => {
    render(<InboxView />);

    expect(mocks.useFoldersForAccountsQuery).toHaveBeenCalledWith(["account-1", "account-2"]);
    expect(mocks.useMessagesQuery).toHaveBeenCalledWith("a1-inbox", ["a1-inbox", "a2-inbox"]);
  });
  it("keeps search inside the list panel and an honest empty reader beside it", () => {
    const { container } = render(<InboxView />);
    expect(screen.getByText("Search bar").closest(".mail-list-panel")).toBeTruthy();
    expect(container.querySelector(".mail-list-title")?.textContent).toBe("sidebar.inbox");
    expect(container.querySelector(".mail-reader-panel--empty")).toBeTruthy();
    expect(screen.getByText("Your next good read")).toBeTruthy();
    expect(container.querySelector(".mail-workspace--detail-open")).toBeNull();
  });
  it("marks an open detail for the narrow-screen list-to-reader transition", () => {
    mocks.mailState.selectedMessageId = "selected";
    const { container } = render(<InboxView />);
    expect(container.querySelector(".mail-workspace--detail-open")).toBeTruthy();
    expect(container.querySelector(".mail-reader-panel--empty")).toBeNull();
    expect(screen.getByText("Message detail")).toBeTruthy();
  });
  it("opens the explicitly selected Gmail draft manager without guessing a remote draft ID", () => {
    mocks.mailState.activeAccountId = "account-1";
    mocks.mailState.activeFolderId = "a1-drafts";
    mocks.useFoldersForAccountsQuery.mockReturnValue({
      data: [{ id: "a1-drafts", account_id: "account-1", role: "drafts", sort_order: 0 }],
      isFetched: true,
    });
    render(<InboxView />);
    fireEvent.click(screen.getByRole("button", { name: "Manage Gmail drafts" }));
    expect(mocks.openCompose).toHaveBeenCalledWith("new", null, { gmailDraftsAccountId: "account-1" });
  });
  it("keeps the account choice explicit in a combined drafts folder", () => {
    mocks.mailState.activeFolderId = "all:drafts";
    mocks.useFoldersForAccountsQuery.mockReturnValue({
      data: mocks.folders.map((folder) => ({ ...folder, role: "drafts" })),
      isFetched: true,
    });
    render(<InboxView />);
    fireEvent.click(screen.getByRole("button", { name: "Manage Gmail drafts: two@example.com" }));
    expect(mocks.openCompose).toHaveBeenCalledWith("new", null, { gmailDraftsAccountId: "account-2" });
  });
  it("does not offer Gmail draft actions for a non-Gmail account", () => {
    mocks.accounts = [{ id: "account-1", provider: "imap" }] as Account[];
    mocks.mailState.activeAccountId = "account-1";
    mocks.mailState.activeFolderId = "a1-drafts";
    mocks.useFoldersForAccountsQuery.mockReturnValue({
      data: [{ id: "a1-drafts", account_id: "account-1", role: "drafts", sort_order: 0 }],
      isFetched: true,
    });
    render(<InboxView />);
    expect(screen.queryByRole("button", { name: /Manage Gmail drafts/ })).toBeNull();
  });
});
