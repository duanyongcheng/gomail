/**
 * IPC type contracts — TypeScript mirrors for backend structs that cross the
 * GoMail desktop IPC boundary (Go services in `internal/`).
 *
 * When updating a type here, check the Go backend definition first; when
 * updating a backend struct, update its mirror here.
 */

// ─── Core domain types ─────────────────────────────────────────────────────────

export interface Account {
  capabilities?: { send: boolean; read: boolean; modify_flags: boolean; html: boolean; folder_management?: boolean };
  account_label?: string | null;
  provider_display_name?: string | null;
  id: string;
  email: string;
  display_name: string;
  color?: string | null;
  /** ProviderType enum (lowercase wire format) */
  provider: "imap" | "pop3" | "gmail" | "outlook";
  created_at: number;
  updated_at: number;
}

export interface Folder {
  id: string;
  account_id: string;
  remote_id: string;
  name: string;
  folder_type: "folder" | "label" | "category";
  role: "inbox" | "sent" | "drafts" | "trash" | "archive" | "spam" | null;
  parent_id: string | null;
  color: string | null;
  is_system: boolean;
  sort_order: number;
}

export interface ImapSyncFolderSettings {
  folders: Folder[];
  selected_remote_ids: string[];
}

export interface EmailAddress {
  name: string | null;
  address: string;
}

/**
 * Lightweight message data for list views (no body fields).
 */
export interface MessageSummary {
  id: string;
  account_id: string;
  remote_id: string;
  message_id_header: string | null;
  in_reply_to: string | null;
  references_header: string | null;
  thread_id: string | null;
  subject: string;
  snippet: string;
  from_address: string;
  from_name: string;
  to_list: EmailAddress[];
  cc_list: EmailAddress[];
  bcc_list: EmailAddress[];
  has_attachments: boolean;
  is_read: boolean;
  is_starred: boolean;
  is_draft: boolean;
  /** GoMail: derived cached Trash membership, independent of the current view. */
  in_trash?: boolean;
  date: number;
  remote_version: string | null;
  is_deleted: boolean;
  deleted_at: number | null;
  created_at: number;
  updated_at: number;
}

/**
 * Full message including body content.
 */
export interface Message extends MessageSummary {
  /** Effective capability of this message's account in GoMail. */
  capabilities?: { modify_flags: boolean };
  body_text: string;
  body_html_raw: string;
}

export interface PendingMailOpsSummary {
  pending_count: number;
  in_progress_count: number;
  failed_count: number;
  total_active_count: number;
  last_error: string | null;
  updated_at: number | null;
}

export type PendingMailOpStatus = "pending" | "in_progress" | "failed";

export interface PendingMailOp {
  id: string;
  account_id: string;
  message_id: string;
  op_type: string;
  status: PendingMailOpStatus;
  attempts: number;
  last_error: string | null;
  created_at: number;
  updated_at: number;
  next_retry_at: number | null;
}

export interface AppLogSnapshot {
  path: string;
  content: string;
  truncated: boolean;
}

export interface ImportedBackgroundImage {
  path: string;
  filename: string;
  size: number;
}

export interface NotificationStatus {
  enabled: boolean;
  attention_active: boolean;
  platform: string;
  app_id: string | null;
}

export interface RenderedHtml {
  html: string;
  trackers_blocked: { domain: string; tracker_type: string }[];
  images_blocked: number;
}

export interface SearchHit {
  message_id: string;
  score: number;
  snippet: string;
  subject?: string;
  from_address?: string;
  date?: number;
}

/**
 * External tagging wire format: unit variants serialize as strings, payload
 * variants as `{ VariantName: value }`.
 */
export type PrivacyMode = "Strict" | { TrustedSender: string } | "LoadOnce" | "Off";

// ─── Mail config types ──────────────────────────────────────────────────────────

/** ConnectionSecurity (lowercase wire format) */
export type ConnectionSecurity = "tls" | "starttls" | "plain";

export interface AddAccountRequest {
  account_label?: string | null;
  email: string;
  display_name: string;
  provider: string;
  imap_host: string;
  imap_port: number;
  smtp_host: string;
  smtp_port: number;
  username: string;
  password: string;
  imap_security: ConnectionSecurity;
  smtp_security: ConnectionSecurity;
  accept_invalid_certs?: boolean;
  allow_plaintext?: boolean;
  proxy_host?: string;
  proxy_port?: number;
}

export interface HttpProxyConfig {
  host: string;
  port: number;
}

export type AccountProxyMode = "inherit" | "disabled" | "custom";

export interface AccountProxySetting {
  mode: AccountProxyMode;
  proxy: HttpProxyConfig | null;
}

// ─── Attachment types ───────────────────────────────────────────────────────────

