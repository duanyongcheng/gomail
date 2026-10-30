import { ComposeService } from "@/mygo";
export type { LocalDraft, DraftSummary, RemoteDraftSummary } from "@/mygo";

export const listLocalDrafts = (accountId: string) => ComposeService.list({ accountId });
export const getLocalDraft = (accountId: string, draftId: string) => ComposeService.get({ accountId, draftId });
export type CloudDraftAction = "upload" | "check" | "delete" | "keep-local" | "import-remote";
export const changeCloudDraft = (accountId: string, draftId: string, action: CloudDraftAction) => ComposeService.cloud({ accountId, draftId, action });
export const listGmailDrafts = (accountId: string, pageToken = "") => ComposeService.cloudList({ accountId, pageToken });
export const importGmailDraft = (accountId: string, remoteDraftId: string, expectedMessageId: string) =>
  ComposeService.cloudImport({ accountId, remoteDraftId, expectedMessageId });

export const MAX_COMPOSE_ATTACHMENT_BYTES = 10 * 1024 * 1024;
export const MAX_DRAFT_ATTACHMENT_BYTES = 25 * 1024 * 1024;
export const MAX_COMPOSE_ATTACHMENTS = 100;
