import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TFunction } from "i18next";
import { useComposeEditor, type EditorMode } from "@/hooks/useComposeEditor";
import { useComposeDraft } from "@/hooks/useComposeDraft";
import { getSignature } from "@/lib/signatures";
import { saveDraft } from "@/lib/api";
import { renderMarkdownSnapshot } from "@/features/compose/markdown-snapshot";
import { signatureHtml } from "@/features/compose/signature";

vi.mock("@/lib/signatures", () => ({ getSignature: vi.fn() }));
vi.mock("@/lib/api", () => ({ saveDraft: vi.fn() }));
const t = ((key: string) => key) as TFunction;
const base = { composeMode: "new", composeReplyTo: null, isReply: false, t };
const imported = '<p>Hello</p><table><tbody><tr><td>Table cell</td></tr></tbody></table><p><img src="cid:logo" alt="invoice image"></p>';
const unsupported = [
  ["CID image and table", imported],
  ["CID image alone", '<p>Hello<img src="cid:logo" alt="invoice"></p>'],
  ["table alone", "<table><tr><th>Heading</th><td>Cell</td></tr></table>"],
  ["font", '<p><font face="Arial" size="5" color="red">Formatted</font></p>'],
  ["inline style", '<p style="text-align: right; color: red">Formatted</p>'],
  ["span style", '<p><span style="background-color: yellow">Highlighted</span></p>'],
  ["unknown tag", "<p><custom-invoice>Special</custom-invoice></p>"],
  ["unknown attribute", '<p data-invoice="123">Special</p>'],
  ["language/direction", '<p dir="rtl" lang="ar">Special</p>'],
  ["link attribute", '<p><a href="https://example.invalid" download="invoice">Link</a></p>'],
  ["styled bold", '<p><b style="color: red">Formatted</b></p>'],
  ["iframe", '<iframe src="https://example.invalid/private"></iframe>'],
  ["remote stylesheet", '<link rel="stylesheet" href="https://example.invalid/style"><p>Hello</p>'],
  ["remote image", '<img src="https://example.invalid/pixel">'],
  ["body attribute", '<html><body bgcolor="red"><p>Hello</p></body></html>'],
  ["comment", "<p>Hello<!--Keep this note--></p>"],
] as const;

beforeEach(() => {
  vi.mocked(getSignature).mockResolvedValue("Signature A");
  vi.mocked(saveDraft).mockResolvedValue("draft-1");
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); vi.resetAllMocks(); });

function restored(source: string, mode: EditorMode = "html") {
  return { editorMode: mode, rawSource: source, richTextHtml: source, quotedReplyHtml: "<blockquote><p>Quote</p></blockquote>" };
}

function renderEditor(source: string, mode: EditorMode = "html") {
  const draft = restored(source, mode);
  return renderHook(() => useComposeEditor({ ...base, fromAccountId: "a", restoredDraft: draft }));
}

