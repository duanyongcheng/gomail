import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/mygo", () => ({
  AccountService: { list: vi.fn(), login: vi.fn(), delete: vi.fn(), update: vi.fn(), signature: vi.fn(), setSignature: vi.fn(), migrateSignature: vi.fn() },
  AppService: { profile: vi.fn(), quit: vi.fn(), openExternal: vi.fn().mockResolvedValue({}) },
  AttachmentService: { path: vi.fn(), download: vi.fn() },
  ComposeService: { save: vi.fn(), delete: vi.fn(), stage: vi.fn(), cleanup: vi.fn() },
  OutboxService: { queue: vi.fn().mockResolvedValue({}) },
  FileService: { downloads: vi.fn(), saveAttachment: vi.fn() },
  FolderService: { list: vi.fn() },
  ThreadService: { list: vi.fn(), messages: vi.fn() },
  TrustService: { list: vi.fn(), set: vi.fn(), remove: vi.fn(), isTrusted: vi.fn() },
  MessageService: {
    list: vi.fn(), get: vi.fn(), rendered: vi.fn(), flags: vi.fn(), attachments: vi.fn(), unreadCounts: vi.fn(),
    archive: vi.fn().mockResolvedValue("archived"), delete: vi.fn().mockResolvedValue({}), restore: vi.fn().mockResolvedValue({}),
    move: vi.fn().mockResolvedValue({}), batchFlags: vi.fn().mockResolvedValue(1),
    markAccountAllRead: vi.fn().mockResolvedValue(1), markFolderAllRead: vi.fn().mockResolvedValue(1),
    batchArchive: vi.fn().mockResolvedValue(1), batchDelete: vi.fn().mockResolvedValue(1),
  },
  PendingOpsService: { summary: vi.fn(), list: vi.fn(), dismiss: vi.fn() },
  SearchService: { search: vi.fn() },
  SyncService: { sync: vi.fn().mockResolvedValue({ count: 1 }), trigger: vi.fn().mockResolvedValue({ count: 1 }), start: vi.fn().mockResolvedValue({ value: "a" }), stop: vi.fn().mockResolvedValue({}), preference: vi.fn().mockResolvedValue({}) },
}));
import { AccountService, AppService, AttachmentService, ComposeService, FileService, MessageService, OutboxService, PendingOpsService, SyncService, ThreadService, TrustService } from "@/mygo";
import { downloadDir } from "../../src/lib/platform/path";
import { saveAttachmentAs } from "../../src/lib/platform/attachments";
import { invoke } from "../../src/lib/platform/core";
import { commands } from "../../src/lib/platform/manifest";
import { isFeatureAvailable } from "../../src/lib/availability";

