import { extractErrorMessage } from "@/lib/extractErrorMessage";
import { profileLocalStorage } from "@/lib/profileStorage";
import { useUIStore } from "@/stores/ui.store";
import type { SummaryResult } from "@/mygo";

export const AI_LANGUAGE_OPTIONS = [
  { code: "zh", labelKey: "languages.chinese" },
  { code: "en", labelKey: "languages.english" },
  { code: "ja", labelKey: "languages.japanese" },
  { code: "ko", labelKey: "languages.korean" },
  { code: "fr", labelKey: "languages.french" },
  { code: "de", labelKey: "languages.german" },
  { code: "es", labelKey: "languages.spanish" },
] as const;

export interface AiPresetOption {
  id: string;
  labelKey: string;
  provider: "openai_compatible" | "anthropic";
  endpoint: string;
  defaultModel: string;
  apiMode: "completions" | "responses";
  local?: boolean;
}

export const AI_PRESETS: readonly AiPresetOption[] = [
  {
    id: "openai",
    labelKey: "ai.presetOpenAi",
    provider: "openai_compatible",
    endpoint: "https://api.openai.com/v1",
    defaultModel: "gpt-4o-mini",
    apiMode: "completions",
  },
  {
    id: "deepseek",
    labelKey: "ai.presetDeepSeek",
    provider: "openai_compatible",
    endpoint: "https://api.deepseek.com/v1",
    defaultModel: "deepseek-chat",
    apiMode: "completions",
  },
  {
    id: "anthropic",
    labelKey: "ai.presetAnthropic",
    provider: "anthropic",
    endpoint: "https://api.anthropic.com",
    defaultModel: "claude-sonnet-4-20250514",
    apiMode: "completions",
  },
  {
    id: "openrouter",
    labelKey: "ai.presetOpenRouter",
    provider: "openai_compatible",
    endpoint: "https://openrouter.ai/api/v1",
    defaultModel: "openai/gpt-4o-mini",
    apiMode: "completions",
  },
  {
    id: "ollama",
    labelKey: "ai.presetOllama",
    provider: "openai_compatible",
    endpoint: "http://127.0.0.1:11434/v1",
    defaultModel: "qwen2.5",
    apiMode: "completions",
    local: true,
  },
  {
    id: "custom",
    labelKey: "ai.presetCustom",
    provider: "openai_compatible",
    endpoint: "",
    defaultModel: "",
    apiMode: "completions",
  },
];

const LEGACY_PRIVACY_ACK_KEY = "gomail-ai-privacy-ack";
const PRIVACY_ACK_HOSTS_KEY = "gomail-ai-privacy-ack-hosts";

function readAckedHosts(): string[] {
  const raw = profileLocalStorage.getItem(PRIVACY_ACK_HOSTS_KEY);
  if (!raw) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === "string") : [];
  } catch {
    return [];
  }
}

export function isAiPrivacyAccepted(endpointHost?: string, isLocalEndpoint?: boolean): boolean {
  if (isLocalEndpoint) return true;
  const host = (endpointHost ?? "").trim().toLowerCase();
  if (host) {
    const hosts = readAckedHosts();
    if (hosts.includes(host)) return true;
  }
  return profileLocalStorage.getItem(LEGACY_PRIVACY_ACK_KEY) === "1";
}

export function acceptAiPrivacy(endpointHost?: string): void {
  profileLocalStorage.setItem(LEGACY_PRIVACY_ACK_KEY, "1");
  const host = (endpointHost ?? "").trim().toLowerCase();
  if (!host) return;
  const hosts = readAckedHosts();
  if (!hosts.includes(host)) {
    hosts.push(host);
    profileLocalStorage.setItem(PRIVACY_ACK_HOSTS_KEY, JSON.stringify(hosts.slice(-20)));
  }
}

let reqSeq = 0;
export function nextAiRequestId(prefix: string): string {
  reqSeq += 1;
  return `${prefix}-${Date.now()}-${reqSeq}`;
}

export type AiErrorKind = "unconfigured" | "disabled" | "keyring" | "cancelled" | "error";

export function classifyAiError(err: unknown): { kind: AiErrorKind; message: string } {
  const raw = extractErrorMessage(err);
  if (raw.includes("AI_NOT_CONFIGURED")) {
    return { kind: "unconfigured", message: raw.replace(/^.*AI_NOT_CONFIGURED:\s*/, "") };
  }
  if (raw.includes("AI_DISABLED")) {
    return { kind: "disabled", message: raw.replace(/^.*AI_DISABLED:\s*/, "") };
  }
  if (raw.includes("AI_KEYRING_LOCKED")) {
    return { kind: "keyring", message: raw.replace(/^.*AI_KEYRING_LOCKED:\s*/, "") };
  }
  if (raw.includes("AI_CANCELLED")) {
    return { kind: "cancelled", message: raw.replace(/^.*AI_CANCELLED:\s*/, "") };
  }
  return { kind: "error", message: raw };
}

export function openAiSettings(): void {
  const ui = useUIStore.getState();
  ui.setSettingsTab("ai");
  ui.setActiveView("settings");
}

export function resolveUiLanguageOption(uiLang?: string): string {
  const lang = uiLang || profileLocalStorage.getItem("gomail-language") || "zh";
  return AI_LANGUAGE_OPTIONS.some((opt) => opt.code === lang) ? lang : "en";
}

export function isLoopbackEndpoint(raw: string): boolean {
  try {
    const u = new URL(raw.trim());
    const h = u.hostname.toLowerCase();
    return h === "localhost" || h.endsWith(".localhost") || h === "127.0.0.1" || h === "::1" || h === "[::1]";
  } catch {
    return false;
  }
}

export function isRemoteHttpEndpoint(raw: string): boolean {
  try {
    const u = new URL(raw.trim());
    return u.protocol === "http:" && !isLoopbackEndpoint(raw);
  } catch {
    return false;
  }
}

export function formatSummaryForClipboard(summary: SummaryResult, actionsHeading: string): string {
  const lines: string[] = [];
  if (summary.tldr?.trim()) {
    lines.push(summary.tldr.trim());
  }
  if (summary.key_points?.length) {
    if (lines.length) lines.push("");
    for (const point of summary.key_points) {
      lines.push(`- ${point}`);
    }
  }
  if (summary.action_items?.length) {
    if (lines.length) lines.push("");
    lines.push(actionsHeading);
    for (const item of summary.action_items) {
      lines.push(item.due ? `- ${item.text} (${item.due})` : `- ${item.text}`);
    }
  }
  return lines.length > 0 ? lines.join("\n") : (summary.raw_text || "");
}

/** Deterministic synchronous content fingerprint for local translation cache keys. */
export function computeContentHash(parts: string[]): string {
  let h1 = 0x811c9dc5;
  let h2 = 0x9e3779b9;
  for (const part of parts) {
    for (let i = 0; i < part.length; i++) {
      const ch = part.charCodeAt(i);
      h1 ^= ch;
      h1 = Math.imul(h1, 0x01000193);
      h2 ^= ch + i;
      h2 = Math.imul(h2, 0x85ebca6b);
    }
    h1 ^= 0xff;
    h1 = Math.imul(h1, 0x01000193);
  }
  return `v1-${(h1 >>> 0).toString(16).padStart(8, "0")}${(h2 >>> 0).toString(16).padStart(8, "0")}`;
}