describe.each(["rich", "markdown"] as const)("HTML -> %s", (target) => {
  it.each(unsupported)("refuses %s without touching source, document, selection or history", async (_label, html) => {
    const { result } = renderEditor(html);
    await waitFor(() => expect(result.current.editorReady).toBe(true));
    // Make the inactive rich editor nonempty, with an undo history. A rejected
    // source conversion must not mutate even this hidden editor state.
    act(() => { result.current.editor!.commands.insertContentAt(1, "Retain undo"); });
    const editor = result.current.editor!;
    const before = { state: editor.state, html: result.current.richTextHtml, source: result.current.rawSource, pristine: result.current.bodyIsPristine };
    const transaction = vi.fn();
    editor.on("transaction", transaction);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    act(() => result.current.switchMode(target));
    expect(result.current.conversionError).toBe(true);
    expect(result.current.editorMode).toBe("html");
    expect(result.current.editorReady).toBe(true);
    expect(result.current.rawSource).toBe(before.source);
    expect(result.current.richTextHtml).toBe(before.html);
    expect(result.current.bodyIsPristine).toBe(before.pristine);
    expect(editor.state).toBe(before.state);
    expect(transaction).not.toHaveBeenCalled();
    expect(confirm).not.toHaveBeenCalled();
    expect(result.current.quotedReplyHtml).toContain("Quote");
    editor.off("transaction", transaction);
    act(() => { editor.commands.undo(); });
    expect(editor.getText()).not.toContain("Retain undo");
  });

  it.each([
    "<p>Hello world</p>",
    "<p>Hello <b>bold</b> and <i>italic</i></p>",
    "<p><em><strong>Hello bold italic</strong></em></p>",
    '<p>Hello <a href="https://example.invalid/path?q=1&amp;x=2" title="Site">link</a></p>',
    "<p>Hello **literal**, _not italic_ and # text</p>",
    "<p>Hello<br>second line<br></p><p></p>",
    "<p>Hello&nbsp;<strong>bold</strong>&nbsp;end</p>",
    "<h2>Hello heading</h2><blockquote><p>Hello quote</p></blockquote><hr>",
    '<p>Body</p>' + signatureHtml("Saved signature"),
  ])("allows supported HTML without introducing Markdown interpretation: %s", async (html) => {
    const { result } = renderEditor(html);
    await waitFor(() => expect(result.current.editorReady).toBe(true));
    act(() => result.current.switchMode(target));
    expect(result.current.conversionError).toBe(false);
    expect(result.current.editorMode).toBe(target);
    const output = target === "rich" ? result.current.editor!.getHTML() : renderMarkdownSnapshot(result.current.rawSource).html;
    expect(output).toContain(html.includes("Hello") ? "Hello" : "Body");
    if (html.includes("**literal**")) {
      expect(output).toContain("**literal**, _not italic_ and # text");
      expect(output).not.toContain("<strong>");
    }
    if (html.includes("Saved signature")) expect(output).toContain('data-gomail-signature="auto"');
    if (html.includes('href="')) expect(output).toContain('href="https://example.invalid/path?q=1&amp;x=2"');
  });
});

it("keeps a rejected HTML draft editable and passes the original source to the real save hook", async () => {
  const draft = restored(imported);
  const attachments = [{ name: "logo.png", path: "compose-attachment:synthetic-logo", size: 42 }];
  const { result } = renderHook(() => {
    const editor = useComposeEditor({ ...base, fromAccountId: "a", restoredDraft: draft });
    const persistence = useComposeDraft({
      to: ["recipient@example.invalid"], cc: [], bcc: ["bcc@example.invalid"], subject: "Imported",
      rawSource: editor.rawSource, richTextHtml: editor.richTextHtml, editorMode: editor.editorMode,
      composeMode: "new", fromAccountId: "a", attachments, editorReady: editor.editorReady,
      existingDraftId: "draft-1", existingDraftAccountId: "a", quotedReplyHtml: editor.quotedReplyHtml,
    });
    return { ...editor, persistence };
  });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  await act(async () => { expect(await result.current.persistence.saveNow()).toBe(true); });
  const first = vi.mocked(saveDraft).mock.calls[0][0];
  // The error-only rerender must not produce a new autosave snapshot.
  vi.useFakeTimers();
  act(() => result.current.switchMode("markdown"));
  act(() => result.current.switchMode("rich"));
  expect(result.current.conversionError).toBe(true);
  await act(async () => { await vi.advanceTimersByTimeAsync(3100); });
  expect(vi.mocked(saveDraft).mock.calls.slice(1).every(([request]) => JSON.stringify(request) === JSON.stringify(first))).toBe(true);
  vi.useRealTimers();
  await act(async () => { expect(await result.current.persistence.saveNow()).toBe(true); });
  expect(vi.mocked(saveDraft).mock.lastCall![0]).toEqual(first);
  expect(first).toMatchObject({ editorMode: "html", rawSource: imported, bodyHtml: imported, attachmentPaths: [attachments[0].path] });
  act(() => result.current.setRawSource(imported.replace("Hello", "Edited in HTML")));
  expect(result.current.conversionError).toBe(false);
  await act(async () => { expect(await result.current.persistence.saveNow()).toBe(true); });
  expect(vi.mocked(saveDraft).mock.lastCall![0]).toMatchObject({ editorMode: "html", bodyHtml: imported.replace("Hello", "Edited in HTML") });
});

