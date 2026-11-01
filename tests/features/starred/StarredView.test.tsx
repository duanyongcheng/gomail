import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MessageSummary } from "../../../src/lib/api";

const fetchNextPage = vi.fn();
const setSelectedMessage = vi.fn((messageId: string | null) => {
  mockMailState.selectedMessageId = messageId;
});
const useStarredMessagesQuery = vi.fn();
const listStarredMessages = vi.fn();
vi.mock("@tanstack/react-query", async (original) => ({ ...await original<object>(), useIsMutating: () => 0 }));

const mockMailState = {
  activeAccountId: "account-1",
  selectedMessageId: null as string | null,
  setSelectedMessage,
};

vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 76,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: `row-${index}`,
        start: index * 76,
      })),
    measureElement: vi.fn(),
    measure: vi.fn(),
  }),
}));

vi.mock("../../../src/hooks/queries/useStarredMessagesQuery", () => ({
  useStarredMessagesQuery: (...args: unknown[]) => useStarredMessagesQuery(...args),
}));

vi.mock("../../../src/lib/api", () => ({
  listStarredMessages: (...args: unknown[]) => listStarredMessages(...args),
}));

vi.mock("../../../src/stores/mail.store", () => ({
  useMailStore: (selector: (state: typeof mockMailState) => unknown) => selector(mockMailState),
}));

vi.mock("../../../src/components/MessageItem", () => ({
  default: ({
    message,
    onClick,
  }: {
    message: MessageSummary;
    onClick: () => void;
  }) => {
    return (
      <div data-testid={`message-${message.id}`}>
        <button onClick={onClick}>{message.subject}</button>
      </div>
    );
  },
}));

vi.mock("../../../src/components/MessageDetail", () => ({
  default: ({ messageId }: { messageId: string }) => <div data-testid="message-detail">{messageId}</div>,
}));

import StarredView from "../../../src/features/starred/StarredView";

function makeMessage(id: string): MessageSummary {
  return {
    id,
    account_id: "account-1",
    remote_id: `remote-${id}`,
    thread_id: `thread-${id}`,
    subject: `Subject ${id}`,
    snippet: `Snippet ${id}`,
    from_address: "sender@example.com",
    from_name: "Sender",
    to_list: [],
    cc_list: [],
    bcc_list: [],
    has_attachments: false,
    is_read: true,
    is_starred: true,
    is_draft: false,
    date: 1_700_000_000,
    remote_version: null,
    is_deleted: false,
    deleted_at: null,
    created_at: 1_700_000_000,
    updated_at: 1_700_000_000,
  };
}

describe("StarredView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockMailState.activeAccountId = "account-1";
    mockMailState.selectedMessageId = null;
    mockMailState.setSelectedMessage = setSelectedMessage;
  });

  it("shows Load More for a full first page and fetches the next page when clicked", () => {
    const messages = Array.from({ length: 50 }, (_, index) => makeMessage(`m-${index + 1}`));
    useStarredMessagesQuery.mockReturnValue({
      data: messages,
      loading: false,
      error: null,
      hasNextPage: true,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    render(<StarredView />);

    expect(useStarredMessagesQuery).toHaveBeenCalledWith("account-1");
    expect(screen.getByRole("button", { name: "Load More" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Load More" }));

    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("filters unstarred rows from query data without closing selected detail prematurely", () => {
    const messages = [makeMessage("m-1")];
    useStarredMessagesQuery.mockReturnValue({
      data: messages,
      loading: false,
      error: null,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    const { rerender } = render(<StarredView />);
    fireEvent.click(screen.getByRole("button", { name: "Subject m-1" }));
    messages[0].is_starred = false;
    useStarredMessagesQuery.mockReturnValue({ data: [...messages], loading: false, error: null, hasNextPage: true, fetchNextPage });
    rerender(<StarredView />);
    expect(screen.queryByTestId("message-m-1")).toBeNull();
    expect(screen.getByTestId("message-detail").textContent).toBe("m-1");
  });

  it("restores a locally removed row when an unstar operation rolls back", () => {
    useStarredMessagesQuery.mockReturnValue({
      data: [makeMessage("m-1")],
      loading: false,
      error: null,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    const { rerender } = render(<StarredView />);
    const data = useStarredMessagesQuery.mock.results.at(-1)!.value;
    useStarredMessagesQuery.mockReturnValue({ ...data, data: [{ ...makeMessage("m-1"), is_starred: false }], hasNextPage: true });
    rerender(<StarredView />);
    expect(screen.queryByTestId("message-m-1")).toBeNull();
    useStarredMessagesQuery.mockReturnValue(data);
    rerender(<StarredView />);
    expect(screen.getByTestId("message-m-1")).toBeTruthy();
  });

  it("does not open a stale globally selected message when entering starred view", () => {
    mockMailState.selectedMessageId = "stale-inbox-message";
    useStarredMessagesQuery.mockReturnValue({
      data: [makeMessage("m-1")],
      loading: false,
      error: null,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    render(<StarredView />);

    expect(screen.queryByTestId("message-detail")).toBeNull();
  });

  it("keeps Load More available when all currently loaded starred messages are unstarred", () => {
    useStarredMessagesQuery.mockReturnValue({
      data: [makeMessage("m-1")],
      loading: false,
      error: null,
      hasNextPage: true,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    const { rerender } = render(<StarredView />);
    const data = useStarredMessagesQuery.mock.results.at(-1)!.value;
    useStarredMessagesQuery.mockReturnValue({ ...data, data: [{ ...makeMessage("m-1"), is_starred: false }] });
    rerender(<StarredView />);
    expect(screen.queryByText("No starred messages")).toBeNull();
    expect(screen.getByRole("button", { name: "Load More" })).toBeTruthy();
  });

  it("resets local selection and removal state when the active account changes", () => {
    useStarredMessagesQuery.mockReturnValue({
      data: [makeMessage("m-1")],
      loading: false,
      error: null,
      hasNextPage: true,
      isFetchingNextPage: false,
      fetchNextPage,
    });
    const { rerender } = render(<StarredView />);
    fireEvent.click(screen.getByRole("button", { name: "Subject m-1" }));
    mockMailState.activeAccountId = "account-2";
    rerender(<StarredView />);

    expect(screen.queryByTestId("message-detail")).toBeNull();
    expect(useStarredMessagesQuery).toHaveBeenLastCalledWith("account-2");
  });

  it("keeps loaded messages visible when fetching another starred page fails", () => {
    useStarredMessagesQuery.mockReturnValue({
      data: [makeMessage("m-1")],
      loading: false,
      error: new Error("next page failed"),
      hasNextPage: true,
      isFetchingNextPage: false,
      fetchNextPage,
    });

    render(<StarredView />);

    expect(screen.getByTestId("message-m-1")).toBeTruthy();
    expect(screen.queryByText("Failed to load starred messages")).toBeNull();
  });
});