function frontendCommands(path: string): Set<string> {
  const names = new Set<string>();
  for (const entry of readdirSync(path, { withFileTypes: true })) {
    const name = join(path, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "platform") for (const command of frontendCommands(name)) names.add(command);
    } else if (/\.tsx?$/.test(name)) {
      for (const match of readFileSync(name, "utf8").matchAll(/\binvoke(?:<[^;]*?>)?\(\s*["']([^"']+)/g)) names.add(match[1]);
    }
  }
  return names;
}

describe("MyGO command adapter", () => {
  it("covers every invoke command and parity entry", () => {
    const names = [...frontendCommands("src")].sort();
    expect(Object.keys(commands).sort()).toEqual(names);
    const parity = readFileSync("docs/parity.md", "utf8");
    expect([...parity.matchAll(/^\| `([^`]+)`/gm)].map((match) => match[1]).sort()).toEqual(names);
  });
  it("keeps advanced search deferred to M3 in generation and product availability", async () => {
    const root = mkdtempSync(join(tmpdir(), "pebble-command-milestones-"));
    try {
      mkdirSync(join(root, "scripts"));
      mkdirSync(join(root, "src/lib/platform"), { recursive: true });
      copyFileSync("scripts/generate_commands.py", join(root, "scripts/generate_commands.py"));
      writeFileSync(join(root, "src/lib/platform/commands.ts"), "");
      writeFileSync(join(root, "src/example.ts"), 'invoke("advanced_search"); invoke("preview_oauth_identity");');
      execFileSync("python3", [join(root, "scripts/generate_commands.py")]);
      const manifest = readFileSync(join(root, "src/lib/platform/manifest.ts"), "utf8");
      const parity = readFileSync(join(root, "docs/parity.md"), "utf8");
      expect(manifest).toContain('advanced_search: pending("advanced_search", "M3")');
      expect(manifest).toContain('preview_oauth_identity: pending("preview_oauth_identity", "M1")');
      expect(parity).toContain("| `advanced_search` | not-implemented | M3 |");
      expect(readFileSync("docs/parity.md", "utf8")).toContain("| `advanced_search` | not-implemented | M3 |");
      expect(commands.advanced_search).toMatchObject({ status: "not-implemented", milestone: "M3" });
      expect(isFeatureAvailable("advancedSearch")).toBe(false);
      await expect(invoke("advanced_search", { query: {} })).rejects.toThrow("NotImplemented: advanced_search (M3)");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
  it("rejects unknown names and action stubs with their command", async () => {
    await expect(invoke("__unknown__")).rejects.toThrow("NotImplemented: __unknown__");
    await expect(invoke("create_rule")).rejects.toThrow("NotImplemented: create_rule");
    expect(await invoke("list_contacts")).toEqual([]);
  });
  it("adapts a typed rendered message to the existing tuple contract and preserves privacy mode", async () => {
    vi.mocked(MessageService.rendered).mockResolvedValueOnce({ message: { id: "m" }, rendered: { html: "safe" } } as never);
    const request = { messageId: "m", privacyMode: "LoadOnce" };
    expect(await invoke("get_message_with_html", request)).toEqual([{ id: "m" }, { html: "safe" }]);
    expect(MessageService.rendered).toHaveBeenCalledWith(request);
  });
  it("queues mail mutations and pending-op reads through typed services", async () => {
    expect(await invoke("archive_message", { messageId: "m" })).toBe("archived");
    expect(MessageService.archive).toHaveBeenCalledWith({ messageId: "m" });
    await invoke("batch_archive", { messageIds: ["m"] });
    expect(MessageService.batchArchive).toHaveBeenCalledWith({ messageIds: ["m"] });
    await invoke("batch_delete", { messageIds: ["m"] });
    expect(MessageService.batchDelete).toHaveBeenCalledWith({ messageIds: ["m"] });
    await invoke("batch_star", { messageIds: ["m"], starred: true });
    expect(MessageService.batchFlags).toHaveBeenCalledWith({ messageIds: ["m"], starred: true });
    await invoke("mark_folder_all_read", { accountId: "a", folderIds: ["f"] });
    expect(MessageService.markFolderAllRead).toHaveBeenCalledWith({ accountId: "a", folderIds: ["f"] });
    await invoke("list_pending_mail_ops", { accountId: null });
    expect(PendingOpsService.list).toHaveBeenCalledWith({ accountId: "", limit: 100 });
    await invoke("dismiss_failed_pending_mail_ops", { accountId: "a" });
    expect(PendingOpsService.dismiss).toHaveBeenCalledWith({ accountId: "a", limit: 100 });
    await expect(invoke("empty_trash", { accountId: "a" })).rejects.toThrow("Permanent delete");
  });

  it("queues sending through the durable service without claiming remote delivery", async () => {
    expect(await invoke("send_email", { accountId: "a", to: ["to@example.invalid"], clientKey: "k", localDraftId: "local-draft:a" })).toBeUndefined();
    expect(OutboxService.queue).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", clientKey: "k", attachmentPaths: [], localDraftId: "local-draft:a" }));
  });

  it("routes external URLs once and propagates typed service errors", async () => {
    vi.mocked(AppService.openExternal).mockClear();
    const request = { url: "https://example.invalid/path?q=private" };
    expect(await invoke("open_external_url", request)).toBeUndefined();
    expect(AppService.openExternal).toHaveBeenCalledExactlyOnceWith(request);
    vi.mocked(AppService.openExternal).mockRejectedValueOnce(new Error("could not open the link"));
    await expect(invoke("open_external_url", request)).rejects.toThrow("could not open the link");
    expect(AppService.openExternal).toHaveBeenCalledTimes(2);
    expect(commands.open_external_url.status).toBe("implemented");
  });

  it("routes atomic Gmail settings and signature migration through typed services", async () => {
    const update = { accountId: "a", email: "a@example.invalid", displayName: "Sender", signature: "Regards" };
    await invoke("update_account", update);
    expect(AccountService.update).toHaveBeenCalledWith(update);
    await invoke("set_email_signature", { accountId: "a", signature: "" });
    expect(AccountService.setSignature).toHaveBeenCalledWith({ accountId: "a", signature: "" });
    vi.mocked(AccountService.signature).mockResolvedValueOnce("Saved");
    expect(await invoke("get_email_signature", { accountId: "a" })).toBe("Saved");
    vi.mocked(AccountService.migrateSignature).mockResolvedValueOnce("");
    expect(await invoke("migrate_email_signature_if_absent", { accountId: "a", signature: "Legacy" })).toBe("");
    expect(AccountService.migrateSignature).toHaveBeenCalledWith({ accountId: "a", signature: "Legacy" });
  });

  it("routes persistent sender trust with account and requested level", async () => {
    await invoke("trust_sender", { accountId: "a", email: "sender@example.invalid", trustType: "images" });
    expect(TrustService.set).toHaveBeenCalledWith({ accountId: "a", email: "sender@example.invalid", trustType: "images" });
    await invoke("remove_trusted_sender", { accountId: "a", email: "sender@example.invalid" });
    expect(TrustService.remove).toHaveBeenCalledWith({ accountId: "a", email: "sender@example.invalid" });
    vi.mocked(TrustService.isTrusted).mockResolvedValueOnce(true);
    expect(await invoke("is_trusted_sender", { accountId: "a", email: "sender@example.invalid" })).toBe(true);
    await invoke("list_trusted_senders", { accountId: "a" });
    expect(TrustService.list).toHaveBeenCalledWith({ accountId: "a" });
  });

  it("routes account-scoped thread handles and multiple folders", async () => {
    await invoke("list_threads", { folderId: "a-inbox", folderIds: ["a-inbox", "b-inbox"], limit: 20, offset: 20 });
    expect(ThreadService.list).toHaveBeenCalledWith({ folderId: "a-inbox", folderIds: ["a-inbox", "b-inbox"], limit: 20, offset: 20 });
    await invoke("list_thread_messages", { threadId: "thread:opaque" });
    expect(ThreadService.messages).toHaveBeenCalledWith({ threadId: "thread:opaque" });
  });

  it("routes local drafts and staged byte arrays without exposing generic filesystem APIs", async () => {
    vi.mocked(ComposeService.save).mockResolvedValueOnce("local-draft:1");
    expect(await invoke("save_draft", { accountId: "a", subject: "s", clientKey: "key", bodyHtml: null })).toBe("local-draft:1");
    expect(ComposeService.save).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", subject: "s", clientKey: "key", bodyHtml: "", attachmentPaths: [] }));
    await invoke("save_draft", { accountId: "a", editorMode: "markdown", rawSource: "**body**", renderedSource: "**body**", bodyHtml: "<p><strong>body</strong></p>" });
    expect(ComposeService.save).toHaveBeenLastCalledWith(expect.objectContaining({ rawSource: "**body**", renderedSource: "**body**", bodyHtml: "<p><strong>body</strong></p>" }));
    await invoke("stage_compose_attachment", { filename: "file", bytes: [0, 255] });
    expect(ComposeService.stage).toHaveBeenCalledWith({ filename: "file", bytes: [0, 255] });
    await invoke("cleanup_staged_compose_attachment", { path: "compose-attachment:opaque" });
    expect(ComposeService.cleanup).toHaveBeenCalledWith({ path: "compose-attachment:opaque" });
    await invoke("delete_draft", { accountId: "a", draftId: "d" });
    expect(ComposeService.delete).toHaveBeenCalledWith({ accountId: "a", draftId: "d" });
  });

  it("routes attachment commands, Downloads and nullable save dialog results", async () => {
    vi.mocked(AttachmentService.path).mockResolvedValueOnce(null);
    expect(await invoke("get_attachment_path", { attachmentId: "att" })).toBeNull();
    expect(AttachmentService.path).toHaveBeenCalledWith({ attachmentId: "att" });
    vi.mocked(AttachmentService.download).mockResolvedValueOnce("/Downloads/report (1).txt");
    const request = { attachmentId: "att", saveTo: "/Downloads/report.txt" };
    expect(await invoke("download_attachment", request)).toBe("/Downloads/report (1).txt");
    expect(AttachmentService.download).toHaveBeenCalledWith(request);
    vi.mocked(FileService.downloads).mockResolvedValueOnce("/Downloads");
    expect(await downloadDir()).toBe("/Downloads");
    vi.mocked(FileService.saveAttachment).mockResolvedValueOnce(null);
    expect(await saveAttachmentAs("att")).toBeNull();
    expect(FileService.saveAttachment).toHaveBeenCalledWith({ attachmentId: "att" });
  });

  it("routes trigger reasons and scheduler controls through typed services", async () => {
    vi.mocked(SyncService.trigger).mockClear();
    for (const reason of ["window_focus", "network_online", "manual"]) {
      await invoke("trigger_sync", { accountId: "a", reason });
      expect(SyncService.trigger).toHaveBeenLastCalledWith({ accountId: "a", reason });
    }
    expect(await invoke("start_sync", { accountId: "a", pollIntervalSecs: 30 })).toBe("a");
    expect(SyncService.start).toHaveBeenCalledWith({ accountId: "a", pollIntervalSecs: 30 });
    await invoke("stop_sync", { accountId: "a" });
    expect(SyncService.stop).toHaveBeenCalledWith({ accountId: "a" });
    await invoke("set_realtime_preference", { mode: "manual" });
    expect(SyncService.preference).toHaveBeenCalledWith({ mode: "manual" });
  });
});
