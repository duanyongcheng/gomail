import { beforeEach, expect, it, vi } from "vitest";
import { AccountService } from "@/mygo";
import { invoke } from "@/lib/platform/core";

vi.mock("@/mygo", () => ({
  AccountService: { network: vi.fn(), setNetwork: vi.fn(), accountNetwork: vi.fn(), setAccountNetwork: vi.fn(), syncFolders: vi.fn(), setSyncFolders: vi.fn(), xoAuth2Status: vi.fn(), login: vi.fn() },
  AppService: {}, AttachmentService: {}, ComposeService: {}, FolderService: {}, MessageService: {}, OutboxService: {}, PendingOpsService: {}, SearchService: {}, SyncService: {}, ThreadService: {}, TrustService: {},
}));
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(AccountService.network).mockResolvedValue({ proxy: { scheme: "socks5", host: "localhost", port: 1080 } });
  vi.mocked(AccountService.accountNetwork).mockResolvedValue({ mode: "inherit", proxy: null });
});
it("resolves the actual effective proxy while keeping inherited settings explicit", async () => {
  expect(await invoke("get_global_proxy")).toEqual({ scheme: "socks5", host: "localhost", port: 1080 });
  expect(await invoke("get_oauth_account_proxy", { accountId: "a" })).toEqual({ scheme: "socks5", host: "localhost", port: 1080 });
  expect(await invoke("get_account_proxy_setting", { accountId: "a" })).toEqual({ mode: "inherit", proxy: null });
  vi.mocked(AccountService.accountNetwork).mockResolvedValue({ mode: "disabled", proxy: null });
  expect(await invoke("get_account_proxy", { accountId: "a" })).toBeNull();
  vi.mocked(AccountService.accountNetwork).mockResolvedValue({ mode: "custom", proxy: { scheme: "http", host: "override", port: 8123 } });
  expect(await invoke("get_account_proxy", { accountId: "a" })).toEqual({ scheme: "http", host: "override", port: 8123 });
});
it("preserves legacy empty/null semantics and explicit inherit/direct/custom routes", async () => {
  await invoke("update_global_proxy", { proxyHost: null, proxyPort: null });
  expect(AccountService.setNetwork).toHaveBeenLastCalledWith({ proxy: null });
  await invoke("update_account_proxy", { accountId: "a", proxyHost: null, proxyPort: null });
  expect(AccountService.setAccountNetwork).toHaveBeenLastCalledWith({ accountId: "a", setting: { mode: "inherit", proxy: null } });
  await invoke("update_oauth_account_proxy_setting", { accountId: "a", mode: "disabled", proxyHost: "stale", proxyPort: 8888 });
  expect(AccountService.setAccountNetwork).toHaveBeenLastCalledWith({ accountId: "a", setting: { mode: "disabled", proxy: null } });
  await invoke("update_account_proxy_setting", { accountId: "a", mode: "custom", proxyHost: "p", proxyPort: 8080 });
  expect(AccountService.setAccountNetwork).toHaveBeenLastCalledWith({ accountId: "a", setting: { mode: "custom", proxy: { scheme: "http", host: "p", port: 8080 } } });
  await invoke("update_account_proxy_setting", { accountId: "a", mode: "custom", proxyHost: "p", proxyPort: 1080, proxyScheme: "socks5" });
  expect(AccountService.setAccountNetwork).toHaveBeenLastCalledWith({ accountId: "a", setting: { mode: "custom", proxy: { scheme: "socks5", host: "p", port: 1080 } } });
  await expect(invoke("update_account_proxy_setting", { accountId: "a", mode: "invalid" })).rejects.toThrow("Invalid proxy mode");
});
it("passes OAuth proxy to the authorization binding, without mutating global settings", async () => {
  await invoke("complete_oauth_flow", { provider: "gmail", email: "a@example.invalid", displayName: "A", proxyHost: "p", proxyPort: 8080 });
  expect(AccountService.login).toHaveBeenCalledWith({ provider: "gmail", email: "a@example.invalid", displayName: "A", accountLabel: null, proxy: { mode: "custom", proxy: { scheme: "http", host: "p", port: 8080 } } });
  expect(AccountService.setNetwork).not.toHaveBeenCalled();
});
it("bridges selected-folder and safe XOAUTH2 status reads and rejects unverified raw imports explicitly", async () => {
  await invoke("get_imap_sync_folders", { accountId: "a" });
  expect(AccountService.syncFolders).toHaveBeenCalledWith({ accountId: "a" });
  await invoke("update_imap_sync_folders", { accountId: "a", selectedRemoteIds: ["INBOX", "Archive"] });
  expect(AccountService.setSyncFolders).toHaveBeenCalledWith({ accountId: "a", selectedRemoteIds: ["INBOX", "Archive"] });
  await invoke("get_xoauth2_status", { accountId: "a" });
  expect(AccountService.xoAuth2Status).toHaveBeenCalledWith({ accountId: "a" });
  await expect(invoke("set_xoauth2_refresh", { accountId: "a", refreshToken: "fixture-only" })).rejects.toThrow("Use Microsoft sign-in in account settings");
});
