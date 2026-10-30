import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import AccountsTab from "@/features/settings/AccountsTab";
import { accountsQueryKey } from "@/hooks/queries";
import { reorderAccounts } from "@/lib/api";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string, options?: Record<string, unknown>) => (fallback ?? key).replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(options?.[name] ?? "")) }) }));
vi.mock("@/hooks/queries", async (original) => ({ ...await original<typeof import("@/hooks/queries")>(), useAccountUnreadCounts: () => ({}) }));
vi.mock("@/lib/api", async (original) => ({ ...await original<typeof import("@/lib/api")>(), reorderAccounts: vi.fn() }));
it("keeps stored ordering stable and disables every unavailable reorder arrow", () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const accounts = ["Work", "Personal", "School"].map((name) => ({ id: name, email: `${name}@example.com`, account_label: name, display_name: name, provider: "imap", created_at: 1, updated_at: 1 }));
  client.setQueryData(accountsQueryKey, accounts);
  render(<QueryClientProvider client={client}><AccountsTab /></QueryClientProvider>);
  for (const name of ["Work", "Personal", "School"]) {
    for (const direction of ["up", "down"]) {
      const button = screen.getByRole("button", { name: `Move ${name} ${direction}` }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      expect(button.title).toMatch(/not available/);
      fireEvent.click(button);
    }
  }
  expect(client.getQueryData(accountsQueryKey)).toEqual(accounts);
  expect(reorderAccounts).not.toHaveBeenCalled();
  expect(screen.getAllByText(/@example\.com$/).map((node) => node.textContent)).toEqual(accounts.map((account) => account.email));
});
