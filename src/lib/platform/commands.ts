import { AccountService, AppService, AttachmentService, ComposeService, FolderService, MessageService, OutboxService, PendingOpsService, SearchService, SyncService } from "@/mygo";
import type { AccountRequest, FlagsRequest, LoginRequest, MessageRequest, MessagesRequest, SearchRequest, SaveDraftRequest, SendRequest } from "@/mygo";

type Entry<A, R> = { status: "implemented" | "startup-stub" | "not-implemented"; milestone: string; run: (args: A) => Promise<R> };
function implemented<A, R>(run: (args: A) => Promise<R>, milestone = "M0"): Entry<A, R> { return { status: "implemented", milestone, run }; }
export function startup<R>(value: R, milestone: string): Entry<Record<string, unknown>, R> {
  return { status: "startup-stub", milestone, run: async () => structuredClone(value) };
}
export function pending(name: string, milestone: string): Entry<Record<string, unknown>, never> {
  return { status: "not-implemented", milestone, run: async () => { throw new Error(`NotImplemented: ${name} (${milestone})`); } };
}

export const implementedCommands = {
  list_accounts: implemented(() => AccountService.list({})),
  complete_oauth_flow: implemented((a: LoginRequest & { proxyHost?: string }) => {
    if (a.proxyHost) return Promise.reject(new Error("NotImplemented: OAuth proxy (M2)"));
    return AccountService.login({ ...a, accountLabel: a.accountLabel ?? null });
  }),
  delete_account: implemented((a: AccountRequest) => AccountService.delete(a)),
  list_folders: implemented((a: AccountRequest) => FolderService.list(a)),
  list_messages: implemented((a: Partial<MessagesRequest>) => MessageService.list({ folderId: a.folderId ?? "", accountId: a.accountId ?? "", limit: a.limit ?? 50, offset: a.offset ?? 0, starred: false })),
  list_starred_messages: implemented((a: Partial<MessagesRequest>) => MessageService.list({ folderId: "", accountId: a.accountId ?? "", limit: a.limit ?? 50, offset: a.offset ?? 0, starred: true })),
  get_message: implemented((a: MessageRequest) => MessageService.get(a)),
  get_message_with_html: implemented(async (a: MessageRequest) => { const result = await MessageService.rendered(a); return [result.message, result.rendered] as const; }),
  get_rendered_html: implemented(async (a: MessageRequest) => (await MessageService.rendered(a)).rendered),
  update_message_flags: implemented((a: FlagsRequest) => MessageService.flags({ messageId: a.messageId, isRead: a.isRead ?? null, isStarred: a.isStarred ?? null })),
  archive_message: implemented((a: { messageId: string }) => MessageService.archive(a), "M1"),
  delete_message: implemented((a: { messageId: string }) => MessageService.delete(a), "M1"),
  restore_message: implemented((a: { messageId: string }) => MessageService.restore(a), "M1"),
  move_to_folder: implemented((a: { messageId: string; targetFolderId: string }) => MessageService.move(a), "M1"),
  batch_archive: implemented((a: { messageIds: string[] }) => MessageService.batchArchive({ messageIds: a.messageIds }), "M1"),
  batch_delete: implemented((a: { messageIds: string[] }) => MessageService.batchDelete({ messageIds: a.messageIds }), "M1"),
  batch_mark_read: implemented((a: { messageIds: string[]; isRead: boolean }) => MessageService.batchFlags({ messageIds: a.messageIds, isRead: a.isRead }), "M1"),
  batch_star: implemented((a: { messageIds: string[]; starred: boolean }) => MessageService.batchFlags({ messageIds: a.messageIds, starred: a.starred }), "M1"),
  mark_account_all_read: implemented((a: AccountRequest) => MessageService.markAccountAllRead(a), "M1"),
  mark_folder_all_read: implemented((a: { accountId: string; folderIds: string[] }) => MessageService.markFolderAllRead(a), "M1"),
  empty_trash: implemented(async () => { throw new Error("Permanent delete is not available with Gmail modify access"); }, "M1"),
  get_messages_batch: implemented(async (a: { messageIds: string[] }) => {
    const messages = await Promise.all(a.messageIds.map((messageId) => MessageService.get({ messageId })));
    return messages.filter(Boolean);
  }, "M1"),
  get_pending_mail_ops_summary: implemented((a: { accountId: string | null }) => PendingOpsService.summary({ accountId: a.accountId ?? "", limit: 100 }), "M1"),
  list_pending_mail_ops: implemented((a: { accountId: string | null; limit?: number }) => PendingOpsService.list({ accountId: a.accountId ?? "", limit: a.limit ?? 100 }), "M1"),
  dismiss_failed_pending_mail_ops: implemented((a: { accountId: string | null }) => PendingOpsService.dismiss({ accountId: a.accountId ?? "", limit: 100 }), "M1"),
  send_email: implemented(async (a: Partial<SendRequest>) => { await OutboxService.queue({
    accountId: a.accountId ?? "", to: a.to ?? [], cc: a.cc ?? [], bcc: a.bcc ?? [],
    subject: a.subject ?? "", bodyText: a.bodyText ?? "", bodyHtml: a.bodyHtml ?? "",
    inReplyTo: a.inReplyTo ?? "", attachmentPaths: a.attachmentPaths ?? [], clientKey: a.clientKey ?? "",
  }); }, "M1"),
  save_draft: implemented((a: Partial<SaveDraftRequest>) => ComposeService.save({
    accountId: a.accountId ?? "", to: a.to ?? [], cc: a.cc ?? [], bcc: a.bcc ?? [],
    subject: a.subject ?? "", bodyText: a.bodyText ?? "", bodyHtml: a.bodyHtml ?? "",
    inReplyTo: a.inReplyTo ?? "", existingDraftId: a.existingDraftId ?? "",
    attachmentPaths: a.attachmentPaths ?? [], clientKey: a.clientKey ?? "",
    editorMode: a.editorMode ?? "rich", rawSource: a.rawSource ?? "", quotedReplyHtml: a.quotedReplyHtml ?? "",
  }), "M1"),
  delete_draft: implemented((a: { accountId: string; draftId: string }) => ComposeService.delete(a), "M1"),
  stage_compose_attachment: implemented((a: { filename: string; bytes: number[] }) => ComposeService.stage(a), "M1"),
  cleanup_staged_compose_attachment: implemented((a: { path: string }) => ComposeService.cleanup(a), "M1"),
  list_attachments: implemented((a: MessageRequest) => MessageService.attachments(a)),
  get_attachment_path: implemented((a: { attachmentId: string }) => AttachmentService.path(a), "M1"),
  download_attachment: implemented((a: { attachmentId: string; saveTo: string }) => AttachmentService.download(a), "M1"),
  search_messages: implemented((a: Partial<SearchRequest>) => SearchService.search({ query: a.query ?? "", accountId: a.accountId ?? "", limit: a.limit ?? 50 })),
  get_folder_unread_counts: implemented((a: AccountRequest) => MessageService.unreadCounts({ accountId: a.accountId, byFolder: true })),
  get_account_unread_counts: implemented(() => MessageService.unreadCounts({ accountId: "", byFolder: false })),
  get_profile_storage_namespace: implemented(async () => (await AppService.profile({})).value),
  quit_app: implemented(() => AppService.quit({})),
  health_check: implemented(async () => "Pebble Go"),
  start_sync: implemented(async (a: AccountRequest & { pollIntervalSecs?: number | null }) => (await SyncService.start({ accountId: a.accountId, pollIntervalSecs: a.pollIntervalSecs ?? null })).value, "M1"),
  stop_sync: implemented(async (a: AccountRequest) => { await SyncService.stop(a); }, "M1"),
  set_realtime_preference: implemented(async (a: { mode: "realtime" | "balanced" | "battery" | "manual" }) => { await SyncService.preference(a); }, "M1"),
  trigger_sync: implemented(async (a: AccountRequest & { reason?: string }) => {
    await SyncService.trigger({ accountId: a.accountId, reason: a.reason ?? "manual" });
  }, "M1"),
};
