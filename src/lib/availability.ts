import i18n from "i18next";
import { commands } from "@/lib/platform/manifest";

/** Product availability, not account authorization. Callers must also check account capabilities. */
export type Feature =
  | "contacts" | "kanban" | "snooze" | "localLabels" | "templates"
  | "ai" | "translation" | "advancedSearch" | "proxy" | "rules" | "backup"
  | "notifications" | "autostart" | "startHidden" | "accountReorder"
  | "imap" | "pop3" | "outlook" | "permanentDelete" | "backgroundImage"
  | "gmailLabels" | "gmailReconnect" | "localCompose" | "cloudDraftCopies"
  | "outbox" | "signatures";

/** Startup defaults are compatibility shims, never evidence of a working feature. */
export function isCommandAvailable(name: string): boolean {
  if (!Object.prototype.hasOwnProperty.call(commands, name)) return false;
  return commands[name as keyof typeof commands].status === "implemented";
}

const requirements: Partial<Record<Feature, readonly string[]>> = {
  contacts: ["list_contacts", "save_contact", "delete_contact"],
  kanban: ["list_kanban_cards", "move_to_kanban", "remove_from_kanban"],
  snooze: ["list_snoozed", "snooze_message", "unsnooze_message"],
  localLabels: ["list_labels", "add_message_label", "remove_message_label"],
  templates: ["list_email_templates", "save_email_template", "delete_email_template"],
  ai: ["ai_get_config", "ai_save_config", "ai_summarize_message"],
  translation: ["get_translate_config", "save_translate_config", "translate_text"],
  advancedSearch: ["advanced_search"],
  proxy: ["get_global_proxy", "update_global_proxy"],
  rules: ["list_rules", "create_rule", "update_rule", "delete_rule"],
  backup: ["export_backup_file", "import_backup_file", "backup_to_webdav"],
  notifications: ["get_notification_status", "set_notifications_enabled", "show_test_notification"],
  autostart: ["get_autostart_enabled", "set_autostart_enabled"],
  accountReorder: ["reorder_accounts"],
  imap: ["add_account", "test_imap_connection"],
  pop3: ["add_account", "test_pop3_connection"],
  backgroundImage: ["import_background_image", "delete_background_image"],
  localCompose: ["save_draft", "delete_draft", "stage_compose_attachment", "cleanup_staged_compose_attachment"],
  outbox: ["send_email"],
  signatures: ["get_email_signature", "set_email_signature", "migrate_email_signature_if_absent"],
};

export function isFeatureAvailable(feature: Feature): boolean {
  switch (feature) {
    // These production services use generated bindings, not legacy invoke names:
    // GmailLabelService.{list,change}, AccountService.{reconnect,cancelReconnect}, ComposeService.cloud.
    case "gmailLabels":
    case "gmailReconnect":
    case "cloudDraftCopies":
      return true;
    // Gmail modify access deliberately cannot permanently delete messages.
    // Outlook OAuth and startup-hidden/tray lifecycle are not implemented.
    case "permanentDelete":
    case "outlook":
    case "startHidden":
      return false;
    default: {
      const required = requirements[feature];
      return !!required?.length && required.every(isCommandAvailable);
    }
  }
}

export function unavailableReason(): string {
  const fallback = "This feature is not available in Pebble Go yet. No changes will be made.";
  return i18n.t("common.featureUnavailable", fallback) || fallback;
}

const viewFeatures: Record<string, Feature> = { contacts: "contacts", kanban: "kanban", snoozed: "snooze", compose: "localCompose" };
const views = new Set(["inbox", "kanban", "contacts", "settings", "search", "snoozed", "starred", "compose"]);
export function isViewAvailable(view: string): boolean {
  return views.has(view) && (!viewFeatures[view] || isFeatureAvailable(viewFeatures[view]));
}

const tabFeatures: Record<string, Feature> = { proxy: "proxy", rules: "rules", translation: "translation", ai: "ai", cloudSync: "backup" };
const tabs = new Set(["accounts", "general", "proxy", "appearance", "privacy", "rules", "remoteWrites", "translation", "ai", "shortcuts", "cloudSync", "about"]);
export function isSettingsTabAvailable(tab: string): boolean {
  return tabs.has(tab) && (!tabFeatures[tab] || isFeatureAvailable(tabFeatures[tab]));
}

const actionFeatures: Record<string, Feature> = {
  "toggle-view-kanban": "kanban", "open-cloud-settings": "backup", "toggle-notifications": "notifications",
  "translate-selection": "translation", "toggle-bilingual": "translation", "summarize-message": "ai",
  "nav:kanban": "kanban", "nav:contacts": "contacts", "nav:snoozed": "snooze", "nav:cloud-settings": "backup",
  "mail:add-to-kanban": "kanban", "settings:toggle-notifications": "notifications",
  "mail:compose": "localCompose", "compose-new": "localCompose", "reply": "localCompose", "reply-all": "localCompose", "forward": "localCompose",
};
/** Shared guard for palette execution and restored/custom keyboard bindings. */
export function isActionAvailable(action: string): boolean {
  return !actionFeatures[action] || isFeatureAvailable(actionFeatures[action]);
}
