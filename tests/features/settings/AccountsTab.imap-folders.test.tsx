import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import AccountsTab from "@/features/settings/AccountsTab";
import { accountsQueryKey } from "@/hooks/queries";
import { getImapSyncFolders, updateImapSyncFolders, updateAccount, testAccountConnection } from "@/lib/api";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/hooks/queries", async (original) => ({ ...await original<typeof import("@/hooks/queries")>(), useAccountUnreadCounts: () => ({}) }));
vi.mock("@/lib/api", async (original) => ({ ...await original<typeof import("@/lib/api")>(), getImapSyncFolders: vi.fn(), updateImapSyncFolders: vi.fn(), updateAccount: vi.fn(), testAccountConnection: vi.fn() }));
it.each(["imap", "pop3", "outlook"])("does not mount unsupported %s forms or run connection tests", (provider) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(accountsQueryKey, [{ id: "a", email: "user@example.com", display_name: "User", provider, created_at: 1, updated_at: 1 }]);
  render(<QueryClientProvider client={client}><AccountsTab /></QueryClientProvider>);
  for (const name of ["Edit account", "Test Connection"]) {
    const button = screen.getByRole("button", { name }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.title).toMatch(/not available/);
    fireEvent.click(button);
  }
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.queryByRole("button", { name: "Choose folders" })).toBeNull();
  for (const request of [getImapSyncFolders, updateImapSyncFolders, updateAccount, testAccountConnection]) expect(request).not.toHaveBeenCalled();
});
