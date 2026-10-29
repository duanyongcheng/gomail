import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Account } from "@/lib/api";

const mocks = vi.hoisted(() => ({
  accounts: [] as Account[], activeAccountId: null as string | null,
  list: vi.fn(), remove: vi.fn(), trust: vi.fn(), confirm: vi.fn(),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback?: string) => fallback ?? _key }) }));
vi.mock("@/hooks/queries", () => ({ useAccountsQuery: () => ({ data: mocks.accounts }) }));
vi.mock("@/stores/mail.store", () => ({ useMailStore: (selector: (state: { activeAccountId: string | null }) => unknown) => selector({ activeAccountId: mocks.activeAccountId }) }));
vi.mock("@/stores/confirm.store", () => ({ useConfirmStore: { getState: () => ({ confirm: mocks.confirm }) } }));
vi.mock("@/lib/api", () => ({ listTrustedSenders: mocks.list, removeTrustedSender: mocks.remove, trustSender: mocks.trust }));
vi.mock("@tauri-apps/api/event", () => ({ listen: () => Promise.resolve(() => {}) }));
import PrivacyTab from "@/features/settings/PrivacyTab";
import { PRIVACY_SETTINGS_CHANGED } from "@/lib/privacyEvents";

beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear();
  mocks.accounts = ["a", "b"].map((id) => ({ id, email: `${id}@example.invalid`, display_name: id, provider: "gmail", created_at: 1, updated_at: 1 }));
  mocks.activeAccountId = null;
  mocks.list.mockResolvedValue([]); mocks.remove.mockResolvedValue(undefined); mocks.trust.mockResolvedValue(undefined); mocks.confirm.mockResolvedValue(true);
});
afterEach(cleanup);

describe("PrivacyTab", () => {
  it("defaults relaxed and broadcasts changes in the same window", async () => {
    const changed = vi.fn(); window.addEventListener(PRIVACY_SETTINGS_CHANGED, changed);
    render(<PrivacyTab />);
    expect(screen.getByText("Load external images by default. Trackers are still blocked.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Strict", exact: true }));
    expect(changed).toHaveBeenCalledOnce(); window.removeEventListener(PRIVACY_SETTINGS_CHANGED, changed);
    await screen.findByText(/No trusted senders yet/);
  });
  it("allows account selection from all-accounts mode and clears old data", async () => {
    mocks.list.mockImplementation(async (account: string) => account === "a" ? [{ account_id: "a", email: "trusted@example.invalid", trust_type: "all", created_at: 1 }] : []);
    render(<PrivacyTab />);
    await screen.findByText("trusted@example.invalid");
    fireEvent.change(screen.getByRole("combobox", { name: "Trust account" }), { target: { value: "b" } });
    expect(screen.queryByText("trusted@example.invalid")).toBeNull();
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith("b"));
  });
  it("adds images-only trust and confirms full trust", async () => {
    render(<PrivacyTab />); await screen.findByText(/No trusted senders yet/);
    fireEvent.change(screen.getByRole("textbox", { name: "Sender email address" }), { target: { value: "sender@example.invalid" } });
    fireEvent.click(screen.getByRole("button", { name: "Save trust" }));
    await waitFor(() => expect(mocks.trust).toHaveBeenCalledWith("a", "sender@example.invalid", "images"));
    await waitFor(() => expect((screen.getByRole("textbox", { name: "Sender email address" }) as HTMLInputElement).disabled).toBe(false));
    fireEvent.change(screen.getByRole("textbox", { name: "Sender email address" }), { target: { value: "all@example.invalid" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Trust level" }), { target: { value: "all" } });
    mocks.confirm.mockResolvedValueOnce(false);
    fireEvent.click(screen.getByRole("button", { name: "Save trust" }));
    await waitFor(() => expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("IP address") })));
    expect(mocks.trust).toHaveBeenCalledTimes(1);
  });
  it("shows loading/read failure/retry and retains a failed removal", async () => {
    mocks.list.mockRejectedValueOnce(new Error("disk"));
    render(<PrivacyTab />);
    expect(screen.getByRole("status")).toBeTruthy();
    expect(await screen.findByRole("alert")).toBeTruthy();
    mocks.list.mockResolvedValue([{ account_id: "a", email: "sender@example.invalid", trust_type: "images", created_at: 1 }]);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove trust: sender@example.invalid" }));
    await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith("a", "sender@example.invalid"));
    await waitFor(() => expect((screen.getByRole("button", { name: "Remove trust: sender@example.invalid" }) as HTMLButtonElement).disabled).toBe(false));
    mocks.remove.mockRejectedValueOnce(new Error("fail"));
    fireEvent.click(screen.getByRole("button", { name: "Remove trust: sender@example.invalid" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Failed to remove");
    expect(screen.getByText("sender@example.invalid")).toBeTruthy();
  });
  it("ignores stale list responses after account changes", async () => {
    let complete!: (rows: unknown[]) => void;
    mocks.list.mockImplementationOnce(() => new Promise((resolve) => { complete = resolve; }));
    render(<PrivacyTab />);
    fireEvent.change(screen.getByRole("combobox", { name: "Trust account" }), { target: { value: "b" } });
    await screen.findByText(/No trusted senders yet/);
    await act(async () => complete([{ account_id: "a", email: "stale@example.invalid", trust_type: "all", created_at: 1 }]));
    expect(screen.queryByText("stale@example.invalid")).toBeNull();
  });
});
