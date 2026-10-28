import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/mygo", () => ({
  AccountService: { list: vi.fn(), login: vi.fn(), delete: vi.fn() },
  AppService: { profile: vi.fn(), quit: vi.fn() },
  AttachmentService: { path: vi.fn(), download: vi.fn() },
  ComposeService: { save: vi.fn(), delete: vi.fn(), stage: vi.fn(), cleanup: vi.fn() },
  FileService: { downloads: vi.fn(), saveAttachment: vi.fn() },
  FolderService: { list: vi.fn() },
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
import { AttachmentService, ComposeService, FileService, MessageService, PendingOpsService, SyncService } from "@/mygo";
import { downloadDir } from "../../src/lib/platform/path";
import { saveAttachmentAs } from "../../src/lib/platform/attachments";
import { invoke } from "../../src/lib/platform/core";
import { commands } from "../../src/lib/platform/manifest";

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
  it("rejects unknown names and action stubs with their command", async () => {
    await expect(invoke("__unknown__")).rejects.toThrow("NotImplemented: __unknown__");
    await expect(invoke("send_email")).rejects.toThrow("NotImplemented: send_email");
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

  it("routes local drafts and staged byte arrays without exposing generic filesystem APIs", async () => {
    vi.mocked(ComposeService.save).mockResolvedValueOnce("local-draft:1");
    expect(await invoke("save_draft", { accountId: "a", subject: "s", clientKey: "key", bodyHtml: null })).toBe("local-draft:1");
    expect(ComposeService.save).toHaveBeenCalledWith(expect.objectContaining({ accountId: "a", subject: "s", clientKey: "key", bodyHtml: "", attachmentPaths: [] }));
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
