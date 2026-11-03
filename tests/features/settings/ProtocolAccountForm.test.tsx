import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ProtocolAccountForm from "@/features/settings/ProtocolAccountForm";
import MicrosoftAccountForm from "@/features/settings/MicrosoftAccountForm";
import { AccountService, MicrosoftService } from "@/mygo";
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/mygo", () => ({ AccountService: { protocolSettings: vi.fn(), saveProtocol: vi.fn(), test: vi.fn(), testProtocolSettings: vi.fn(), testStored: vi.fn(), add: vi.fn() }, FolderService: { list: vi.fn() }, MicrosoftService: { status: vi.fn(), authorize: vi.fn(), cancel: vi.fn() } }));
const account = { id: "a", email: "a@example.com", display_name: "A", provider: "imap" as const, created_at: 1, updated_at: 1 };
const endpoint = { host: "mail.example.com", port: 993, security: "tls", root_ca_pem: "", username: account.email, auth_mode: "password", has_password: true };
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(AccountService.protocolSettings).mockResolvedValue({ accountId: "a", revision: "ciphertext-digest", imap: endpoint, pop3: endpoint, smtp: { ...endpoint, auth_mode: "disabled" }, selected_folders: ["INBOX"], proxy: { mode: "inherit", proxy: null } });
  vi.mocked(AccountService.saveProtocol).mockResolvedValue(account as never);
});
it("tests edited password accounts with the saved revision and blank-password retention", async () => {
  vi.mocked(AccountService.testProtocolSettings).mockResolvedValue({ success: true, message: "Connection verified" });
  render(<ProtocolAccountForm account={account} onSaved={vi.fn()} />);
  await screen.findByDisplayValue(endpoint.host);
  fireEvent.click(screen.getByRole("button", { name: "Test entered settings" }));
  await screen.findByText("Connection verified");
  expect(AccountService.testProtocolSettings).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", revision: "ciphertext-digest", config: expect.objectContaining({ imap: expect.objectContaining({ password: "" }) }) }));
  expect(AccountService.test).not.toHaveBeenCalled();
});
it("tests Microsoft through the saved provider and exposes independent OAuth choices on IMAP accounts", async () => {
  vi.mocked(AccountService.protocolSettings).mockResolvedValueOnce({ accountId: "a", revision: "r", imap: { ...endpoint, auth_mode: "xoauth2" }, pop3: endpoint, smtp: { ...endpoint, auth_mode: "disabled" }, selected_folders: ["INBOX"], proxy: { mode: "inherit", proxy: null }, microsoft_client_id: "client" });
  vi.mocked(AccountService.testStored).mockResolvedValue({ success: true, message: "Connection verified" });
  render(<ProtocolAccountForm account={account} onSaved={vi.fn()} />);
  await screen.findByRole("button", { name: "Test saved connection" });
  expect((screen.getByLabelText("IMAP authentication") as HTMLSelectElement).value).toBe("xoauth2");
  expect(screen.queryByLabelText("IMAP password")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Test saved connection" }));
  await screen.findByText("Connection verified");
  expect(AccountService.testStored).toHaveBeenCalledWith({ accountId: "a" });
  expect(AccountService.testProtocolSettings).not.toHaveBeenCalled();
});
it("keeps secrets blank and saves trusted configuration with revision and selected folders", async () => {
  const onSaved = vi.fn();
  render(<ProtocolAccountForm account={account} onSaved={onSaved} />);
  await waitFor(() => expect((screen.getByLabelText("IMAP host") as HTMLInputElement).value).toBe(endpoint.host));
  expect((screen.getByLabelText("IMAP password") as HTMLInputElement).value).toBe("");
  expect(screen.queryByRole("option", { name: /plain/i })).toBeNull();
  fireEvent.change(screen.getByLabelText("Remote folder names (one per line)"), { target: { value: "INBOX\nProjects" } });
  fireEvent.click(screen.getByRole("button", { name: "Save protocol settings" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
  expect(AccountService.saveProtocol).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", revision: "ciphertext-digest", config: expect.objectContaining({ selected_folders: ["INBOX", "Projects"], imap: expect.objectContaining({ password: "", security: "tls" }) }) }));
  expect(JSON.stringify(vi.mocked(AccountService.saveProtocol).mock.calls)).not.toContain("has_password");
});
it("requires successful settings load before a stale account can be saved", async () => {
  vi.mocked(AccountService.protocolSettings).mockRejectedValueOnce(new Error("opaque secret"));
  render(<ProtocolAccountForm account={account} onSaved={vi.fn()} />);
  await screen.findByRole("alert");
  fireEvent.submit(screen.getByRole("form", { name: "Protocol account settings" }));
  expect(AccountService.saveProtocol).not.toHaveBeenCalled();
  expect(screen.queryByText("opaque secret")).toBeNull();
});
it("keeps duplicate save actions locked while native persistence is pending", async () => {
  let resolve!: (value: never) => void;
  vi.mocked(AccountService.saveProtocol).mockReturnValue(new Promise((done) => { resolve = done; }));
  const { unmount } = render(<ProtocolAccountForm account={account} onSaved={vi.fn()} />);
  await screen.findByDisplayValue(endpoint.host);
  const form = screen.getByRole("form", { name: "Protocol account settings" });
  fireEvent.submit(form); fireEvent.submit(form);
  expect(AccountService.saveProtocol).toHaveBeenCalledOnce();
  unmount(); resolve(account as never);
});
it("adds a receive-only POP3 account without enabling remote folder or plaintext controls", async () => {
  vi.mocked(AccountService.add).mockResolvedValue({ ...account, provider: "pop3" } as never);
  render(<ProtocolAccountForm provider="pop3" onSaved={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "pop@example.com" } });
  fireEvent.change(screen.getByLabelText("POP3 host"), { target: { value: "pop.example.com" } });
  fireEvent.change(screen.getByLabelText("POP3 username"), { target: { value: "pop@example.com" } });
  fireEvent.change(screen.getByLabelText("POP3 password"), { target: { value: "fixture-only" } });
  expect(screen.queryByText("Selected folders")).toBeNull();
  expect(screen.queryByRole("option", { name: "Plaintext" })).toBeNull();
  fireEvent.submit(screen.getByRole("form", { name: "Protocol account settings" }));
  await waitFor(() => expect(AccountService.add).toHaveBeenCalledOnce());
  expect(AccountService.add).toHaveBeenCalledWith(expect.objectContaining({ provider: "pop3", config: expect.objectContaining({ pop3: expect.objectContaining({ password: "fixture-only", security: "tls" }), smtp: expect.objectContaining({ auth_mode: "disabled" }) }) }));
});
it("Microsoft collects public client policy and independent Exchange options without tokens or secrets", async () => {
  vi.mocked(MicrosoftService.authorize).mockResolvedValue(account as never);
  render(<MicrosoftAccountForm onSaved={vi.fn()} />);
  expect(document.querySelector('input[type="password"]')).toBeNull();
  fireEvent.change(screen.getByLabelText("Public client ID"), { target: { value: "client-id" } });
  fireEvent.click(screen.getByLabelText("SMTP XOAUTH2"));
  fireEvent.click(screen.getByRole("button", { name: "Authorize selected resources" }));
  await waitFor(() => expect(MicrosoftService.authorize).toHaveBeenCalledOnce());
  expect(MicrosoftService.authorize).toHaveBeenCalledWith(expect.objectContaining({ resource: "exchange", graph: false, imap: true, smtp: true, proxy: { mode: "inherit", proxy: null } }));
});
it("shows an isolated outgoing authorization failure alongside healthy IMAP status", async () => {
  vi.mocked(MicrosoftService.status).mockResolvedValue({ accountId: "a", clientId: "client", tenant: "common", audience: "all", graph: false, imap: true, smtp: true, graphAuthorized: false, imapAuthorized: true, smtpAuthorized: true, sendState: "reauth_required", writeState: "permission_required" });
  render(<MicrosoftAccountForm account={{ ...account, provider: "outlook" }} onSaved={vi.fn()} />);
  await screen.findByText(/Outgoing authorization: Sign in again/);
  expect(screen.getByText(/Mail change authorization: Permission required/)).toBeDefined();
  expect(screen.getByText(/IMAP: authorized/)).toBeDefined();
  expect(screen.getByRole("button", { name: "Authorize selected resources" })).toBeDefined();
  expect(document.querySelector('input[type="password"]')).toBeNull();
});
