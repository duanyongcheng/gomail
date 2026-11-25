import { useCallback, useEffect, useRef, useState } from "react";
import { AIService, type AIConfigView, type TranslateItem } from "@/mygo";
import { sanitizeHtml } from "@/lib/sanitizeHtml";
import type { Message, RenderedHtml, TranslateResult } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";
import {
  BILINGUAL_TRANSLATION_CLASS,
  TRANSLATED_BLOCK_CLASS,
} from "@/lib/sourceEcho";
import {
  AI_LANGUAGE_OPTIONS,
  acceptAiPrivacy,
  classifyAiError,
  computeContentHash,
  isAiPrivacyAccepted,
  nextAiRequestId,
  resolveUiLanguageOption,
} from "@/lib/ai";

const MAX_ITEMS_PER_BATCH = 15;
const MAX_CHARS_PER_BATCH = 2000;
const BATCH_CONCURRENCY = 2;

const SKIP_TAGS = new Set([
  "STYLE", "SCRIPT", "TITLE", "NOSCRIPT", "TEXTAREA", "PRE", "CODE", "SVG", "MATH",
]);

const SEMANTIC_BLOCK_TAGS = new Set([
  "P", "LI", "H1", "H2", "H3", "H4", "H5", "H6",
  "BLOCKQUOTE", "FIGCAPTION", "DT", "DD", "SUMMARY", "CAPTION", "ADDRESS",
]);

const CONTAINER_BLOCK_TAGS = new Set([
  "DIV", "TD", "TH", "ARTICLE", "SECTION", "HEADER", "FOOTER", "MAIN", "ASIDE", "CENTER",
]);

const ALL_BLOCK_TAGS = new Set([...SEMANTIC_BLOCK_TAGS, ...CONTAINER_BLOCK_TAGS, "UL", "OL", "TABLE", "TR", "TBODY", "THEAD", "TFOOT", "DL"]);

export type TranslationDisplayMode = "bilingual" | "translated" | "original";

export type BilingualErrorCode = "failed" | "empty" | "unchanged" | "noText";

export interface BilingualError {
  code: BilingualErrorCode;
  detail?: string;
}

export interface BilingualWarning {
  code: "partial";
  done: number;
  total: number;
}

export interface TranslationProgress {
  done: number;
  total: number;
}

export interface AnnotatedTranslationResult extends TranslateResult {
  _isHtml?: boolean;
  translatedSubject?: string;
  model?: string;
  cached?: boolean;
}

class BilingualFailure extends Error {
  code: BilingualErrorCode;
  detail?: string;

  constructor(code: BilingualErrorCode, detail?: string) {
    super(code);
    this.name = "BilingualFailure";
    this.code = code;
    this.detail = detail;
  }
}

function isProseWorthTranslating(text: string): boolean {
  const trimmed = text.trim();
  if (trimmed.length < 2) return false;
  // Skip bare URLs or bare email addresses.
  if (/^https?:\/\/\S+$/i.test(trimmed)) return false;
  if (/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) return false;
  // Require at least 2 letter / CJK characters (avoid pure numbers, dates, punctuation).
  const letters = trimmed.match(/[\p{L}]/gu);
  return (letters?.length ?? 0) >= 2;
}

function normalizeBlockText(el: Element): string {
  return (el.textContent ?? "").replace(/\s+/g, " ").trim();
}

function hasSkipAncestor(el: Element): boolean {
  let cur: Element | null = el;
  while (cur) {
    if (SKIP_TAGS.has(cur.tagName)) return true;
    if (cur.classList.contains("blocked-image") || cur.classList.contains(BILINGUAL_TRANSLATION_CLASS)) return true;
    cur = cur.parentElement;
  }
  return false;
}

function hasTranslatableChildBlock(el: Element): boolean {
  for (const child of Array.from(el.children)) {
    if (SKIP_TAGS.has(child.tagName)) continue;
    if (ALL_BLOCK_TAGS.has(child.tagName)) {
      if (isProseWorthTranslating(normalizeBlockText(child))) return true;
    }
    if (hasTranslatableChildBlock(child)) return true;
  }
  return false;
}

