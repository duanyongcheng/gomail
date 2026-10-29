import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Message, RenderedHtml } from "../../src/lib/api";
import { useMessageLoader } from "../../src/hooks/useMessageLoader";
import { getMessageWithHtml, getRenderedHtml } from "../../src/lib/api";

const privacyEvents = vi.hoisted(() => ({ callback: undefined as undefined | ((e: { payload: { accountId: string; email: string } }) => void) }));
vi.mock("@tauri-apps/api/event", () => ({ listen: (_name: string, callback: typeof privacyEvents.callback) => { privacyEvents.callback = callback; return Promise.resolve(() => {}); } }));

vi.mock("../../src/lib/api", () => ({
  getMessageWithHtml: vi.fn(),
  getRenderedHtml: vi.fn(),
}));

const { mutateFlags } = vi.hoisted(() => ({ mutateFlags: vi.fn() }));
vi.mock("../../src/hooks/mutations/useUpdateFlagsMutation", () => ({
  useUpdateFlagsMutation: () => ({ mutate: mutateFlags }),
}));

function makeMessage(overrides: Partial<Message> = {}): Message {
  return {
    id: "message-1",
    account_id: "account-1",
    remote_id: "remote-1",
    message_id_header: null,
    in_reply_to: null,
    references_header: null,
    thread_id: "thread-1",
    subject: "Subject",
    snippet: "Snippet",
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
    body_text: "Body",
    body_html_raw: "<p>Body</p>",
    ...overrides,
  };
}

function makeRendered(html = "<p>Body</p>"): RenderedHtml {
  return {
    html,
    trackers_blocked: [],
    images_blocked: 0,
  };
}

describe("useMessageLoader", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not request a second HTML render after the initial message load", async () => {
    vi.mocked(getMessageWithHtml).mockResolvedValue([makeMessage(), makeRendered()]);

    const { result } = renderHook(() => useMessageLoader("message-1", "Strict"));

    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(getMessageWithHtml).toHaveBeenCalledOnce();
    expect(getMessageWithHtml).toHaveBeenCalledWith("message-1", "Strict");
    expect(getRenderedHtml).not.toHaveBeenCalled();
  });

  it("re-renders HTML without reloading the message when privacy mode changes", async () => {
    vi.mocked(getMessageWithHtml).mockResolvedValue([makeMessage(), makeRendered()]);
    vi.mocked(getRenderedHtml).mockResolvedValue(makeRendered("<p>Strict again</p>"));

    const { rerender } = renderHook(
      ({ privacyMode }) => useMessageLoader("message-1", privacyMode),
      { initialProps: { privacyMode: "Strict" as const } },
    );

    await waitFor(() => expect(getMessageWithHtml).toHaveBeenCalledOnce());

    rerender({ privacyMode: "LoadOnce" as const });

    await waitFor(() => expect(getRenderedHtml).toHaveBeenCalledOnce());
    expect(getRenderedHtml).toHaveBeenCalledWith("message-1", "LoadOnce");
    expect(getMessageWithHtml).toHaveBeenCalledOnce();
  });

  it("rejects a permissive in-flight response after trust revocation", async () => {
    vi.mocked(getMessageWithHtml).mockResolvedValue([makeMessage(), makeRendered("<p>initial</p>")]);
    let oldResponse!: (value: RenderedHtml) => void;
    vi.mocked(getRenderedHtml).mockImplementationOnce(() => new Promise((resolve) => { oldResponse = resolve; }))
      .mockResolvedValueOnce(makeRendered("<p>strict after revoke</p>"));
    const { result } = renderHook(() => useMessageLoader("message-1", "Strict"));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.refreshPrivacy());
    await waitFor(() => expect(getRenderedHtml).toHaveBeenCalledTimes(1));
    act(() => privacyEvents.callback?.({ payload: { accountId: "account-1", email: "sender@example.com" } }));
    await waitFor(() => expect(result.current.rendered?.html).toContain("strict after revoke"));
    await act(async () => oldResponse(makeRendered("<p>STALE PERMISSIVE</p>")));
    expect(result.current.rendered?.html).not.toContain("STALE");
    expect(getMessageWithHtml).toHaveBeenCalledOnce();
  });

  it("ignores trust events for other mailboxes and rejects old initial HTML on mode change", async () => {
    let initial!: (value: [Message, RenderedHtml]) => void;
    vi.mocked(getMessageWithHtml).mockImplementationOnce(() => new Promise((resolve) => { initial = resolve; }));
    vi.mocked(getRenderedHtml).mockResolvedValue(makeRendered("<p>new strict</p>"));
    const { result, rerender } = renderHook(({ mode }) => useMessageLoader("message-1", mode), { initialProps: { mode: "Off" as "Off" | "Strict" } });
    rerender({ mode: "Strict" });
    await act(async () => initial([makeMessage(), makeRendered("<p>OLD OFF</p>")]));
    await waitFor(() => expect(result.current.rendered?.html).toContain("new strict"));
    act(() => privacyEvents.callback?.({ payload: { accountId: "other", email: "sender@example.com" } }));
    expect(getRenderedHtml).toHaveBeenCalledOnce();
  });

  it("cannot restore initial trusted HTML after a trust change before metadata arrives", async () => {
    let initial!: (value: [Message, RenderedHtml]) => void;
    vi.mocked(getMessageWithHtml).mockImplementationOnce(() => new Promise((resolve) => { initial = resolve; }));
    vi.mocked(getRenderedHtml).mockResolvedValue(makeRendered("<p>revoked before load</p>"));
    const { result } = renderHook(() => useMessageLoader("message-1", "Strict"));
    act(() => privacyEvents.callback?.({ payload: { accountId: "account-1", email: "sender@example.com" } }));
    await act(async () => initial([makeMessage(), makeRendered("<p>OLD TRUSTED</p>")]));
    await waitFor(() => expect(result.current.rendered?.html).toContain("revoked before load"));
    expect(result.current.rendered?.html).not.toContain("OLD TRUSTED");
  });

  it("does not automatically mark read when the account capability is unavailable", async () => {
    vi.mocked(getMessageWithHtml).mockResolvedValue([
      makeMessage({ is_read: false, capabilities: { modify_flags: false } }), makeRendered(),
    ]);
    const { result } = renderHook(() => useMessageLoader("message-1", "Strict"));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mutateFlags).not.toHaveBeenCalled();
  });

  it("exposes message load failures instead of collapsing them into a missing message", async () => {
    vi.mocked(getMessageWithHtml).mockRejectedValue(new Error("IPC failed"));

    const { result } = renderHook(() => useMessageLoader("message-1", "Strict"));

    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.message).toBeNull();
    expect(result.current.error).toBe("IPC failed");
  });

  it("exposes privacy re-render failures without clearing the loaded message", async () => {
    vi.mocked(getMessageWithHtml).mockResolvedValue([makeMessage(), makeRendered()]);
    vi.mocked(getRenderedHtml).mockRejectedValue(new Error("render failed"));

    const { result, rerender } = renderHook(
      ({ privacyMode }) => useMessageLoader("message-1", privacyMode),
      { initialProps: { privacyMode: "Strict" as const } },
    );

    await waitFor(() => expect(result.current.message?.id).toBe("message-1"));

    rerender({ privacyMode: "LoadOnce" as const });

    await waitFor(() => expect(result.current.error).toBe("render failed"));
    expect(result.current.message?.id).toBe("message-1");
  });
});
