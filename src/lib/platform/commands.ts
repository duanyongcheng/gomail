import { AccountService, AppService, AttachmentService, ComposeService, FolderService, MessageService, OutboxService, PendingOpsService, SearchService, SyncService, ThreadService, TrustService } from "@/mygo";
import type { AccountRequest, FlagsRequest, LoginRequest, MessageRequest, MessagesRequest, SearchRequest, SaveDraftRequest, SendRequest, UpdateAccountRequest, SignatureRequest, AddAccountRequest, ProxySetting } from "@/mygo";

type Entry<A, R> = { status: "implemented" | "startup-stub" | "not-implemented"; milestone: string; run: (args: A) => Promise<R> };
function implemented<A, R>(run: (args: A) => Promise<R>, milestone = "M0"): Entry<A, R> { return { status: "implemented", milestone, run }; }
export function startup<R>(value: R, milestone: string): Entry<Record<string, unknown>, R> {
  return { status: "startup-stub", milestone, run: async () => structuredClone(value) };
}
export function pending(name: string, milestone: string, message?: string): Entry<Record<string, unknown>, never> {
  return { status: "not-implemented", milestone, run: async () => { throw new Error(message ?? `NotImplemented: ${name} (${milestone})`); } };
}

export const implementedCommands = {
  get_global_proxy: implemented(async () => (await AccountService.network({})).proxy, "M2"),
  update_global_proxy: implemented(async (a: LegacyProxyRequest) => { await AccountService.setNetwork({ proxy: legacyRoute(a, "disabled").proxy }); }, "M2"),
  get_account_proxy_setting: implemented((a: AccountRequest) => AccountService.accountNetwork(a), "M2"),
  get_oauth_account_proxy_setting: implemented((a: AccountRequest) => AccountService.accountNetwork(a), "M2"),
  get_account_proxy: implemented(effectiveProxy, "M2"),
  get_oauth_account_proxy: implemented(effectiveProxy, "M2"),
  update_account_proxy: implemented(saveLegacyRoute, "M2"),
  update_oauth_account_proxy: implemented(saveLegacyRoute, "M2"),
  update_account_proxy_setting: implemented(saveLegacyRoute, "M2"),
  update_oauth_account_proxy_setting: implemented(saveLegacyRoute, "M2"),
  get_imap_sync_folders: implemented((a: AccountRequest) => AccountService.syncFolders(a), "M2"),
  update_imap_sync_folders: implemented((a: { accountId: string; selectedRemoteIds: string[] }) => AccountService.setSyncFolders(a), "M2"),
  get_xoauth2_status: implemented((a: AccountRequest) => AccountService.xoAuth2Status(a), "M2"),
  add_account: implemented((a: { request: AddAccountRequest }) => AccountService.add(a.request), "M2"),
  test_account_connection: implemented(async (a: AccountRequest) => (await AccountService.testStored(a)).message, "M2"),
  test_imap_connection: implemented(async (a: { request: Partial<AddAccountRequest> }) => (await AccountService.test(protocolTestRequest(a.request, "imap"))).message, "M2"),
  test_pop3_connection: implemented(async (a: { request: Partial<AddAccountRequest> & { pop3_host?: string; pop3_port?: number; pop3_security?: string } }) => (await AccountService.test(protocolTestRequest({ ...a.request, imap_host: a.request.pop3_host, imap_port: a.request.pop3_port, imap_security: a.request.pop3_security }, "pop3"))).message, "M2"),
  list_accounts: implemented(() => AccountService.list({})),
  complete_oauth_flow: implemented((a: LoginRequest & LegacyProxyRequest) => {
    return AccountService.login({ provider: a.provider, email: a.email, displayName: a.displayName, accountLabel: a.accountLabel ?? null, ...(a.proxyHost ? { proxy: legacyRoute(a, "inherit") } : {}) });
  }),
  delete_account: implemented((a: AccountRequest) => AccountService.delete(a)),
  update_account: implemented(async (a: UpdateAccountRequest) => { await AccountService.update(a); }, "M1"),
  get_email_signature: implemented((a: AccountRequest) => AccountService.signature(a), "M1"),
  set_email_signature: implemented(async (a: SignatureRequest) => { await AccountService.setSignature(a); }, "M1"),
  migrate_email_signature_if_absent: implemented((a: SignatureRequest) => AccountService.migrateSignature(a), "M1"),
  list_folders: implemented((a: AccountRequest) => FolderService.list(a)),
  trust_sender: implemented(async (a: { accountId: string; email: string; trustType: string }) => { await TrustService.set(a); }, "M1"),
  remove_trusted_sender: implemented(async (a: { accountId: string; email: string }) => { await TrustService.remove(a); }, "M1"),
  list_trusted_senders: implemented((a: AccountRequest) => TrustService.list(a), "M1"),
  is_trusted_sender: implemented((a: { accountId: string; email: string }) => TrustService.isTrusted(a), "M1"),
  list_threads: implemented((a: { folderId?: string; folderIds?: string[]; limit?: number; offset?: number }) => ThreadService.list({ folderId: a.folderId ?? "", folderIds: a.folderIds ?? [], limit: a.limit ?? 50, offset: a.offset ?? 0 }), "M1"),
  list_thread_messages: implemented((a: { threadId: string }) => ThreadService.messages(a), "M1"),
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
    inReplyTo: a.inReplyTo ?? "", replyMessageId: a.replyMessageId, attachmentPaths: a.attachmentPaths ?? [], clientKey: a.clientKey ?? "",
    localDraftId: a.localDraftId,
  }); }, "M1"),
  save_draft: implemented((a: Partial<SaveDraftRequest>) => ComposeService.save({
    accountId: a.accountId ?? "", to: a.to ?? [], cc: a.cc ?? [], bcc: a.bcc ?? [],
    subject: a.subject ?? "", bodyText: a.bodyText ?? "", bodyHtml: a.bodyHtml ?? "",
    inReplyTo: a.inReplyTo ?? "", replyMessageId: a.replyMessageId, existingDraftId: a.existingDraftId ?? "",
    attachmentPaths: a.attachmentPaths ?? [], clientKey: a.clientKey ?? "",
    editorMode: a.editorMode ?? "rich", rawSource: a.rawSource ?? "", renderedSource: a.renderedSource, quotedReplyHtml: a.quotedReplyHtml ?? "",
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
  open_external_url: implemented(async (a: { url: string }) => { await AppService.openExternal(a); }, "M1"),
  health_check: implemented(async () => "GoMail"),
  start_sync: implemented(async (a: AccountRequest & { pollIntervalSecs?: number | null }) => (await SyncService.start({ accountId: a.accountId, pollIntervalSecs: a.pollIntervalSecs ?? null })).value, "M1"),
  stop_sync: implemented(async (a: AccountRequest) => { await SyncService.stop(a); }, "M1"),
  set_realtime_preference: implemented(async (a: { mode: "realtime" | "balanced" | "battery" | "manual" }) => { await SyncService.preference(a); }, "M1"),
  trigger_sync: implemented(async (a: AccountRequest & { reason?: string }) => {
    await SyncService.trigger({ accountId: a.accountId, reason: a.reason ?? "manual" });
  }, "M1"),
};

type LegacyProxyRequest = { mode?: string | null; proxyHost?: string | null; proxyPort?: number | null; proxyScheme?: string | null };
function legacyRoute(a: LegacyProxyRequest, fallback = "inherit"): ProxySetting {
  const mode = a.mode ?? (a.proxyHost ? "custom" : fallback);
  if (!["inherit", "disabled", "custom"].includes(mode)) throw new Error("Invalid proxy mode");
  return { mode, proxy: mode === "custom" ? { scheme: a.proxyScheme ?? "http", host: a.proxyHost ?? "", port: a.proxyPort ?? 0 } : null };
}
async function saveLegacyRoute(a: AccountRequest & LegacyProxyRequest): Promise<void> {
  await AccountService.setAccountNetwork({ accountId: a.accountId, setting: legacyRoute(a) });
}
async function effectiveProxy(a: AccountRequest) {
  const setting = await AccountService.accountNetwork(a);
  return setting.mode === "custom" ? setting.proxy : setting.mode === "disabled" ? null : (await AccountService.network({})).proxy;
}

function protocolTestRequest(request: Partial<AddAccountRequest>, provider: string): AddAccountRequest {
  return { email: request.email ?? request.username ?? "", display_name: "", provider, imap_host: "", imap_port: 0, smtp_host: "", smtp_port: 0, username: "", password: "", imap_security: "tls", smtp_security: "tls", accept_invalid_certs: false, allow_plaintext: false, ...request };
}
