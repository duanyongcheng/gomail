import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { StrictMode } from "react";
import type { TFunction } from "i18next";
import { useComposeEditor, type EditorMode } from "../../src/hooks/useComposeEditor";
import { getSignature } from "../../src/lib/signatures";

vi.mock("../../src/lib/signatures", () => ({ getSignature: vi.fn() }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const t = ((key: string) => key) as TFunction;
const base = { composeMode: "new", composeReplyTo: null, isReply: false, t, restoredDraft: null };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => { resolve = res; });
  return { promise, resolve };
}

it("does not enable editing until signature and initial content are applied", async () => {
  const pending = deferred<string>();
  vi.mocked(getSignature).mockReturnValue(pending.promise);
  const { result } = renderHook(() => useComposeEditor({ ...base, fromAccountId: "a", prefillBody: "Prefilled body" }));
  await waitFor(() => expect(result.current.editor).not.toBeNull());
  expect(result.current.editorReady).toBe(false);
  expect(result.current.editor!.isEditable).toBe(false);
  act(() => result.current.switchMode("html"));
  expect(result.current.editorMode).toBe("rich");
  await act(async () => { pending.resolve("Saved signature"); });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.isEditable).toBe(true);
  expect(result.current.editor!.getText()).toContain("Prefilled body");
  expect(result.current.editor!.getText()).toContain("Saved signature");
  act(() => { result.current.editor!.commands.insertContentAt(1, "User edits "); });
  expect(result.current.editor!.getText()).toContain("User edits");
});

it("keeps empty documents and ordinary typing separate from the signature block under StrictMode", async () => {
  vi.mocked(getSignature).mockImplementation(async account => account === "a" ? "" : "B signature");
  const { result, rerender } = renderHook(({ account }) => useComposeEditor({ ...base, fromAccountId: account }), { initialProps: { account: "a" }, wrapper: StrictMode });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.state.doc.firstChild!.type.name).toBe("paragraph");
  act(() => { result.current.editor!.commands.insertContentAt(1, "User body"); });
  rerender({ account: "b" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.getText()).toContain("User body");
  expect(result.current.editor!.getText()).toContain("B signature");
  rerender({ account: "a" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.getText()).toContain("User body");
  expect(result.current.editor!.getText()).not.toContain("B signature");
});

it("keeps read errors blocked and retries without initializing from an empty fallback", async () => {
  vi.mocked(getSignature).mockRejectedValueOnce(new Error("locked")).mockResolvedValueOnce("Recovered");
  const { result } = renderHook(() => useComposeEditor({ ...base, fromAccountId: "a" }));
  await waitFor(() => expect(result.current.signatureError).toBe(true));
  expect(result.current.editorReady).toBe(false);
  expect(result.current.editor!.isEditable).toBe(false);
  act(() => result.current.retrySignature());
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.getText()).toContain("Recovered");
});

it("switches only the automatic signature and ignores stale account results", async () => {
  const b = deferred<string>();
  const c = deferred<string>();
  vi.mocked(getSignature).mockImplementation(account => account === "a" ? Promise.resolve("Private A") : account === "b" ? b.promise : c.promise);
  const { result, rerender } = renderHook(({ account }) => useComposeEditor({ ...base, fromAccountId: account }), { initialProps: { account: "a" } });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  act(() => { result.current.editor!.commands.insertContentAt(1, "Keep my body, including Private A. "); });
  rerender({ account: "b" });
  expect(result.current.editorReady).toBe(false);
  expect(result.current.editor!.isEditable).toBe(false);
  rerender({ account: "c" });
  await act(async () => { c.resolve("Work C"); });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  await act(async () => { b.resolve("Wrong B"); });
  const html = result.current.editor!.getHTML();
  expect(html).toContain("Keep my body, including Private A.");
  expect(html).toContain("Work C");
  expect(html).not.toContain("Wrong B");
  expect((html.match(/data-gomail-signature/g) ?? []).length).toBe(1);
  expect(result.current.editor!.getText().match(/Private A/g)).toHaveLength(1);
});

it.each<EditorMode>(["rich", "markdown", "html"])("preserves body in %s mode and replaces/removes the selected signature", async (mode) => {
  vi.mocked(getSignature).mockImplementation(async account => account === "a" ? "Signature A" : account === "b" ? "Signature B" : "");
  const { result, rerender } = renderHook(({ account }) => useComposeEditor({ ...base, fromAccountId: account, prefillBody: "Body survives" }), { initialProps: { account: "a" } });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  act(() => result.current.switchMode(mode));
  if (mode !== "rich") {
    act(() => result.current.setRawSource(`Untouched source prefix\n\n${result.current.rawSource}`));
  }
  rerender({ account: "b" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  const content = () => mode === "rich" ? result.current.editor!.getHTML() : result.current.rawSource;
  expect(content()).toContain("Body survives");
  expect(content()).toContain("Signature B");
  expect(content()).not.toContain("Signature A");
  if (mode !== "rich") expect(content()).toMatch(/^Untouched source prefix\n\n/);
  act(() => result.current.switchMode("rich"));
  expect(result.current.editor!.getHTML()).toContain('data-gomail-signature="auto"');
  act(() => result.current.switchMode(mode));
  rerender({ account: "empty" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(content()).toContain("Body survives");
  expect(content()).not.toContain("Signature B");
});

it("restores original draft content without fetching or replacing its signature", async () => {
  const restored = { editorMode: "rich" as const, richTextHtml: '<p>Saved draft</p><p data-gomail-signature="auto">Old signature</p>', quotedReplyHtml: "<blockquote>Original quote</blockquote>" };
  vi.mocked(getSignature).mockImplementation(async account => `Current signature ${account}`);
  const { result, rerender } = renderHook(({ account }) => useComposeEditor({ ...base, fromAccountId: account, restoredDraft: restored }), { initialProps: { account: "a" } });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(getSignature).not.toHaveBeenCalled();
  expect(result.current.editor!.getText()).toContain("Old signature");
  rerender({ account: "b" });
  await waitFor(() => expect(result.current.editorReady).toBe(true));
  expect(result.current.editor!.getText()).toContain("Saved draft");
  expect(result.current.editor!.getText()).toContain("Current signature b");
  expect(result.current.editor!.getText()).not.toContain("Old signature");
  expect(result.current.quotedReplyHtml).toContain("Original quote");
  rerender({ account: "a" });
  await waitFor(() => expect(result.current.editor!.getText()).toContain("Current signature a"));
});
