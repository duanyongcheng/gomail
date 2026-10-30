import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AccountsTab from "../../../src/features/settings/AccountsTab";
import {
  getOAuthAccountProxySetting,
  updateAccount,
  updateOAuthAccountProxySetting,
} from "../../../src/lib/api";

import { clearLegacySignature, getSignature } from "../../../src/lib/signatures";

const fixture = vi.hoisted(() => ({ provider: "outlook" }));

vi.mock("../../../src/features/settings/AccountConnections", () => ({ default: () => null }));
vi.mock("../../../src/features/settings/MailDownloadFailures", () => ({ default: () => null }));
vi.mock("../../../src/features/settings/GmailLabels", () => ({ default: () => null }));

vi.mock("react-i18next", () => ({
  initReactI18next: {
    type: "3rdParty",
    init: vi.fn(),
  },
  useTranslation: () => ({
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
  }),
}));

vi.mock("../../../src/hooks/queries", () => ({
  accountsQueryKey: ["accounts"],
  useAccountsQuery: () => ({
    data: [
      {
        id: "account-1",
        email: "user@example.com",
        display_name: "User",
        provider: fixture.provider,
        color: "#22c55e",
        created_at: 1,
        updated_at: 1,
      },
    ],
  }),
  useAccountUnreadCounts: () => ({}),
  unreadCountForAccount: () => 0,
}));

// Proxy/colour editing does not exercise mark-all-read; it has its own suite
// (MarkAllReadButton.test.tsx).
vi.mock("../../../src/components/MarkAllReadButton", () => ({
  default: () => null,
}));

vi.mock("../../../src/lib/api", () => ({
  deleteAccount: vi.fn(),
  getOAuthAccountProxy: vi.fn(() => Promise.resolve(null)),
  getOAuthAccountProxySetting: vi.fn(),
  testAccountConnection: vi.fn(),
  updateAccount: vi.fn(),
  updateOAuthAccountProxy: vi.fn(() => Promise.resolve(undefined)),
  updateOAuthAccountProxySetting: vi.fn(),
}));

vi.mock("../../../src/lib/signatures", () => ({
  clearLegacySignature: vi.fn(),
  getSignature: vi.fn(() => Promise.resolve("")),
  setSignature: vi.fn(() => Promise.resolve()),
}));

vi.mock("../../../src/stores/mail.store", () => ({
  useMailStore: {
    getState: () => ({
      activeAccountId: null,
      setActiveAccountId: vi.fn(),
    }),
  },
}));

vi.mock("../../../src/stores/ui.store", () => ({
  useUIStore: (selector: (state: { realtimeStatusByAccount: Record<string, unknown> }) => unknown) =>
    selector({ realtimeStatusByAccount: {} }),
}));

vi.mock("../../../src/stores/toast.store", () => ({
  useToastStore: {
    getState: () => ({
      addToast: vi.fn(),
    }),
  },
}));

describe("AccountsTab supported local settings", () => {
  it("edits a local label independently and keeps the OAuth address read-only", async () => {
    render(<AccountsTab />);
    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));
    const label = await screen.findByLabelText("Account label") as HTMLInputElement;
    fireEvent.change(label, { target: { value: "Work only" } });
    expect((screen.getByLabelText("accountSetup.emailAddress") as HTMLInputElement).readOnly).toBe(true);
    await waitFor(() => expect(screen.getByRole("button", { name: "common.save" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));
    await waitFor(() => expect(updateAccount).toHaveBeenCalled());
    const args = vi.mocked(updateAccount).mock.calls[0];
    expect(args[1]).toBe("user@example.com");
    expect(args[2]).toBe("User");
    expect(args[14]).toBe("Work only");
  });
  beforeEach(() => {
    vi.clearAllMocks();
    fixture.provider = "gmail";
    vi.mocked(getSignature).mockResolvedValue("");
    vi.mocked(getOAuthAccountProxySetting).mockResolvedValue({
      mode: "custom",
      proxy: { host: "127.0.0.1", port: 7890 },
    });
    vi.mocked(updateAccount).mockResolvedValue(undefined);
    vi.mocked(updateOAuthAccountProxySetting).mockResolvedValue(undefined);
  });

  it("does not expose Outlook editing or request proxy settings", () => {
    fixture.provider = "outlook";
    render(<AccountsTab />);
    const edit = screen.getByRole("button", { name: "Edit account" });
    expect(edit).toBeDisabled();
    expect(edit).toHaveAttribute("title", expect.any(String));
    fireEvent.click(edit);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(getOAuthAccountProxySetting).not.toHaveBeenCalled();
    expect(getSignature).not.toHaveBeenCalled();
    expect(updateAccount).not.toHaveBeenCalled();
    expect(updateOAuthAccountProxySetting).not.toHaveBeenCalled();
  });

  it("saves a custom account color from the edit dialog", async () => {
    render(<AccountsTab />);

    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));

    const colorInput = await screen.findByLabelText("Account color");
    expect((colorInput as HTMLInputElement).value).toBe("#22c55e");
    await waitFor(() => expect(screen.getByRole("button", { name: "common.save" })).not.toBeDisabled());

    fireEvent.change(colorInput, {
      target: { value: "#f97316" },
    });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(updateAccount).toHaveBeenCalledWith(
        "account-1",
        "user@example.com",
        "User",
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        "#f97316",
        "",
        "",
      );
    });
  });

  it("offers built-in color presets in the edit dialog", async () => {
    render(<AccountsTab />);

    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));

    const preset = await screen.findByRole("button", { name: "Use color #0ea5e9" });
    fireEvent.click(preset);
    await waitFor(() => expect(screen.getByRole("button", { name: "common.save" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(updateAccount).toHaveBeenCalledWith(
        "account-1",
        "user@example.com",
        "User",
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined,
        "#0ea5e9",
        "",
        "",
      );
    });
  });

  it("keeps the edit-account dialog open when clicking the backdrop", async () => {
    render(<AccountsTab />);

    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));

    const dialog = await screen.findByRole("dialog", { name: "Edit Account" });
    fireEvent.mouseDown(dialog);
    fireEvent.click(dialog);

    expect(screen.queryByRole("dialog", { name: "Edit Account" })).not.toBeNull();
  });
});

