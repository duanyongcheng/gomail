import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { getContactByEmail } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";
import ContactAddressAction from "@/components/ContactAddressAction";
const mocks = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/lib/api", () => ({ getContactByEmail: vi.fn() }));
vi.mock("@/hooks/queries", () => ({ useAccountsQuery: () => ({ data: [{ id: "a", email: "me@example.com" }] }) }));
vi.mock("@/hooks/mutations", () => ({ useContactMutations: () => ({ save: { mutateAsync: mocks.save, isPending: false } }) }));
function setup(address: string, cached = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cached) client.setQueryData(["contacts", "by-address", address], { id: "old", display_name: "Alice", emails: [{ address }] });
  render(<QueryClientProvider client={client}><ContactAddressAction accountId="a" name="Alice" address={address} /></QueryClientProvider>);
}
beforeEach(() => { vi.clearAllMocks(); useUIStore.setState({ activeView: "inbox", pendingContactId: null }); });
it.each([false, true])("blocks contact editing/navigation without even a lookup (cached=%s)", (cached) => {
  setup("alice@example.com", cached);
  const button = screen.getByRole("button");
  expect(button).toBeDisabled();
  expect(button.getAttribute("title")).toMatch(/not available/);
  fireEvent.click(button);
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(getContactByEmail).not.toHaveBeenCalled();
  expect(mocks.save).not.toHaveBeenCalled();
  expect(useUIStore.getState().activeView).toBe("inbox");
});
it("does not offer contact actions for the current account address", () => {
  setup("ME@example.com");
  expect(screen.queryByRole("button")).toBeNull();
  expect(getContactByEmail).not.toHaveBeenCalled();
});
