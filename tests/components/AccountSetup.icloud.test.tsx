import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import AccountSetup from "@/components/AccountSetup";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/mygo", () => ({ AccountService: { status: vi.fn().mockResolvedValue({ configured: true }) } }));
it("does not offer legacy password-provider presets or collect app passwords", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><AccountSetup onClose={vi.fn()} /></QueryClientProvider>);
  await waitFor(() => expect((screen.getByRole("button", { name: "Sign in with Google" }) as HTMLButtonElement).disabled).toBe(false));
  for (const name of ["iCloud", "QQ", "163", "Outlook", "Gmail"]) expect(screen.queryByRole("button", { name, exact: true })).toBeNull();
  expect(document.querySelector('input[type="password"]')).toBeNull();
  expect(screen.queryByTestId("icloud-password-hint")).toBeNull();
});
