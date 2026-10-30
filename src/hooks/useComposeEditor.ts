import { useState, useEffect, useRef, useMemo } from "react";
import { useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import { Markdown as MarkdownExtension } from "tiptap-markdown";
import { getSignature } from "@/lib/signatures";
import { ComposeSignature, replaceEditorSignature, replaceSourceSignature, signatureHtml as buildSignatureHtml } from "@/features/compose/signature";
import { sanitizeComposeQuoteHtml } from "@/lib/sanitizeHtml";
import { prepareComposeConversion } from "@/features/compose/mode-conversion";
import type { Message } from "@/lib/ipc-types";
import type { TFunction } from "i18next";

export type EditorMode = "rich" | "markdown" | "html";

interface UseComposeEditorArgs {
  fromAccountId: string;
  composeMode: string | null;
  composeReplyTo: Message | null;
  isReply: boolean;
  t: TFunction;
  restoredDraft: {
    editorMode?: EditorMode;
    rawSource?: string;
    richTextHtml?: string;
    quotedReplyHtml?: string;
  } | null;
  prefillBody?: string;
}

interface BuildComposeEditorContentArgs {
  composeMode: string | null;
  composeReplyTo: Message | null;
  isReply: boolean;
  signatureHtml: string;
  prefillBody?: string;
  t: TFunction;
}

interface BuildReplyQuoteHtmlArgs {
  composeReplyTo: Message | null;
  t: TFunction;
}

export function escapeHtml(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/**
 * Turn plain text (as produced by the AI assistant) into editor paragraphs.
 * Shared with the AI module so generated text enters the rich editor the same
 * way a prefilled body does.
 */
export function plainTextToParagraphs(text: string) {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim() ? `<p>${escapeHtml(line)}</p>` : "<p><br></p>")
    .join("");
}

function parseBodyHtml(html: string) {
  const doc = new DOMParser().parseFromString(html, "text/html");
  return doc.body.innerHTML;
}

function decodeHtmlEntities(value: string) {
  const doc = new DOMParser().parseFromString(value, "text/html");
  return doc.body.textContent ?? "";
}

function startsWithEncodedHtmlTag(value: string) {
  return /^\s*(?:&lt;|&#0*60;|&#x0*3c;)\s*\/?[a-z][\w:-]*(?:\s|\/?(?:&gt;|&#0*62;|&#x0*3e;))/i.test(value);
}

function looksLikeHtml(value: string) {
  return /<\s*\/?[a-z][\w:-]*(?:\s|\/?>)/i.test(value);
}

export function extractComposeBodyHtml(html: string) {
  try {
    const source = html.trim();
    let body = parseBodyHtml(source);
    const decoded = decodeHtmlEntities(body).trim();

    if (startsWithEncodedHtmlTag(source) && decoded !== body.trim() && looksLikeHtml(decoded)) {
      body = parseBodyHtml(decoded);
    }

    return sanitizeComposeQuoteHtml(body);
  } catch {
    return `<p>${escapeHtml(html)}</p>`;
  }
}

export function buildComposeEditorContent({
  composeMode,
  composeReplyTo,
  isReply,
  signatureHtml,
  prefillBody,
  t,
}: BuildComposeEditorContentArgs) {
  try {
    if (isReply && composeReplyTo) {
      return `<p><br></p>${signatureHtml}`;
    }
    if (composeMode === "forward" && composeReplyTo) {
      const sender = escapeHtml(composeReplyTo.from_name || composeReplyTo.from_address || "");
      const fwdSubject = escapeHtml(composeReplyTo.subject || "");
      const body = composeReplyTo.body_html_raw
        ? extractComposeBodyHtml(composeReplyTo.body_html_raw)
        : `<p>${escapeHtml(composeReplyTo.body_text || "")}</p>`;
      return `<p><br></p>${signatureHtml}<br/><br/><p>${escapeHtml(t("compose.forwardedHeader"))}</p><p>${escapeHtml(t("compose.forwardedFrom", { sender }))}</p><p>${escapeHtml(t("compose.forwardedSubject", { subject: fwdSubject }))}</p>${body}`;
    }
    if (composeMode === "new" && prefillBody?.trim()) {
      return `${plainTextToParagraphs(prefillBody)}${signatureHtml}`;
    }
    return signatureHtml ? `<p><br></p>${signatureHtml}` : "";
  } catch (err) {
    console.error("[ComposeView] Failed to build editor content:", err);
    return "";
  }
}

export function buildReplyQuoteHtml({ composeReplyTo, t }: BuildReplyQuoteHtmlArgs) {
  if (!composeReplyTo) return "";

  const sender = escapeHtml(composeReplyTo.from_name || composeReplyTo.from_address || "");
  const dateStr = escapeHtml(new Date((composeReplyTo.date || 0) * 1000).toLocaleString());
  const body = composeReplyTo.body_html_raw
    ? extractComposeBodyHtml(composeReplyTo.body_html_raw)
    : `<p>${escapeHtml(composeReplyTo.body_text || "")}</p>`;
  const attribution = t("compose.quoteAttribution", { date: dateStr, sender });

  return `<blockquote class="compose-original-quote"><p>${escapeHtml(attribution)}</p>${body}</blockquote>`;
}

export function appendReplyQuoteHtml(bodyHtml: string, quotedReplyHtml: string) {
  const quote = quotedReplyHtml.trim();
  if (!quote) return bodyHtml;
  const body = bodyHtml.trim() || "<p><br></p>";
  return `${body}<br/><br/>${quote}`;
}

export function useComposeEditor({
  fromAccountId,
  composeMode,
  composeReplyTo,
  isReply,
  t,
  restoredDraft,
  prefillBody,
}: UseComposeEditorArgs) {
  const [editorMode, setEditorMode] = useState<EditorMode>(restoredDraft?.editorMode ?? "rich");
  const [rawSource, setRawSource] = useState(restoredDraft?.rawSource ?? prefillBody ?? "");
  const [conversionError, setConversionError] = useState(false);
  useEffect(() => { setConversionError(false); }, [rawSource]);
  const [richTextHtml, setRichTextHtml] = useState("");
  // Only a fresh, unprefilled editor can have replaceable automatic content.
  // Compare the whole active representation, not just the signature marker:
  // users can edit inside that node, or add non-text content such as a divider.
  const [pristineBody, setPristineBody] = useState<{ html: string; source: string } | null>(null);
  const [htmlPreview, setHtmlPreview] = useState(false);
  const [signatureLoad, setSignatureLoad] = useState<{ account: string; attempt: number; value: string; failed: boolean } | null>(null);
  const [signatureAttempt, setSignatureAttempt] = useState(0);
  const [appliedAccount, setAppliedAccount] = useState<string | null>(null);
  const restoredAccountRef = useRef(fromAccountId);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const contentInitializedRef = useRef(false);
  const preserveRestored = Boolean(restoredDraft) && fromAccountId === restoredAccountRef.current
    && (appliedAccount === null || appliedAccount === fromAccountId);
  const currentLoad = signatureLoad?.account === fromAccountId && signatureLoad.attempt === signatureAttempt ? signatureLoad : null;
  const signatureReady = preserveRestored || Boolean(currentLoad && !currentLoad.failed);
  const signatureError = !preserveRestored && Boolean(currentLoad?.failed);
  const signature = currentLoad?.value ?? "";
  // Account identity is checked during render, not just in an effect: no render
  // may send/save the previous account's signature while a new read is pending.
  const editorReady = appliedAccount === fromAccountId && signatureReady;

  useEffect(() => {
    if (preserveRestored || !fromAccountId) return;
    let cancelled = false;
    getSignature(fromAccountId)
      .then((value) => {
        if (!cancelled) setSignatureLoad({ account: fromAccountId, attempt: signatureAttempt, value, failed: false });
      })
      .catch(() => {
        if (!cancelled) setSignatureLoad({ account: fromAccountId, attempt: signatureAttempt, value: "", failed: true });
      });
    return () => { cancelled = true; };
  }, [fromAccountId, preserveRestored, signatureAttempt]);

  const signatureHtml = useMemo(() => buildSignatureHtml(signature), [signature]);

  const editorContent = useMemo(() => {
    return buildComposeEditorContent({ composeMode, composeReplyTo, isReply, signatureHtml, prefillBody, t });
  }, [composeMode, composeReplyTo, isReply, t, signatureHtml, prefillBody]);
  const quotedReplyHtml = useMemo(() => {
    return restoredDraft?.quotedReplyHtml !== undefined
      ? sanitizeComposeQuoteHtml(restoredDraft.quotedReplyHtml)
      : (isReply ? buildReplyQuoteHtml({ composeReplyTo, t }) : "");
  }, [composeReplyTo, isReply, t, restoredDraft]);

  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit,
      ComposeSignature,
      Placeholder.configure({ placeholder: t("compose.editorPlaceholder", "Write your message...") }),
      MarkdownExtension.configure({ html: true, transformPastedText: true }),
    ],
    content: "",
    editable: editorReady,
  });

  const bodyIsPristine = Boolean(pristineBody && (editorMode === "rich"
    ? editor?.getHTML() === pristineBody.html
    : rawSource === pristineBody.source));

  useEffect(() => {
    editor?.setEditable(editorReady);
  }, [editor, editorReady]);

  // Only the initial, non-editable editor receives a whole-document snapshot.
  // Subsequent From changes replace the explicitly marked signature only.
  useEffect(() => {
    if (!editor || !signatureReady || appliedAccount === fromAccountId) return;
    const keepPristine = contentInitializedRef.current
      ? bodyIsPristine
      : composeMode === "new" && !restoredDraft && !prefillBody && !composeReplyTo;
    let nextSource = rawSource;
    if (!contentInitializedRef.current) {
      if (restoredDraft && editorMode !== "rich") {
        // Source is authoritative, including intentionally empty HTML. Populate
        // the inactive rich editor only when the schema can preserve it safely.
        const restored = prepareComposeConversion(editor, editorMode, "rich", rawSource);
        if (restored?.content) editor.commands.setContent(restored.content);
      } else if (restoredDraft?.richTextHtml) {
        editor.commands.setContent(restoredDraft.richTextHtml);
      } else if (editorContent) {
        editor.commands.setContent(editorContent);
      }
      if (isReply && !restoredDraft) {
        requestAnimationFrame(() => { if (!editor.isDestroyed) editor.commands.focus("start"); });
      }
      contentInitializedRef.current = true;
    } else if (editorMode === "rich") {
      replaceEditorSignature(editor, signature);
    } else {
      nextSource = replaceSourceSignature(rawSource, signature);
      setRawSource(nextSource);
      const converted = prepareComposeConversion(editor, editorMode, "rich", nextSource);
      if (converted?.content) editor.commands.setContent(converted.content);
    }
    // A From change may replace an edited signature. Never promote that edited
    // message back to pristine just because the generated result looks blank.
    setPristineBody(keepPristine ? { html: editor.getHTML(), source: nextSource } : null);
    setRichTextHtml(editor.getHTML());
    setAppliedAccount(fromAccountId);
  }, [editor, editorContent, isReply, restoredDraft, signatureReady, appliedAccount, fromAccountId, editorMode, rawSource, signature, bodyIsPristine, composeMode, prefillBody, composeReplyTo]);

  useEffect(() => {
    if (!editor) {
      return;
    }

    const syncRichTextHtml = () => {
      setRichTextHtml(editor.getHTML());
    };

    syncRichTextHtml();
    editor.on("update", syncRichTextHtml);

    return () => {
      editor.off("update", syncRichTextHtml);
    };
  }, [editor]);

  // Switch between modes, syncing content
  function switchMode(newMode: EditorMode) {
    if (newMode === editorMode || !editor || !editorReady) return;

    const converted = prepareComposeConversion(editor, editorMode, newMode, rawSource);
    if (!converted) {
      // Validate before ANY transaction/state change: no undo entry, selection
      // reset, autosave snapshot or mode change on refusal.
      setConversionError(true);
      return;
    }
    if (converted.content) editor.commands.setContent(converted.content);
    // Mode conversion alone is not an edit, but conversion must never establish
    // a new pristine baseline for user-authored source/body content.
    setPristineBody(bodyIsPristine ? { html: editor.getHTML(), source: converted.source } : null);
    setRawSource(converted.source);
    setEditorMode(newMode);
    setConversionError(false);
  }

  return {
    editor,
    editorReady,
    bodyIsPristine,
    signatureError,
    conversionError,
    retrySignature: () => setSignatureAttempt((attempt) => attempt + 1),
    editorMode,
    rawSource,
    setRawSource,
    richTextHtml,
    htmlPreview,
    setHtmlPreview,
    switchMode,
    textareaRef,
    quotedReplyHtml,
  };
}
