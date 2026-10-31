import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Account } from "@/lib/api";
import type { DownloadFailure } from "@/mygo";

const events = vi.hoisted(() => ({ progress: undefined as undefined | ((event: { payload: { status: string } }) => void) }));
vi.mock("@/lib/mailDownloads", () => ({ listDownloadFailures: vi.fn(), retryDownloads: vi.fn() }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn((_name, callback) => { events.progress = callback; return Promise.resolve(vi.fn()); }) }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string, options?: Record<string, unknown>) => (fallback ?? key).replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(options?.[name] ?? "")) }) }));
import { listDownloadFailures, retryDownloads } from "@/lib/mailDownloads";
import MailDownloadFailures from "@/features/settings/MailDownloadFailures";

const accounts: Account[] = [{ id: "a", email: "a@example.invalid", display_name: "A", provider: "gmail", created_at: 1, updated_at: 1, color: null }];
let online: boolean;
function failure(overrides: Partial<DownloadFailure> = {}): DownloadFailure {
  return { account_id: "a", remote_id: "opaque-id", account_email: "a@example.invalid", subject: "", kind: "invalid_content", attempts: 1, next_retry_at: 1700000000, updated_at: 1700000000, ...overrides };
}
function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><MailDownloadFailures accounts={accounts} /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks(); events.progress = undefined;
  online = onlineManager.isOnline(); onlineManager.setOnline(true);
  vi.mocked(listDownloadFailures).mockReset().mockResolvedValue({ items: [], total: 0 });
  vi.mocked(retryDownloads).mockReset().mockResolvedValue(undefined);
});
afterEach(() => { cleanup(); onlineManager.setOnline(online); });

describe("MailDownloadFailures", () => {
  it("shows an empty list and disables retry", async () => {
    renderPanel();
    expect(await screen.findByText("No failed downloads")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Retry failed downloads" }) as HTMLButtonElement).disabled).toBe(true);
  });
  it("loads local failures and runs the explicit retry action while browser connectivity is offline", async () => {
    onlineManager.setOnline(false);
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [failure()], total: 1 });
    renderPanel();
    await screen.findByText("The message could not be decoded.");
    fireEvent.click(screen.getByRole("button", { name: "Retry failed downloads" }));
    await waitFor(() => expect(retryDownloads).toHaveBeenCalledWith(""));
    expect(await screen.findByText("Retry finished.")).toBeTruthy();
    await waitFor(() => expect(listDownloadFailures.mock.calls.length).toBeGreaterThan(1));
    expect(onlineManager.isOnline()).toBe(false);
  });
  it("explains content and size failures without exposing opaque IDs", async () => {
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [failure(), failure({ remote_id: "large", kind: "too_large", subject: "Cached subject" })], total: 2 });
    renderPanel();
    expect(await screen.findByText("The message could not be decoded.")).toBeTruthy();
    expect(screen.getByText("Message exceeds the 50 MiB download limit.")).toBeTruthy();
    expect(screen.getByText("Cached subject")).toBeTruthy();
    expect(screen.queryByText("opaque-id")).toBeNull();
    expect(screen.getByText("2 messages waiting for download")).toBeTruthy();
  });
  it("shows loading and lets the user retry a failed list request", async () => {
    let reject: (error: Error) => void = () => {};
    vi.mocked(listDownloadFailures).mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; }));
    renderPanel();
    expect(screen.getByText("Loading...")).toBeTruthy();
    await act(async () => reject(new Error("local read failure")));
    expect((await screen.findByRole("alert")).textContent).toContain("Could not load failed downloads.");
    fireEvent.click(screen.getByRole("button", { name: "Retry", exact: true }));
    expect(await screen.findByText("No failed downloads")).toBeTruthy();
  });
  it("filters an account, disables repeated retry and refreshes the result", async () => {
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [failure()], total: 1 });
    let finish: () => void = () => {};
    vi.mocked(retryDownloads).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    renderPanel();
    await screen.findByText("The message could not be decoded.");
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "a" } });
    await waitFor(() => expect(listDownloadFailures).toHaveBeenCalledWith("a", 20, 0));
    await waitFor(() => expect((screen.getByRole("button", { name: "Retry failed downloads" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Retry failed downloads" }));
    expect((await screen.findByRole("button", { name: "Retrying…" })) as HTMLButtonElement).toHaveProperty("disabled", true);
    expect(retryDownloads).toHaveBeenCalledWith("a");
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [], total: 0 });
    await act(async () => finish());
    expect(await screen.findByText("No failed downloads")).toBeTruthy();
    expect(screen.getByText("Retry finished.")).toBeTruthy();
  });
  it("keeps failed mail visible when retry fails", async () => {
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [failure()], total: 1 });
    vi.mocked(retryDownloads).mockRejectedValue(new Error("private remote diagnostic"));
    renderPanel(); await screen.findByText("The message could not be decoded.");
    fireEvent.click(screen.getByRole("button", { name: "Retry failed downloads" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Check your connection and account sign-in");
    expect(screen.getByText("1 messages waiting for download")).toBeTruthy();
    expect(screen.queryByText("private remote diagnostic")).toBeNull();
  });
  it("paginates and returns to a valid page after synchronization clears failures", async () => {
    vi.mocked(listDownloadFailures).mockImplementation(async (_account, _limit, offset) => ({ items: [failure({ subject: offset === 0 ? "First page" : "Second page" })], total: 21 }));
    renderPanel(); await screen.findByText("First page");
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("Second page")).toBeTruthy();
    expect(listDownloadFailures).toHaveBeenCalledWith("", 20, 20);
    vi.mocked(listDownloadFailures).mockResolvedValue({ items: [], total: 0 });
    act(() => events.progress?.({ payload: { status: "completed" } }));
    expect(await screen.findByText("No failed downloads")).toBeTruthy();
    await waitFor(() => expect(listDownloadFailures).toHaveBeenLastCalledWith("", 20, 0));
  });
});
