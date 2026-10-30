import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { executeMessageFlags } from "@/hooks/mutations/messageFlagsMutation";
import { useMessageLoader } from "@/hooks/useMessageLoader";
import { getMessage, getRenderedHtml, updateMessageFlags } from "@/lib/api";
import type { Message } from "@/lib/api";
import "@/lib/i18n";

vi.mock("@/lib/api", () => ({ getMessage: vi.fn(), getRenderedHtml: vi.fn(), updateMessageFlags: vi.fn() }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn(async () => () => {}) }));
const base = { id: "offline:one", account_id: "offline", is_read: true, is_starred: false, from_address: "sender@example.invalid", updated_at: 1, capabilities: { modify_flags: true } } as Message;
const t = (_key: string, fallback: string) => fallback;
let client: QueryClient;
afterEach(async () => {
  onlineManager.setOnline(true);
  await client?.resumePausedMutations();
  cleanup();
  client?.clear();
  vi.resetAllMocks();
});

it("executes local flag writes while offline rather than pausing until connectivity returns", async () => {
  client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  client.setQueryData(["message", base.id], base);
  vi.mocked(updateMessageFlags).mockResolvedValue();
  onlineManager.setOnline(false);
  const done = executeMessageFlags(client, { messageId: base.id, toggleStarred: true }, t);
  await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, undefined, true));
  await done;
  expect(client.isMutating()).toBe(0);
});

it("reopening cached read mail still fetches fresh local unread metadata while offline", async () => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  client.setQueryData(["message", base.id], base);
  let persisted = { ...base, is_read: false };
  vi.mocked(getMessage).mockImplementation(async () => ({ ...persisted }));
  vi.mocked(getRenderedHtml).mockResolvedValue({ html: "<p>Body</p>", trackers_blocked: [], images_blocked: 0 });
  vi.mocked(updateMessageFlags).mockImplementation(async (_id, read) => { persisted = { ...persisted, is_read: read! }; });
  onlineManager.setOnline(false);
  const { result } = renderHook(() => useMessageLoader(base.id, "Strict"), {
    wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
  });
  await waitFor(() => expect(updateMessageFlags).toHaveBeenCalledWith(base.id, true, undefined));
  expect(result.current.message?.is_read).toBe(true);
  expect(persisted.is_read).toBe(true);
});
