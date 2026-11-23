import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SenderIdentityNotice from "../../src/components/SenderIdentityNotice";
import { useAccountsQuery } from "../../src/hooks/queries";
import { profileLocalStorage } from "../../src/lib/profileStorage";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }) }));
vi.mock("../../src/hooks/queries", () => ({ useAccountsQuery: vi.fn() }));
vi.mock("../../src/lib/profileStorage", () => ({ profileLocalStorage: { getItem: vi.fn(), setItem: vi.fn() } }));

describe("sender identity upgrade notice", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(useAccountsQuery).mockReturnValue({ data: [
      { id: "smtp", provider: "imap", email: "sender@example.com", display_name: "Public Name", account_label: "Private Description" },
      { id: "outlook", provider: "outlook", email: "outlook@example.com", display_name: "Ignored Name" },
    ] } as ReturnType<typeof useAccountsQuery>);
  });

  it("previews outgoing names without exposing local labels or Outlook overrides", () => {
    render(<SenderIdentityNotice />);
    expect(screen.getByRole("heading", { name: "Review your sender names", level: 3 })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Review in account settings" })).toBeNull();
    expect(screen.getByText("Public Name <sender@example.com>")).toBeTruthy();
    expect(screen.queryByText(/Private Description/)).toBeNull();
    expect(screen.queryByText(/Ignored Name/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    expect(profileLocalStorage.setItem).toHaveBeenCalledWith("gomail-sender-identity-notice-v1", "seen");
    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("remains dismissed when opened again in the same profile", () => {
    vi.mocked(profileLocalStorage.getItem).mockReturnValue("seen");
    render(<SenderIdentityNotice />);
    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("does not show a reminder without an eligible sender name", () => {
    vi.mocked(useAccountsQuery).mockReturnValue({ data: [
      { id: "gmail", provider: "gmail", email: "sender@example.com", display_name: "  " },
      { id: "outlook", provider: "outlook", email: "outlook@example.com", display_name: "Provider name" },
    ] } as ReturnType<typeof useAccountsQuery>);
    render(<SenderIdentityNotice />);
    expect(screen.queryByRole("complementary")).toBeNull();
  });
});