describe("Gmail local account settings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fixture.provider = "gmail";
    vi.mocked(getSignature).mockReset().mockResolvedValue("Saved signature");
    vi.mocked(updateAccount).mockReset().mockResolvedValue(undefined);
  });

  async function edit() {
    render(<AccountsTab />);
    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "common.save" })).not.toBeDisabled());
  }

  it("saves sender, label, color and signature together without proxy dependencies", async () => {
    await edit();
    expect(screen.queryByLabelText("SOCKS5 Proxy")).toBeNull();
    expect(screen.queryByRole("button", { name: "Verify mailbox" })).toBeNull();
    expect(getOAuthAccountProxySetting).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("accountSetup.displayName"), { target: { value: "New sender" } });
    fireEvent.change(screen.getByLabelText("Account label"), { target: { value: "Local label" } });
    fireEvent.change(screen.getByLabelText("Account color"), { target: { value: "#f97316" } });
    fireEvent.change(screen.getByLabelText("Signature"), { target: { value: "New signature" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(updateAccount).toHaveBeenCalledExactlyOnceWith(
      "account-1", "user@example.com", "New sender",
      undefined, undefined, undefined, undefined, undefined,
      undefined, undefined, undefined, undefined, undefined,
      "#f97316", "Local label", "New signature",
    );
    expect(updateOAuthAccountProxySetting).not.toHaveBeenCalled();
    expect(clearLegacySignature).toHaveBeenCalledWith("account-1");
  });

  it("blocks a failed signature read and allows retry before saving", async () => {
    vi.mocked(getSignature).mockRejectedValueOnce(new Error("Keyring locked"));
    render(<AccountsTab />);
    fireEvent.click(screen.getByRole("button", { name: "Edit account" }));
    await screen.findByText("Keyring locked");
    expect(screen.getByRole("button", { name: "common.save" })).toBeDisabled();
    fireEvent.submit(screen.getByRole("button", { name: "common.save" }).closest("form")!);
    expect(updateAccount).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByLabelText("Signature")).toHaveValue("Saved signature"));
    expect(screen.getByRole("button", { name: "common.save" })).not.toBeDisabled();
  });

  it("guards submissions and retains the form and legacy copy on failed save", async () => {
    let reject!: (error: Error) => void;
    vi.mocked(updateAccount).mockImplementationOnce(() => new Promise((_resolve, rejectSave) => { reject = rejectSave; }));
    await edit();
    fireEvent.change(screen.getByLabelText("Signature"), { target: { value: "Keep edited signature" } });
    const form = screen.getByRole("button", { name: "common.save" }).closest("form")!;
    fireEvent.submit(form);
    fireEvent.submit(form);
    expect(updateAccount).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Signature")).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    reject(new Error("Save failed"));
    await screen.findByText("Save failed");
    expect(screen.getByLabelText("Signature")).toHaveValue("Keep edited signature");
    expect(clearLegacySignature).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
