import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createInstance } from "i18next";
import { I18nextProvider } from "react-i18next";
import { beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import ProtocolAccountForm from "@/features/settings/ProtocolAccountForm";
import MicrosoftAccountForm from "@/features/settings/MicrosoftAccountForm";
import ProtocolFolders from "@/features/settings/ProtocolFolders";
import GmailNetwork from "@/features/settings/GmailNetwork";
import ProxyTab from "@/features/settings/ProxyTab";
import { AccountService, FolderService, MicrosoftService } from "@/mygo";
import { useConfirmStore } from "@/stores/confirm.store";
import en from "@/locales/en.json";
import zh from "@/locales/zh.json";

vi.mock("@/mygo", () => ({
  AccountService: { protocolSettings: vi.fn(), saveProtocol: vi.fn(), testProtocolSettings: vi.fn(), network: vi.fn(), setNetwork: vi.fn(), accountNetwork: vi.fn(), setAccountNetwork: vi.fn() },
  FolderService: { management: vi.fn(), manage: vi.fn() },
  MicrosoftService: { status: vi.fn(), authorize: vi.fn(), cancel: vi.fn() },
}));

const account = { id: "a", email: "a@example.com", display_name: "A", provider: "imap" as const, created_at: 1, updated_at: 1 };
const endpoint = { host: "mail.example.com", port: 993, security: "tls", root_ca_pem: "", username: account.email, auth_mode: "password", has_password: true };
const folder = { id: "f", account_id: "a", remote_id: "Projects", name: "Projects", is_system: false };

beforeEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  vi.mocked(AccountService.protocolSettings).mockResolvedValue({ accountId: "a", revision: "saved-revision", imap: endpoint, pop3: endpoint, smtp: { ...endpoint, auth_mode: "disabled" }, selected_folders: ["INBOX"], proxy: { mode: "inherit", proxy: null } });
  vi.mocked(AccountService.saveProtocol).mockResolvedValue(account as never);
  vi.mocked(AccountService.testProtocolSettings).mockResolvedValue({ success: true, message: "Connection verified" });
});

async function localized(node: ReactNode, language = "zh") {
  const i18n = createInstance();
  await i18n.init({ lng: language, fallbackLng: "en", resources: { en: { translation: en }, zh: { translation: zh } }, interpolation: { escapeValue: false } });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    render(<I18nextProvider i18n={i18n}><QueryClientProvider client={client}>{node}</QueryClientProvider></I18nextProvider>);
  });
  return i18n;
}

it("switches protocol labels and a completed test without reloading or discarding unsaved settings", async () => {
  const i18n = await localized(<ProtocolAccountForm account={account} onSaved={vi.fn()} />, "en");
  await screen.findByDisplayValue(endpoint.host);
  fireEvent.change(screen.getByLabelText("IMAP host"), { target: { value: "edited.example.com" } });
  fireEvent.click(screen.getByRole("button", { name: "Test entered settings" }));
  await screen.findByText("Connection verified");

  await act(() => i18n.changeLanguage("zh"));
  expect(screen.getByLabelText("IMAP 服务器")).toHaveValue("edited.example.com");
  expect(screen.getByLabelText("IMAP 密码")).toHaveValue("");
  expect(screen.getByLabelText("代理模式")).toHaveValue("inherit");
  expect(screen.getByLabelText("服务器文件夹名称（每行一个）")).toHaveValue("INBOX");
  expect(screen.getByText("连接验证通过")).toBeInTheDocument();
  expect(AccountService.protocolSettings).toHaveBeenCalledTimes(1);
  expect(AccountService.testProtocolSettings).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "保存协议设置" }));
  await waitFor(() => expect(AccountService.saveProtocol).toHaveBeenCalledWith(expect.objectContaining({
    accountId: "a", revision: "saved-revision",
    config: expect.objectContaining({ imap: expect.objectContaining({ host: "edited.example.com", password: "", security: "tls" }), selected_folders: ["INBOX"], proxy: { mode: "inherit", proxy: null } }),
  })));
});

it("retranslates a load failure while keeping global proxy saving disabled", async () => {
  vi.mocked(AccountService.network).mockRejectedValueOnce(new Error("sensitive transport detail"));
  const i18n = await localized(<ProxyTab />, "en");
  expect(await screen.findByRole("alert")).toHaveTextContent(en.connectionRoute.globalLoadFailed);
  await act(() => i18n.changeLanguage("zh"));
  expect(screen.getByRole("alert")).toHaveTextContent(zh.connectionRoute.globalLoadFailed);
  expect(screen.getByRole("button", { name: "保存全局代理" })).toBeDisabled();
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.queryByText("sensitive transport detail")).toBeNull();
  expect(AccountService.network).toHaveBeenCalledTimes(1);
  expect(AccountService.setNetwork).not.toHaveBeenCalled();
});

