import { AccountService, AppService, FolderService, MessageService, SearchService, SyncService } from "@/mygo";
import type { AccountRequest, FlagsRequest, LoginRequest, MessageRequest, MessagesRequest, SearchRequest } from "@/mygo";

type Entry<A, R> = { status: "implemented" | "startup-stub" | "not-implemented"; milestone: string; run: (args: A) => Promise<R> };
function implemented<A, R>(run: (args: A) => Promise<R>): Entry<A, R> { return { status: "implemented", milestone: "M0", run }; }
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
  list_attachments: implemented((a: MessageRequest) => MessageService.attachments(a)),
  search_messages: implemented((a: Partial<SearchRequest>) => SearchService.search({ query: a.query ?? "", accountId: a.accountId ?? "", limit: a.limit ?? 50 })),
  get_folder_unread_counts: implemented((a: AccountRequest) => MessageService.unreadCounts({ accountId: a.accountId, byFolder: true })),
  get_account_unread_counts: implemented(() => MessageService.unreadCounts({ accountId: "", byFolder: false })),
  get_profile_storage_namespace: implemented(async () => (await AppService.profile({})).value),
  quit_app: implemented(() => AppService.quit({})),
  health_check: implemented(async () => "Pebble Go"),
  trigger_sync: implemented(async (a: AccountRequest & { reason?: string }) => {
    // Automatic polling/focus/reconnect behavior arrives with the M1 scheduler.
    // M0 issues network requests only for an explicit manual sync.
    if (a.reason === "manual") await SyncService.sync(a);
  }),
};
