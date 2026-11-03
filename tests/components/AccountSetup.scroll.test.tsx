import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AccountSetup from "@/components/AccountSetup";
import { AccountService } from "@/mygo";

vi.mock("@/mygo", () => ({ AccountService: { status: vi.fn() } }));
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(AccountService.status).mockResolvedValue({ configured: true, message: "Ready" });
});

async function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(<QueryClientProvider client={client}><AccountSetup onClose={vi.fn()} /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole("button", { name: "Sign in with Google" })).toBeEnabled());
  return container.querySelector<HTMLElement>(".account-setup-scroll")!;
}

describe("account setup scrolling layout", () => {
  it("constrains the body, not just the outer dialog, and keeps the header from shrinking", async () => {
    const body = await setup();
    // jsdom does not perform layout: assert the full sizing chain so a bare
    // overflow:auto body cannot regress to growing beyond its clipped shell.
    expect(body.parentElement).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      maxHeight: "90vh",
      overflow: "hidden",
    });
    expect(body.previousElementSibling).toHaveStyle({ flexShrink: "0" });
    expect(body).toHaveStyle({ flex: "1 1 auto", minHeight: "0", overflowY: "auto" });
    expect(body).toHaveClass("scroll-region");
    expect(body).not.toContainElement(screen.getByRole("button", { name: "Close" }));
  });

  it.each([
    ["imap", "Add protocol account"],
    ["pop3", "Add protocol account"],
    ["outlook", "Authorize selected resources"],
  ])("keeps the lower %s form controls inside the scroll body as fields expand", async (provider, action) => {
    const body = await setup();
    fireEvent.change(screen.getByLabelText("Provider"), { target: { value: provider } });
    fireEvent.change(screen.getByLabelText("Proxy mode"), { target: { value: "custom" } });
    expect(within(body).getByLabelText("Proxy host")).toBeInTheDocument();
    expect(within(body).getByRole("button", { name: action })).toBeInTheDocument();
    if (provider !== "outlook") {
      fireEvent.change(screen.getByLabelText("SMTP authentication"), { target: { value: "password" } });
      expect(within(body).getByLabelText("SMTP password")).toBeInTheDocument();
      expect(within(body).getByRole("button", { name: "Test entered settings" })).toBeInTheDocument();
    }
    expect(body).toHaveStyle({ flex: "1 1 auto", minHeight: "0", overflowY: "auto" });
  });
});