export interface Attachment {
  id: string;
  message_id: string;
  filename: string;
  mime_type: string;
  size: number;
  local_path: string | null;
  content_id: string | null;
  is_inline: boolean;
}

// ─── Kanban types ───────────────────────────────────────────────────────────────

/** KanbanColumn (lowercase wire format) */
export type KanbanColumnType = "todo" | "waiting" | "done";

export interface KanbanCard {
  message_id: string;
  column: KanbanColumnType;
  position: number;
  created_at: number;
  updated_at: number;
}

// ─── Snooze types ───────────────────────────────────────────────────────────────

export interface SnoozedMessage {
  message_id: string;
  snoozed_at: number;
  unsnoozed_at: number;
  return_to: string;
}

// ─── Trusted Sender types ───────────────────────────────────────────────────────

export interface TrustedSender {
  account_id: string;
  email: string;
  /** TrustType enum (lowercase wire format) */
  trust_type: "images" | "all";
  created_at: number;
}

// ─── Rule types ─────────────────────────────────────────────────────────────────

export interface Rule {
  id: string;
  name: string;
  priority: number;
  conditions: string;
  actions: string;
  is_enabled: boolean;
  created_at: number;
  updated_at: number;
}

// ─── Search types ───────────────────────────────────────────────────────────────

/** AdvancedSearchQuery (camelCase wire format) */
export interface AdvancedSearchQuery {
  text?: string;
  from?: string;
  to?: string;
  subject?: string;
  dateFrom?: number;
  dateTo?: number;
  hasAttachment?: boolean;
  folderId?: string;
  /** Scope the search to a single mailbox; omit for the combined view. */
  accountId?: string;
}

// ─── Translate types ────────────────────────────────────────────────────────────

export interface TranslateConfig {
  id: string;
  provider_type: string;
  config: string;
  is_enabled: boolean;
  created_at: number;
  updated_at: number;
}

export interface TranslateResult {
  translated: string;
  segments: { source: string; target: string }[];
}

// ─── AI assistant types ─────────────────────────────────────────────────────────

export interface AiConfig {
  id: string;
  provider_type: string;
  config: string;
  is_enabled: boolean;
  created_at: number;
  updated_at: number;
}

export type AiProviderType = "openai_compatible" | "generic";

/** Shared wire format for LLM API mode */
export type AiApiMode = "completions" | "responses";

export type AiTone = "neutral" | "formal" | "friendly" | "concise";

export type AiLength = "short" | "medium" | "long";

export interface AiResult {
  text: string;
  /** `"ai"` when the AI service answered, `"translate"` when the compose
   *  translate action fell back to the translate engine. */
  engine: "ai" | "translate" | string;
}

// ─── Thread types ───────────────────────────────────────────────────────────────

export interface ThreadSummary {
  thread_id: string;
  /** Mailbox of the most recent message in the thread. */
  account_id: string;
  subject: string;
  snippet: string;
  last_date: number;
  message_count: number;
  unread_count: number;
  is_starred: boolean;
  participants: string[];
  has_attachments: boolean;
}

// ─── Label types ────────────────────────────────────────────────────────────────

export interface Label {
  id: string;
  name: string;
  color: string;
  is_system: boolean;
  rule_id: string | null;
}

// ─── Cloud Sync types ───────────────────────────────────────────────────────────

export interface BackupPreview {
  version: number;
  exported_at: number;
  account_count: number;
  rule_count: number;
  kanban_card_count: number;
  kanban_note_count: number;
  contact_count: number;
  has_translate_config: boolean;
  has_encrypted_secrets: boolean;
  secret_account_count: number;
  has_translate_secret: boolean;
  size_bytes: number;
}

// ─── Contacts types ─────────────────────────────────────────────────────────────

export type ContactEmailLabel = "work" | "personal" | "other";

export interface ContactEmail {
  id: string;
  address: string;
  label: ContactEmailLabel;
  is_primary: boolean;
}

export interface Contact {
  id: string;
  display_name: string;
  notes: string;
  is_favorite: boolean;
  emails: ContactEmail[];
  created_at: number;
  updated_at: number;
}

export interface ContactEmailInput {
  id?: string | null;
  address: string;
  label: ContactEmailLabel;
  is_primary: boolean;
}

export interface ContactInput {
  id?: string | null;
  display_name: string;
  notes: string;
  is_favorite: boolean;
  emails: ContactEmailInput[];
}

export type ContactSuggestionSource = "saved" | "recent";

export interface ContactSuggestion {
  contact_id: string | null;
  name: string | null;
  address: string;
  source: ContactSuggestionSource;
  is_favorite: boolean;
  last_interaction_at: number | null;
}

export interface VcardImportResult {
  created: number;
  merged: number;
  skipped: number;
  invalid: number;
  errors: string[];
}

export interface KnownContact {
  name: string | null;
  address: string;
}