/**
 * Collect leaf semantic prose blocks from a sanitized HTML document.
 *
 * Translating complete semantic blocks instead of isolated DOM Text nodes
 * preserves natural sentence grammar across inline `<strong>` / `<a>` tags,
 * keeps links clickable, and enables instant Bilingual / Translated-only toggles.
 */
function collectTranslatableBlocks(doc: Document): Element[] {
  const blocks: Element[] = [];
  const visited = new Set<Element>();

  function walk(node: Element) {
    if (SKIP_TAGS.has(node.tagName)) return;
    if (node.classList.contains("blocked-image") || node.classList.contains(BILINGUAL_TRANSLATION_CLASS)) return;

    const isSemantic = SEMANTIC_BLOCK_TAGS.has(node.tagName);
    const isContainer = CONTAINER_BLOCK_TAGS.has(node.tagName);
    const hasChildBlocks = hasTranslatableChildBlock(node);

    if ((isSemantic || isContainer) && !hasChildBlocks) {
      const text = normalizeBlockText(node);
      if (isProseWorthTranslating(text) && !visited.has(node)) {
        visited.add(node);
        blocks.push(node);
      }
      return;
    }

    for (const child of Array.from(node.children)) {
      walk(child);
    }
  }

  if (doc.body) {
    walk(doc.body);
    // Fallback: body with loose direct text nodes and no block wrappers.
    if (blocks.length === 0 && !hasSkipAncestor(doc.body)) {
      const bodyText = normalizeBlockText(doc.body);
      if (isProseWorthTranslating(bodyText)) {
        blocks.push(doc.body);
      }
    }
  }
  return blocks;
}

function buildBatches(items: TranslateItem[]): TranslateItem[][] {
  const batches: TranslateItem[][] = [];
  let current: TranslateItem[] = [];
  let charCount = 0;

  for (const item of items) {
    const len = item.text.length;
    if (current.length > 0 && (current.length >= MAX_ITEMS_PER_BATCH || charCount + len > MAX_CHARS_PER_BATCH)) {
      batches.push(current);
      current = [];
      charCount = 0;
    }
    current.push(item);
    charCount += len;
  }
  if (current.length > 0) batches.push(current);
  return batches;
}

function renderTranslatedHtmlSnapshot(
  cleanHtml: string,
  translations: Map<string, string>,
): { html: string; assigned: number; changed: number; total: number } {
  const doc = new DOMParser().parseFromString(cleanHtml, "text/html");
  const blocks = collectTranslatableBlocks(doc);
  let assigned = 0;
  let changed = 0;

  blocks.forEach((block, index) => {
    const id = String(index);
    const translated = translations.get(id)?.trim();
    if (!translated) return;
    assigned += 1;
    const original = normalizeBlockText(block);
    if (translated === original) return;
    changed += 1;
    block.classList.add(TRANSLATED_BLOCK_CLASS);
    const span = doc.createElement("span");
    span.setAttribute("class", BILINGUAL_TRANSLATION_CLASS);
    span.textContent = translated;
    block.appendChild(span);
  });

  return {
    html: sanitizeHtml(doc.body.innerHTML),
    assigned,
    changed,
    total: blocks.length,
  };
}

function splitPlainParagraphs(text: string): string[] {
  return text
    .replace(/\r\n/g, "\n")
    .split(/\n{2,}/)
    .map((p) => p.trim())
    .filter((p) => p.length > 0);
}

/**
 * Immersive block-level AI email translation hook:
 * - Translates both subject line and semantic HTML / plain-text blocks
 * - Supports instant Bilingual / Translated-only / Original view switching
 * - Persists results in GoMail's local SQLite AI cache for instant offline revisit
 * - Supports mid-flight cancellation when stopping, switching language, or changing messages
 */
