import { beforeEach, describe, expect, it, vi } from "vitest";
import { commands } from "../../src/lib/platform/manifest";
import { invoke } from "../../src/lib/platform/core";
import { isCommandAvailable, isFeatureAvailable, unavailableReason, type Feature } from "../../src/lib/availability";
import { AccountService, GmailLabelService, OutboxService } from "../../src/mygo";
import { changeCloudDraft } from "../../src/lib/platform/compose";
import { call } from "mygo-runtime";
import i18n from "../../src/lib/i18n";

// Mock only native transport: real API -> command manifest -> generated binding remains in use.
vi.mock("mygo-runtime", () => ({ call: vi.fn(), event: vi.fn() }));

const unavailable: Feature[] = ["contacts", "kanban", "snooze", "localLabels", "templates", "ai", "translation", "advancedSearch", "proxy", "rules", "backup", "notifications", "autostart", "startHidden", "accountReorder", "imap", "pop3", "outlook", "permanentDelete", "backgroundImage"];
const available: Feature[] = ["gmailLabels", "gmailReconnect", "localCompose", "cloudDraftCopies", "outbox", "signatures"];

describe("production adapter availability", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(call).mockImplementation(async (method) => {
      switch (method) {
        case "SearchService.Search": case "OutboxService.List": return [];
        case "ComposeService.Save": return "local-draft:synthetic";
        case "AccountService.Signature": return "Regards";
        case "AccountService.Reconnect": return {};
        case "GmailLabelService.List": return { folders: [], pending: null };
        default: throw new Error(`Unexpected native request: ${method}`);
      }
    });
  });
  it("uses the actual manifest classification, never startup defaults", () => {
    for (const [name, entry] of Object.entries(commands)) {
      expect(isCommandAvailable(name), name).toBe(entry.status === "implemented");
    }
    expect(isCommandAvailable("__unknown__")).toBe(false);
    expect(isCommandAvailable("toString")).toBe(false);
  });
  it.each(unavailable)("does not advertise %s", (feature) => {
    expect(isFeatureAvailable(feature)).toBe(false);
  });
  it.each(available)("retains working %s", (feature) => {
    expect(isFeatureAvailable(feature)).toBe(true);
  });
  it("distinguishes an implemented rejecting command from authorized permanent deletion", async () => {
    expect(isCommandAvailable("empty_trash")).toBe(true);
    expect(isFeatureAvailable("permanentDelete")).toBe(false);
    await expect(invoke("empty_trash", { accountId: "synthetic" })).rejects.toThrow("Permanent delete");
    expect(call).not.toHaveBeenCalled();
  });
  it("unsupported action adapters reject rather than reaching a native service", async () => {
    for (const command of ["advanced_search", "reorder_accounts", "create_rule", "snooze_message"]) {
      await expect(invoke(command)).rejects.toThrow(`NotImplemented: ${command}`);
    }
    expect(await invoke("list_contacts")).toEqual([]);
    expect(isFeatureAvailable("contacts")).toBe(false);
    expect(call).not.toHaveBeenCalled();
  });
  it("working search, local save and signatures route to typed generated services", async () => {
    await invoke("search_messages", { query: "synthetic", accountId: "a" });
    expect(call).toHaveBeenLastCalledWith("SearchService.Search", { query: "synthetic", accountId: "a", limit: 50 });
    await invoke("save_draft", { accountId: "a", clientKey: "synthetic" });
    expect(call).toHaveBeenLastCalledWith("ComposeService.Save", expect.objectContaining({ accountId: "a", clientKey: "synthetic" }));
    await invoke("get_email_signature", { accountId: "a" });
    expect(call).toHaveBeenLastCalledWith("AccountService.Signature", { accountId: "a" });
  });
  it("explicit feature exceptions correspond to actual generated service routes", async () => {
    await GmailLabelService.list({ accountId: "a" });
    expect(call).toHaveBeenLastCalledWith("GmailLabelService.List", { accountId: "a" });
    await AccountService.reconnect({ accountId: "a" });
    expect(call).toHaveBeenLastCalledWith("AccountService.Reconnect", { accountId: "a" });
    vi.mocked(call).mockResolvedValueOnce({ state: "synced" });
    expect(await changeCloudDraft("a", "d", "check")).toEqual({ state: "synced" });
    expect(call).toHaveBeenLastCalledWith("ComposeService.Cloud", { accountId: "a", draftId: "d", action: "check" });
    vi.mocked(call).mockResolvedValueOnce({ state: "synced" });
    expect(await changeCloudDraft("a", "d", "upload")).toEqual({ state: "synced" });
    expect(call).toHaveBeenLastCalledWith("ComposeService.Cloud", { accountId: "a", draftId: "d", action: "upload" });
    await OutboxService.list({ accountId: "a" });
    expect(call).toHaveBeenLastCalledWith("OutboxService.List", { accountId: "a" });
  });
  it("localizes the explanation", async () => {
    await i18n.changeLanguage("zh");
    expect(unavailableReason()).toContain("暂不支持");
    await i18n.changeLanguage("en");
    expect(unavailableReason()).toContain("not available");
  });
});