it("preserves unsupported source and automatic signature identity across From changes", async () => {
  vi.mocked(getSignature).mockImplementation(async account => `Signature ${account}`);
  const original = imported + signatureHtml("Imported signature");
  const draft = restored(original);
  const { result, rerender } = renderHook(({ account }) => useComposeEditor({ ...base, fromAccountId: account, restoredDraft: draft }), { initialProps: { account: "a" } });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(getSignature).not.toHaveBeenCalled();
  act(() => result.current.switchMode("rich"));
  expect(result.current.rawSource).toBe(original);
  rerender({ account: "b" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.rawSource).toBe(imported + signatureHtml("Signature b"));
  act(() => result.current.switchMode("markdown"));
  expect(result.current.editorMode).toBe("html");
  expect(result.current.rawSource).toBe(imported + signatureHtml("Signature b"));
  expect(result.current.rawSource.match(/data-gomail-signature/g)).toHaveLength(1);
});

it("does not parse remote-loading markup in an active document even on initial restore", async () => {
  const parse = vi.spyOn(DOMParser.prototype, "parseFromString");
  const html = '<p>Body</p><img src="https://example.invalid/pixel"><style>@import "https://example.invalid/css";</style>';
  const { result } = renderEditor(html);
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  act(() => result.current.switchMode("markdown"));
  act(() => result.current.switchMode("rich"));
  expect(result.current.editorMode).toBe("html");
  expect(parse.mock.calls.every(([source]) => !String(source).includes("https://example.invalid"))).toBe(true);
});

it("does not fall back to stale HTML when an authoritative source is intentionally empty", async () => {
  const parse = vi.spyOn(DOMParser.prototype, "parseFromString");
  const draft = { ...restored(""), richTextHtml: '<img src="https://example.invalid/stale">' };
  const { result } = renderHook(() => useComposeEditor({ ...base, fromAccountId: "a", restoredDraft: draft }));
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.rawSource).toBe("");
  expect(parse.mock.calls.every(([source]) => !String(source).includes("https://example.invalid/stale"))).toBe(true);
  act(() => result.current.switchMode("rich"));
  expect(result.current.editorMode).toBe("rich");
  expect(result.current.editor!.getText()).toBe("");
});

it("clears refusal when source changes, then permits a successful conversion", async () => {
  const { result } = renderEditor(imported);
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  act(() => result.current.switchMode("rich"));
  expect(result.current.conversionError).toBe(true);
  act(() => result.current.setRawSource("<p>Simple now</p>"));
  expect(result.current.conversionError).toBe(false);
  act(() => result.current.switchMode("rich"));
  expect(result.current.editorMode).toBe("rich");
  expect(result.current.editor!.getHTML()).toBe("<p>Simple now</p>");
});

it.each(["rich", "html"] as const)("refuses unsupported raw HTML in Markdown -> %s", async (target) => {
  const source = "# Heading\n\n" + imported;
  const { result } = renderEditor(source, "markdown");
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  const state = result.current.editor!.state;
  act(() => result.current.switchMode(target));
  expect(result.current.editorMode).toBe("markdown");
  expect(result.current.rawSource).toBe(source);
  expect(result.current.editor!.state).toBe(state);
  expect(result.current.conversionError).toBe(true);
});

it.each(["markdown", "html"] as const)("allows ordinary rich bold/link and signature roundtrip through %s", async (mode) => {
  const { result } = renderHook(() => useComposeEditor({ ...base, fromAccountId: "a", restoredDraft: null }));
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  act(() => { result.current.editor!.commands.insertContentAt(1, '<strong>Bold</strong> <a href="https://example.invalid">Link</a>'); });
  const html = result.current.editor!.getHTML();
  act(() => result.current.switchMode(mode));
  expect(result.current.editorMode).toBe(mode);
  expect(result.current.conversionError).toBe(false);
  act(() => result.current.switchMode("rich"));
  expect(result.current.editorMode).toBe("rich");
  expect(result.current.editor!.getHTML()).toBe(html);
});
