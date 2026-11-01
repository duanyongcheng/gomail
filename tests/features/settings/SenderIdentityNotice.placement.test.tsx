import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsView from "@/features/settings/SettingsView";
import { accountsQueryKey } from "@/hooks/queries";
import {
  initializeProfileStorageNamespace,
  profileLocalStorage,
  resetProfileStorageNamespaceForTests,
} from "@/lib/profileStorage";
import { useUIStore } from "@/stores/ui.store";

vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));
vi.mock("@/hooks/queries", async (original) => ({
  ...await original<typeof import("@/hooks/queries")>(),
  useAccountUnreadCounts: () => ({}),
}));
vi.mock("@/features/settings/AccountConnections", () => ({ default: () => null }));
vi.mock("@/features/settings/MailDownloadFailures", () => ({ default: () => null }));
vi.mock("@/features/settings/GmailLabels", () => ({ default: () => null }));
vi.mock("@/features/settings/GeneralTab", () => ({ default: () => <div>General settings</div> }));

describe("sender identity reminder placement", () => {
  let client: QueryClient;

  beforeEach(async () => {
    localStorage.clear();
    await initializeProfileStorageNamespace(async () => "sender-notice-placement");
    useUIStore.setState({ settingsTab: "accounts" });
    client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(accountsQueryKey, [{
      id: "gmail",
      email: "sender@example.invalid",
      display_name: "Public Sender",
      account_label: "Private mailbox",
      provider: "gmail",
      created_at: 1,
      updated_at: 1,
    }]);
  });

  afterEach(() => {
    cleanup();
    client.clear();
    resetProfileStorageNamespaceForTests();
  });

  function renderSettings() {
    return render(<QueryClientProvider client={client}><SettingsView /></QueryClientProvider>);
  }

  it("keeps the reminder inside the accounts panel and out of other settings tabs", () => {
    renderSettings();
    const panel = screen.getByRole("tabpanel");
    const reminder = within(panel).getByRole("complementary", { name: "Review your sender names" });
    expect(within(reminder).getByText("Public Sender <sender@example.invalid>")).toBeTruthy();
    expect(within(reminder).queryByText("Private mailbox")).toBeNull();
    expect(within(reminder).queryByRole("button", { name: "Review in account settings" })).toBeNull();
    const editButton = within(panel).getByRole("button", { name: "Edit account" });
    expect(reminder.compareDocumentPosition(editButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "settings.general" }));
    expect(screen.getByText("General settings")).toBeTruthy();
    expect(screen.queryByRole("complementary", { name: "Review your sender names" })).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: "settings.accounts" }));
    expect(screen.getByRole("complementary", { name: "Review your sender names" })).toBeTruthy();
  });

  it("remembers dismissal when account settings are reopened", () => {
    const view = renderSettings();
    fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    expect(profileLocalStorage.getItem("pebble-sender-identity-notice-v1")).toBe("seen");
    expect(screen.queryByRole("complementary", { name: "Review your sender names" })).toBeNull();

    view.unmount();
    renderSettings();
    expect(screen.queryByRole("complementary", { name: "Review your sender names" })).toBeNull();
    expect(screen.getByRole("button", { name: "Edit account" })).toBeTruthy();
  });
});