it("translates independent Microsoft resource states without changing authorization policy", async () => {
  vi.mocked(MicrosoftService.status).mockResolvedValue({ accountId: "a", clientId: "client", tenant: "common", audience: "all", graph: false, imap: true, smtp: true, graphAuthorized: false, imapAuthorized: true, smtpAuthorized: true, sendState: "reauth_required", writeState: "permission_required" });
  vi.mocked(MicrosoftService.authorize).mockResolvedValue(account as never);
  const i18n = await localized(<MicrosoftAccountForm account={{ ...account, provider: "outlook" }} onSaved={vi.fn()} />);
  expect(await screen.findByText("IMAP：已授权")).toBeInTheDocument();
  expect(screen.getByText(/发信授权：需要重新登录/)).toBeInTheDocument();
  expect(screen.getByText(/邮件修改授权：需要补充权限/)).toBeInTheDocument();
  expect(screen.getByLabelText("租户")).toHaveValue("common");
  expect(screen.getByLabelText("账户类型")).toHaveValue("all");
  expect(screen.queryByText(/reauth_required|permission_required/)).toBeNull();
  await act(() => i18n.changeLanguage("en"));
  expect(screen.getByText("IMAP: authorized")).toBeInTheDocument();
  expect(screen.getByText(/Outgoing authorization: Sign in again/)).toBeInTheDocument();
  expect(MicrosoftService.status).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Authorize selected resources" }));
  await waitFor(() => expect(MicrosoftService.authorize).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", tenant: "common", audience: "all", resource: "exchange", graph: false, imap: true, smtp: true })));
});

it.each(["delete", "dismiss"] as const)("keeps the %s folder confirmation in Chinese and does not act when cancelled", async (action) => {
  vi.mocked(FolderService.management).mockResolvedValue({
    folders: [folder], pending: action === "dismiss" ? { id: "intent", action: "rename", name: "Projects", remoteId: "Projects", createdAt: 1 } : null,
  } as never);
  const confirm = vi.spyOn(useConfirmStore.getState(), "confirm").mockResolvedValue(false);
  await localized(<ProtocolFolders accountId="a" />);
  await screen.findByRole("option", { name: "Projects" });
  if (action === "delete") fireEvent.change(screen.getByLabelText("文件夹"), { target: { value: "f" } });
  else expect(screen.getByRole("status")).toHaveTextContent("文件夹操作的结果尚未确认（重命名文件夹）");
  fireEvent.click(screen.getByRole("button", { name: action === "delete" ? "删除文件夹" : "清除未确认操作记录" }));
  await waitFor(() => expect(confirm).toHaveBeenCalledWith({
    title: action === "delete" ? zh.protocolFolders.deleteTitle : zh.protocolFolders.dismissTitle,
    message: action === "delete" ? zh.protocolFolders.deleteWarning : zh.protocolFolders.dismissWarning,
    destructive: true,
  }));
  expect(FolderService.manage).not.toHaveBeenCalled();
});

it("saves the Gmail account route with Chinese controls and the unchanged service values", async () => {
  vi.mocked(AccountService.accountNetwork).mockResolvedValue({ mode: "inherit", proxy: null });
  vi.mocked(AccountService.setAccountNetwork).mockResolvedValue({});
  await localized(<GmailNetwork accounts={[{ ...account, provider: "gmail" }]} />);
  fireEvent.click(screen.getByRole("button", { name: "Gmail 账户连接方式" }));
  const save = screen.getByRole("button", { name: "保存 Gmail 账户连接方式" });
  await waitFor(() => expect(save).toBeEnabled());
  expect(screen.getByRole("region", { name: "a@example.com — Gmail 连接方式" })).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("代理模式"), { target: { value: "disabled" } });
  fireEvent.click(save);
  expect(await screen.findByRole("status")).toHaveTextContent(zh.connectionRoute.accountSaved);
  expect(AccountService.setAccountNetwork).toHaveBeenCalledExactlyOnceWith({ accountId: "a", setting: { mode: "disabled", proxy: null } });
});
