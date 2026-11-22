import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import GoogleOAuthConfig from "@/features/settings/GoogleOAuthConfig";
import AccountSetup from "@/components/AccountSetup";
import { AccountService } from "@/mygo";

vi.mock("@/mygo", () => ({
  AccountService: {
    status: vi.fn(),
    login: vi.fn(),
    googleOAuthConfig: vi.fn(),
    saveGoogleOAuthConfig: vi.fn(),
    clearGoogleOAuthConfig: vi.fn(),
  },
  SyncService: { start: vi.fn() },
}));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (_key: string, fallback?: string) => fallback ?? _key }),
}));

let client: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => {
  cleanup();
  client.clear();
});

describe("GoogleOAuthConfig", () => {
  it("loads config lazily on expand, saves credentials, preserves secret when left blank, and clears override", async () => {
    vi.mocked(AccountService.googleOAuthConfig).mockResolvedValueOnce({
      configured: false,
      source: "none",
      clientId: "",
      hasSecret: false,
      hasAppConfig: false,
      appClientId: "",
      appHasSecret: false,
      keyringError: false,
    });
    vi.mocked(AccountService.saveGoogleOAuthConfig).mockResolvedValueOnce({
      configured: true,
      source: "app",
      clientId: "desktop-1.apps.googleusercontent.com",
      hasSecret: true,
      hasAppConfig: true,
      appClientId: "desktop-1.apps.googleusercontent.com",
      appHasSecret: true,
      keyringError: false,
    });
    vi.mocked(AccountService.saveGoogleOAuthConfig).mockResolvedValueOnce({
      configured: true,
      source: "app",
      clientId: "desktop-2.apps.googleusercontent.com",
      hasSecret: true,
      hasAppConfig: true,
      appClientId: "desktop-2.apps.googleusercontent.com",
      appHasSecret: true,
      keyringError: false,
    });
    vi.mocked(AccountService.saveGoogleOAuthConfig).mockResolvedValueOnce({
      configured: true,
      source: "app",
      clientId: "desktop-2.apps.googleusercontent.com",
      hasSecret: false,
      hasAppConfig: true,
      appClientId: "desktop-2.apps.googleusercontent.com",
      appHasSecret: false,
      keyringError: false,
    });
    vi.mocked(AccountService.clearGoogleOAuthConfig).mockResolvedValueOnce({
      configured: true,
      source: "env",
      clientId: "env-client",
      hasSecret: true,
      hasAppConfig: false,
      appClientId: "",
      appHasSecret: false,
      keyringError: false,
    });

    const invalidate = vi.spyOn(client, "invalidateQueries");
    render(
      <QueryClientProvider client={client}>
        <GoogleOAuthConfig />
      </QueryClientProvider>,
    );

    expect(AccountService.googleOAuthConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Configure Google OAuth" }));

    await screen.findByText("Active source: Not configured");
    expect(AccountService.googleOAuthConfig).toHaveBeenCalledOnce();

    // Save initial Client ID and Client Secret.
    fireEvent.change(screen.getByLabelText("Google Client ID"), {
      target: { value: "  desktop-1.apps.googleusercontent.com  " },
    });
    fireEvent.change(screen.getByLabelText("Google Client Secret (optional)"), {
      target: { value: "  my-secret  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Google OAuth" }));

    await screen.findByText("Google OAuth configuration saved and applied.");
    expect(AccountService.saveGoogleOAuthConfig).toHaveBeenNthCalledWith(1, {
      clientId: "desktop-1.apps.googleusercontent.com",
      clientSecret: "my-secret",
      keepExistingSecret: false,
    });
    expect(screen.getByLabelText("Google Client Secret (optional)")).toHaveValue("");
    expect(screen.getByText("Active source: In-app configuration")).toBeInTheDocument();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["account-connections"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["accounts"] });

    // Edit Client ID while leaving secret blank -> keepExistingSecret: true.
    fireEvent.change(screen.getByLabelText("Google Client ID"), {
      target: { value: "desktop-2.apps.googleusercontent.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Google OAuth" }));
    await waitFor(() => expect(AccountService.saveGoogleOAuthConfig).toHaveBeenCalledTimes(2));
    expect(AccountService.saveGoogleOAuthConfig).toHaveBeenNthCalledWith(2, {
      clientId: "desktop-2.apps.googleusercontent.com",
      clientSecret: "",
      keepExistingSecret: true,
    });

    // Explicitly clear saved secret via checkbox -> keepExistingSecret: false.
    fireEvent.click(screen.getByLabelText("Clear saved secret"));
    expect(screen.getByLabelText("Google Client Secret (optional)")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Save Google OAuth" }));
    await waitFor(() => expect(AccountService.saveGoogleOAuthConfig).toHaveBeenCalledTimes(3));
    expect(AccountService.saveGoogleOAuthConfig).toHaveBeenNthCalledWith(3, {
      clientId: "desktop-2.apps.googleusercontent.com",
      clientSecret: "",
      keepExistingSecret: false,
    });

    // Clear in-app configuration -> falls back to env source.
    fireEvent.click(screen.getByRole("button", { name: "Clear in-app configuration" }));
    await screen.findByText("In-app Google OAuth configuration cleared.");
    expect(AccountService.clearGoogleOAuthConfig).toHaveBeenCalledWith({});
    expect(screen.getByText("Active source: Environment variable")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear in-app configuration" })).toBeNull();
  });

  it("shows keyring lock alert and supports retry on load failure", async () => {
    vi.mocked(AccountService.googleOAuthConfig)
      .mockRejectedValueOnce(new Error("db error"))
      .mockResolvedValueOnce({
        configured: false,
        source: "none",
        clientId: "",
        hasSecret: false,
        hasAppConfig: true,
        appClientId: "",
        appHasSecret: false,
        keyringError: true,
      });

    render(
      <QueryClientProvider client={client}>
        <GoogleOAuthConfig defaultOpen />
      </QueryClientProvider>,
    );

    await screen.findByText("Could not load Google OAuth configuration.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText(/profile keyring is unavailable/i);
    expect(screen.getByRole("button", { name: "Clear in-app configuration" })).toBeInTheDocument();
  });

  it("unlocks Google sign-in immediately inside AccountSetup after saving", async () => {
    vi.mocked(AccountService.status).mockResolvedValue({ configured: false, message: "Not configured" });
    vi.mocked(AccountService.googleOAuthConfig).mockResolvedValue({
      configured: false,
      source: "none",
      clientId: "",
      hasSecret: false,
      hasAppConfig: false,
      appClientId: "",
      appHasSecret: false,
      keyringError: false,
    });
    vi.mocked(AccountService.saveGoogleOAuthConfig).mockResolvedValue({
      configured: true,
      source: "app",
      clientId: "live-client.apps.googleusercontent.com",
      hasSecret: true,
      hasAppConfig: true,
      appClientId: "live-client.apps.googleusercontent.com",
      appHasSecret: true,
      keyringError: false,
    });

    render(
      <QueryClientProvider client={client}>
        <AccountSetup onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    await screen.findByText("Configure Google OAuth before reconnecting.");
    const signInButton = screen.getByRole("button", { name: "Sign in with Google" });
    expect(signInButton).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Configure Google OAuth" }));
    await screen.findByLabelText("Google Client ID");
    fireEvent.change(screen.getByLabelText("Google Client ID"), {
      target: { value: "live-client.apps.googleusercontent.com" },
    });
    fireEvent.change(screen.getByLabelText("Google Client Secret (optional)"), {
      target: { value: "live-secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Google OAuth" }));

    await waitFor(() => expect(signInButton).toBeEnabled());
    expect(screen.queryByText("Configure Google OAuth before reconnecting.")).toBeNull();
  });
});
