import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AccountSetup from "@/components/AccountSetup";
import { AccountService } from "@/mygo";
import { accountsQueryKey } from "@/hooks/queries";
import { useUIStore } from "@/stores/ui.store";

// Keep api.ts AND platform/commands.ts real: a fake invoke used to hide missing support.
vi.mock("@/mygo", () => ({ AccountService: { status: vi.fn(), login: vi.fn() }, SyncService: { start: vi.fn() } }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
import { SyncService } from "@/mygo";

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const onClose = vi.fn();
  render(<QueryClientProvider client={client}><AccountSetup onClose={onClose} /></QueryClientProvider>);
  return { invalidate, onClose };
}

beforeEach(() => {
  vi.clearAllMocks();
  useUIStore.setState({ realtimeMode: "realtime" });
  vi.mocked(AccountService.status).mockResolvedValue({ configured: true, message: "Ready" });
  vi.mocked(AccountService.login).mockResolvedValue({ id: "account-1", email: "user@example.com", display_name: "User", provider: "gmail", created_at: 1, updated_at: 1 });
  vi.mocked(SyncService.start).mockResolvedValue({ value: "started" });
});

describe("Gmail account setup regression", () => {
  it("waits for configuration then signs in through the real adapter and refreshes folders", async () => {
    const { invalidate, onClose } = setup();
    const button = screen.getByRole("button", { name: "Sign in with Google" });
    expect(button).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Account label"), { target: { value: "Work" } });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(AccountService.login).toHaveBeenCalledExactlyOnceWith({ provider: "gmail", email: "", displayName: "", proxyHost: undefined, proxyPort: undefined, accountLabel: "Work" });
    expect(SyncService.start).toHaveBeenCalledOnce();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accountsQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["folders", "account-1"] });
  });

  it("keeps Gmail selected and protocol passwords unmounted until explicitly selected", async () => {
    setup();
    await waitFor(() => expect(screen.getByRole("button", { name: "Sign in with Google" })).toBeEnabled());
    expect(screen.queryByRole("button", { name: "Sign in with Microsoft" })).toBeNull();
    expect(screen.queryByLabelText(/Incoming protocol|Password|SOCKS5 Proxy|SMTP|IMAP/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Account & Sync" })).toBeNull();
    expect(screen.getByRole("option", { name: "IMAP / SMTP" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Microsoft" })).toBeInTheDocument();
    expect(AccountService.login).not.toHaveBeenCalled();
  });

  it("fails closed on status failure and supports retry", async () => {
    vi.mocked(AccountService.status).mockRejectedValueOnce(new Error("offline"));
    setup();
    await screen.findByRole("alert");
    const button = screen.getByRole("button", { name: "Sign in with Google" });
    fireEvent.click(button);
    expect(AccountService.login).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(button).toBeEnabled());
  });

  it("keeps unconfigured Google disabled and preserves the dialog on backdrop clicks", async () => {
    vi.mocked(AccountService.status).mockResolvedValue({ configured: false, message: "Not configured" });
    const { onClose } = setup();
    await screen.findByText("Configure Google OAuth before reconnecting.");
    expect(screen.getByRole("button", { name: "Sign in with Google" })).toBeDisabled();
    fireEvent.click(screen.getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("blocks repeated sign-ins and keeps a failed attempt visible", async () => {
    let reject!: (reason: Error) => void;
    vi.mocked(AccountService.login).mockImplementationOnce(() => new Promise((_resolve, rejectRequest) => { reject = rejectRequest; }));
    const { onClose } = setup();
    const button = screen.getByRole("button", { name: "Sign in with Google" });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    fireEvent.click(button);
    expect(AccountService.login).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
    await act(async () => reject(new Error("Sign-in cancelled")));
    expect(screen.getByRole("alert")).toHaveTextContent("Sign-in cancelled");
    expect(onClose).not.toHaveBeenCalled();
  });
});
