import { ComposeService } from "@/mygo";
export type { LocalDraft, DraftSummary } from "@/mygo";

export const listLocalDrafts = (accountId: string) => ComposeService.list({ accountId });
export const getLocalDraft = (accountId: string, draftId: string) => ComposeService.get({ accountId, draftId });

export const MAX_COMPOSE_ATTACHMENT_BYTES = 10 * 1024 * 1024;
export const MAX_DRAFT_ATTACHMENT_BYTES = 25 * 1024 * 1024;
export const MAX_COMPOSE_ATTACHMENTS = 100;