export function useBilingualTranslation(
  messageId: string | null,
  rendered: RenderedHtml | null,
  message: Message | null,
) {
  const interfaceLanguage = useUIStore((state) => state.language);
  const [bilingualMode, setBilingualMode] = useState(false);
  const [displayMode, setDisplayMode] = useState<TranslationDisplayMode>("bilingual");
  const [targetLang, setTargetLangState] = useState(() => resolveUiLanguageOption(interfaceLanguage));
  const [bilingualResult, setBilingualResult] = useState<AnnotatedTranslationResult | null>(null);
  const [bilingualLoading, setBilingualLoading] = useState(false);
  const [progress, setProgress] = useState<TranslationProgress | null>(null);
  const [bilingualError, setBilingualError] = useState<BilingualError | null>(null);
  const [bilingualWarning, setBilingualWarning] = useState<BilingualWarning | null>(null);
  const [configIssue, setConfigIssue] = useState<"unconfigured" | "disabled" | "keyring" | null>(null);
  const [needsPrivacyAck, setNeedsPrivacyAck] = useState(false);
  const [endpointHost, setEndpointHost] = useState("");
  const [isLocalEndpoint, setIsLocalEndpoint] = useState(false);

  const generationRef = useRef(0);
  const activeRequestIdsRef = useRef<Set<string>>(new Set());
  const pendingOptionsRef = useRef<{ lang: string; forceRefresh: boolean } | null>(null);

  const cancelInFlight = useCallback(() => {
    for (const reqId of activeRequestIdsRef.current) {
      void AIService.cancel({ requestId: reqId }).catch(() => {});
    }
    activeRequestIdsRef.current.clear();
  }, []);

  useEffect(() => () => { cancelInFlight(); }, [cancelInFlight]);

  const executeTranslation = useCallback(
    async (
      chosenLang: string,
      forceRefresh: boolean,
      cfg: AIConfigView | null,
    ) => {
      if (!message || !messageId) return;

      cancelInFlight();
      const gen = ++generationRef.current;
      setBilingualMode(true);
      setBilingualLoading(true);
      setBilingualError(null);
      setBilingualWarning(null);
      setConfigIssue(null);
      setNeedsPrivacyAck(false);
      setProgress(null);
      if (forceRefresh) {
        setBilingualResult(null);
      }

      const isHtml = Boolean(rendered && rendered.html);
      const cleanHtml = isHtml ? sanitizeHtml(rendered!.html) : "";
      const subject = message.subject?.trim() || "";
      const items: TranslateItem[] = [];
      let plainParagraphs: string[] = [];

      if (subject && isProseWorthTranslating(subject)) {
        items.push({ id: "subject", text: subject });
      }

      if (isHtml) {
        const doc = new DOMParser().parseFromString(cleanHtml, "text/html");
        const blocks = collectTranslatableBlocks(doc);
        blocks.forEach((block, idx) => {
          items.push({ id: String(idx), text: normalizeBlockText(block) });
        });
      } else {
        const rawText =
          message.body_text ||
          new DOMParser().parseFromString(message.body_html_raw || "", "text/html").body.textContent ||
          "";
        plainParagraphs = splitPlainParagraphs(rawText);
        plainParagraphs.forEach((para, idx) => {
          if (isProseWorthTranslating(para)) {
            items.push({ id: String(idx), text: para });
          }
        });
      }

      if (items.length === 0) {
        setBilingualLoading(false);
        setBilingualError({ code: "noText" });
        return;
      }

      const contentHash = computeContentHash(items.map((it) => `${it.id}:${it.text}`));

      // 1. Check local SQLite cache unless explicitly re-translating.
      if (!forceRefresh) {
        try {
          const cached = await AIService.getTranslationCache({
            messageId,
            targetLang: chosenLang,
            contentHash,
          });
          if (gen !== generationRef.current) return;
          if (cached?.found && cached.segments && Object.keys(cached.segments).length > 0) {
            const map = new Map<string, string>(Object.entries(cached.segments));
            if (isHtml) {
              const snap = renderTranslatedHtmlSnapshot(cleanHtml, map);
              setBilingualResult({
                translated: snap.html,
                segments: [],
                _isHtml: true,
                translatedSubject: cached.subject || undefined,
                model: cached.model || undefined,
                cached: true,
              });
            } else {
              const segments = plainParagraphs.map((src, idx) => ({
                source: src,
                target: map.get(String(idx)) || src,
              }));
              setBilingualResult({
                translated: segments.map((s) => s.target).join("\n\n"),
                segments,
                _isHtml: false,
                translatedSubject: cached.subject || undefined,
                model: cached.model || undefined,
                cached: true,
              });
            }
            setBilingualLoading(false);
            return;
          }
        } catch {
          // Cache miss or unavailable; continue to live translation.
        }
      }

      // 2. Verify AI config and privacy consent before sending content.
      let activeCfg = cfg;
      if (!activeCfg) {
        try {
          activeCfg = await AIService.getConfig({});
        } catch {
          activeCfg = null;
        }
      }
      if (gen !== generationRef.current) return;

      if (activeCfg) {
        setEndpointHost(activeCfg.endpointHost || "");
        setIsLocalEndpoint(Boolean(activeCfg.isLocalEndpoint));
        if (activeCfg.keyringError) {
          setConfigIssue("keyring");
          setBilingualLoading(false);
          return;
        }
        if (!activeCfg.configured) {
          setConfigIssue("unconfigured");
          setBilingualLoading(false);
          return;
        }
        if (!activeCfg.enabled) {
          setConfigIssue("disabled");
          setBilingualLoading(false);
          return;
        }
        if (!isAiPrivacyAccepted(activeCfg.endpointHost, activeCfg.isLocalEndpoint)) {
          pendingOptionsRef.current = { lang: chosenLang, forceRefresh };
          setNeedsPrivacyAck(true);
          setBilingualLoading(false);
          return;
        }
      }

      // 3. Batch translate progressively with concurrency limit & cancellation.
      const batches = buildBatches(items);
      const translatedMap = new Map<string, string>();
      let completedCount = 0;
      let modelUsed = "";
      let firstError: unknown = null;
      const totalItems = items.length;
      setProgress({ done: 0, total: totalItems });

      const emitProgressive = (cached = false) => {
        const translatedSubject = translatedMap.get("subject");
        if (isHtml) {
          const snap = renderTranslatedHtmlSnapshot(cleanHtml, translatedMap);
          setBilingualResult({
            translated: snap.html,
            segments: [],
            _isHtml: true,
            translatedSubject: translatedSubject && translatedSubject !== subject ? translatedSubject : undefined,
            model: modelUsed || undefined,
            cached,
          });
        } else {
          const segments = plainParagraphs.map((src, idx) => ({
            source: src,
            target: translatedMap.get(String(idx)) || src,
          }));
          setBilingualResult({
            translated: segments.map((s) => s.target).join("\n\n"),
            segments,
            _isHtml: false,
            translatedSubject: translatedSubject && translatedSubject !== subject ? translatedSubject : undefined,
            model: modelUsed || undefined,
            cached,
          });
        }
      };

      let nextBatchIndex = 0;
      const runWorker = async () => {
        while (nextBatchIndex < batches.length) {
          if (gen !== generationRef.current) return;
          const idx = nextBatchIndex++;
          const batch = batches[idx];
          const requestId = nextAiRequestId(`tr-${idx}`);
          activeRequestIdsRef.current.add(requestId);
          try {
            const res = await AIService.translateBatch({
              requestId,
              items: batch,
              fromLang: "auto",
              toLang: chosenLang,
            });
            if (gen !== generationRef.current) return;
            if (res.model) modelUsed = res.model;
            for (const item of res.items ?? []) {
              if (item.text?.trim()) {
                translatedMap.set(item.id, item.text.trim());
              }
            }
            completedCount += batch.length;
            setProgress({ done: Math.min(completedCount, totalItems), total: totalItems });
            if (translatedMap.size > 0) {
              emitProgressive(false);
            }
          } catch (err) {
            if (gen !== generationRef.current) return;
            const classified = classifyAiError(err);
            if (classified.kind === "cancelled") {
              return;
            }
            if (classified.kind === "unconfigured" || classified.kind === "disabled" || classified.kind === "keyring") {
              setConfigIssue(classified.kind);
              firstError = err;
              return;
            }
            if (!firstError) firstError = err;
          } finally {
            activeRequestIdsRef.current.delete(requestId);
          }
        }
      };

      const workers = Array.from({ length: Math.min(BATCH_CONCURRENCY, batches.length) }, () => runWorker());
      await Promise.all(workers);

      if (gen !== generationRef.current) return;
      setBilingualLoading(false);
      setProgress(null);

      if (configIssue) return;

      if (translatedMap.size === 0) {
        if (firstError) {
          const classified = classifyAiError(firstError);
          if (classified.kind === "unconfigured" || classified.kind === "disabled" || classified.kind === "keyring") {
            setConfigIssue(classified.kind);
          } else if (classified.kind !== "cancelled") {
            setBilingualError({ code: "failed", detail: classified.message });
          }
        } else {
          setBilingualError({ code: "empty" });
        }
        return;
      }

      // Check whether anything actually changed vs original.
      let changedCount = 0;
      for (const item of items) {
        const tr = translatedMap.get(item.id);
        if (tr && tr !== item.text) changedCount += 1;
      }
      if (changedCount === 0) {
        setBilingualResult(null);
        setBilingualError({ code: "unchanged" });
        return;
      }

      emitProgressive(false);

      if (translatedMap.size < totalItems) {
        setBilingualWarning({ code: "partial", done: translatedMap.size, total: totalItems });
        return;
      }

      // Save full translation to local SQLite cache for offline reuse.
      const segmentsRecord: Record<string, string> = {};
      for (const [k, v] of translatedMap.entries()) {
        if (k !== "subject") segmentsRecord[k] = v;
      }
      void AIService.saveTranslationCache({
        messageId,
        accountId: message.account_id || "",
        targetLang: chosenLang,
        contentHash,
        model: modelUsed,
        subject: translatedMap.get("subject") || "",
        segments: segmentsRecord,
      }).catch(() => {});
    },
    [cancelInFlight, configIssue, message, messageId, rendered],
  );

  const handleBilingualToggle = useCallback(async () => {
    if (bilingualMode) {
      cancelInFlight();
      generationRef.current += 1;
      setBilingualMode(false);
      setBilingualLoading(false);
      setProgress(null);
      return;
    }
    if (!message || !messageId) return;

    let cfg: AIConfigView | null = null;
    try {
      cfg = await AIService.getConfig({});
    } catch {
      cfg = null;
    }

    const defaultLang =
      cfg?.translateLanguage && AI_LANGUAGE_OPTIONS.some((o) => o.code === cfg!.translateLanguage)
        ? cfg.translateLanguage
        : resolveUiLanguageOption(interfaceLanguage);
    const defaultMode: TranslationDisplayMode =
      cfg?.translateDisplayMode === "translated" ? "translated" : "bilingual";

    setTargetLangState(defaultLang);
    setDisplayMode(defaultMode);
    await executeTranslation(defaultLang, false, cfg);
  }, [bilingualMode, cancelInFlight, executeTranslation, interfaceLanguage, message, messageId]);

  const setTargetLang = useCallback(
    (nextLang: string) => {
      setTargetLangState(nextLang);
      if (bilingualMode) {
        void executeTranslation(nextLang, false, null);
      }
    },
    [bilingualMode, executeTranslation],
  );

  const handleRetranslate = useCallback(() => {
    void executeTranslation(targetLang, true, null);
  }, [executeTranslation, targetLang]);

  const handleStopTranslation = useCallback(() => {
    cancelInFlight();
    generationRef.current += 1;
    setBilingualLoading(false);
    setProgress(null);
  }, [cancelInFlight]);

  const acceptPrivacyAndTranslate = useCallback(() => {
    acceptAiPrivacy(endpointHost);
    setNeedsPrivacyAck(false);
    const pending = pendingOptionsRef.current ?? { lang: targetLang, forceRefresh: false };
    pendingOptionsRef.current = null;
    void executeTranslation(pending.lang, pending.forceRefresh, null);
  }, [endpointHost, executeTranslation, targetLang]);

  const resetBilingual = useCallback(() => {
    cancelInFlight();
    generationRef.current += 1;
    setBilingualMode(false);
    setBilingualResult(null);
    setBilingualLoading(false);
    setProgress(null);
    setBilingualError(null);
    setBilingualWarning(null);
    setConfigIssue(null);
    setNeedsPrivacyAck(false);
  }, [cancelInFlight]);

  return {
    bilingualMode,
    displayMode,
    setDisplayMode,
    targetLang,
    setTargetLang,
    bilingualResult,
    bilingualLoading,
    progress,
    bilingualError,
    bilingualWarning,
    configIssue,
    needsPrivacyAck,
    endpointHost,
    isLocalEndpoint,
    acceptPrivacyAndTranslate,
    handleBilingualToggle,
    handleRetranslate,
    handleStopTranslation,
    resetBilingual,
  };
}

export { BilingualFailure };
