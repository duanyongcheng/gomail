import { act, renderHook } from "@testing-library/react";
import { StrictMode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { loadDraftFromStorage, useComposeDraft } from "../../src/hooks/useComposeDraft";
import { saveDraft } from "../../src/lib/api";

vi.mock("../../src/lib/api", () => ({
  saveDraft: vi.fn(),
}));

const saveDraftMock = vi.mocked(saveDraft);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function baseProps(overrides: Partial<Parameters<typeof useComposeDraft>[0]> = {}) {
  return {
    to: [],
    cc: [],
    bcc: [],
    subject: "",
    rawSource: "",
    richTextHtml: "",
    editorMode: "rich" as const,
    composeMode: "new",
    fromAccountId: "account-1",
    editorReady: true,
    attachments: [],
    ...overrides,
  };
}

describe("useComposeDraft", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    localStorage.clear();
    saveDraftMock.mockReset();
  });

  it("does not autosave or explicitly save before editor initialization finishes", async () => {
    saveDraftMock.mockResolvedValue("draft-1");
    const { result, rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "Do not save incomplete body", editorReady: false }),
    });
    await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
    expect(await result.current.saveNow()).toBe(false);
    expect(saveDraftMock).not.toHaveBeenCalled();
    rerender(baseProps({ subject: "Complete", editorReady: true, richTextHtml: "<p>Body and signature</p>" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
    expect(saveDraftMock).toHaveBeenCalledOnce();
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({ bodyHtml: "<p>Body and signature</p>" }));
  });

  it("restores a valid legacy draft without deleting it before migration", () => {
    const legacyDraft = {
      accountId: "account-1",
      to: ["recipient@example.com"],
      cc: [],
      bcc: [],
      subject: "Important draft",
      rawSource: "",
      richTextHtml: "<p>Do not lose this</p>",
      editorMode: "rich",
      attachments: [],
      savedAt: Date.now(),
    };
    localStorage.setItem("gomail-compose-draft", JSON.stringify(legacyDraft));

    expect(loadDraftFromStorage(["account-1"])).toEqual(legacyDraft);
    expect(localStorage.getItem("gomail-compose-draft")).toBe(JSON.stringify(legacyDraft));
  });

  it("clears a restored legacy draft only after backend persistence succeeds", async () => {
    const storedDraft = JSON.stringify({
      accountId: "account-1",
      to: [], cc: [], bcc: [], subject: "Legacy", rawSource: "",
      richTextHtml: "", editorMode: "rich", attachments: [], savedAt: Date.now(),
    });
    localStorage.setItem("gomail-compose-draft", storedDraft);
    saveDraftMock.mockResolvedValue("draft-1");
    const props = ({ ...baseProps({ subject: "Legacy" }), migrateLegacyDraft: true }) as Parameters<typeof useComposeDraft>[0];

    renderHook(() => useComposeDraft(props));
    await act(async () => {
      vi.advanceTimersByTime(3000);
      await Promise.resolve();
    });

    expect(saveDraftMock).toHaveBeenCalledOnce();
    expect(localStorage.getItem("gomail-compose-draft")).toBeNull();
  });

  it("retains a restored legacy draft when backend persistence fails", async () => {
    const storedDraft = JSON.stringify({
      accountId: "account-1",
      to: [], cc: [], bcc: [], subject: "Legacy", rawSource: "",
      richTextHtml: "", editorMode: "rich", attachments: [], savedAt: Date.now(),
    });
    localStorage.setItem("gomail-compose-draft", storedDraft);
    saveDraftMock.mockRejectedValue(new Error("write failed"));
    const props = ({ ...baseProps({ subject: "Legacy" }), migrateLegacyDraft: true }) as Parameters<typeof useComposeDraft>[0];

    renderHook(() => useComposeDraft(props));
    await act(async () => {
      vi.advanceTimersByTime(3000);
      await Promise.resolve();
    });

    expect(localStorage.getItem("gomail-compose-draft")).toBe(storedDraft);
  });

  it("retries a transient autosave failure without requiring another edit", async () => {
    saveDraftMock
      .mockRejectedValueOnce(new Error("temporary provider failure"))
      .mockResolvedValueOnce("draft-1");
    renderHook(() => useComposeDraft(baseProps({ subject: "Retry me" })));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(saveDraftMock).toHaveBeenCalledOnce();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(saveDraftMock).toHaveBeenCalledTimes(2);
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      subject: "Retry me",
      existingDraftId: undefined,
    }));
  });

  it("autosaves under React StrictMode effect replay", async () => {
    saveDraftMock.mockResolvedValue("draft-1");

    renderHook(() => useComposeDraft(baseProps({ subject: "Strict draft" })), {
      wrapper: StrictMode,
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    expect(saveDraftMock).toHaveBeenCalledOnce();
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({
      subject: "Strict draft",
    }));
  });

  it("gives a newer snapshot a fresh retry budget when it arrives during retry sleep", async () => {
    saveDraftMock.mockRejectedValue(new Error("provider unavailable"));
    const { rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "Old snapshot" }) as Parameters<typeof useComposeDraft>[0],
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000 + 1_000 + 2_000);
    });
    expect(saveDraftMock).toHaveBeenCalledTimes(3);

    rerender(baseProps({ subject: "New snapshot" }) as Parameters<typeof useComposeDraft>[0]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000 + 1_000 + 1_000 + 2_000 + 4_000);
    });

    const newSnapshotAttempts = saveDraftMock.mock.calls.filter(
      ([args]) => args.subject === "New snapshot",
    );
    expect(newSnapshotAttempts).toHaveLength(4);
  });

  it("bounds automatic autosave retries", async () => {
    saveDraftMock.mockRejectedValue(new Error("provider unavailable"));
    renderHook(() => useComposeDraft(baseProps({ subject: "Do not spin" })));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000 + 1_000 + 2_000 + 4_000);
    });
    expect(saveDraftMock).toHaveBeenCalledTimes(4);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(saveDraftMock).toHaveBeenCalledTimes(4);
  });

  it("cancels a scheduled autosave retry during cleanup", async () => {
    saveDraftMock.mockRejectedValue(new Error("provider unavailable"));
    const { result } = renderHook(() => useComposeDraft(baseProps({ subject: "Cancel retry" })));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(saveDraftMock).toHaveBeenCalledOnce();

    await expect(result.current.cancelPendingDraftSaves()).resolves.toEqual({});
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(saveDraftMock).toHaveBeenCalledOnce();
  });

  it("autosaves attachments only to the backend draft", async () => {
    saveDraftMock.mockResolvedValue("draft-1");

    renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({
        attachments: [{ name: "report.pdf", path: "C:\\tmp\\report.pdf", size: 1234 }],
      }) as Parameters<typeof useComposeDraft>[0],
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    expect(saveDraftMock).toHaveBeenCalledOnce();
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({
      attachmentPaths: ["C:\\tmp\\report.pdf"],
    }));
    expect(localStorage.getItem("gomail-compose-draft")).toBeNull();
  });

  it("does not reuse a stale draft id after switching accounts", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    saveDraftMock
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
      .mockResolvedValue("draft-account-2-later");

    const { rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "A", fromAccountId: "account-1" }) as Parameters<typeof useComposeDraft>[0],
    });

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({
      accountId: "account-1",
      existingDraftId: undefined,
    }));

    rerender(baseProps({ subject: "B", fromAccountId: "account-2" }) as Parameters<typeof useComposeDraft>[0]);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      accountId: "account-2",
      existingDraftId: undefined,
    }));

    await act(async () => {
      first.resolve("draft-account-1");
      await first.promise;
    });

    rerender(baseProps({ subject: "B updated", fromAccountId: "account-2" }) as Parameters<typeof useComposeDraft>[0]);
    act(() => {
      vi.advanceTimersByTime(3000);
    });

    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      accountId: "account-2",
      existingDraftId: undefined,
    }));
  });

  it("serializes saves for one account and reuses the first saved draft id", async () => {
    const first = deferred<string>();
    saveDraftMock.mockReturnValueOnce(first.promise).mockResolvedValueOnce("draft-1");
    const { rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "First version" }) as Parameters<typeof useComposeDraft>[0],
    });

    act(() => vi.advanceTimersByTime(3000));
    expect(saveDraftMock).toHaveBeenCalledOnce();

    rerender(baseProps({ subject: "Latest version" }) as Parameters<typeof useComposeDraft>[0]);
    act(() => vi.advanceTimersByTime(3000));

    expect(saveDraftMock).toHaveBeenCalledOnce();

    await act(async () => {
      first.resolve("draft-1");
      await first.promise;
      await Promise.resolve();
    });

    expect(saveDraftMock).toHaveBeenCalledTimes(2);
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      accountId: "account-1",
      subject: "Latest version",
      existingDraftId: "draft-1",
    }));
  });

  it("restores the existing draft id and preserves source/reply fields on explicit save", async () => {
    saveDraftMock.mockResolvedValue("existing");
    const { result } = renderHook(() => useComposeDraft(baseProps({
      existingDraftId: "existing", existingDraftAccountId: "account-1",
      editorMode: "markdown", rawSource: "**source**", inReplyTo: "original@example.invalid",
    })));
    await act(async () => { expect(await result.current.saveNow()).toBe(true); });
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      existingDraftId: "existing", rawSource: "**source**", editorMode: "markdown", inReplyTo: "original@example.invalid",
      clientKey: expect.any(String),
    }));
    expect(result.current.saveStatus).toBe("saved");
  });

  it("does not display saved for edits made while an older snapshot is saving", async () => {
    const first = deferred<string>();
    saveDraftMock.mockReturnValueOnce(first.promise).mockResolvedValue("draft-1");
    const { result, rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "first" }),
    });
    await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
    rerender(baseProps({ subject: "newer" }));
    await act(async () => { first.resolve("draft-1"); await first.promise; });
    expect(result.current.saveStatus).not.toBe("saved");
    await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
    expect(result.current.saveStatus).toBe("saved");
  });

  it("preserves reply quotes separately from editable source", async () => {
    saveDraftMock.mockResolvedValue("d");
    const { result } = renderHook(() => useComposeDraft(baseProps({
      subject: "reply", editorMode: "markdown", rawSource: "**answer**",
      quotedReplyHtml: "<blockquote>original</blockquote>",
    })));
    await act(async () => { await result.current.saveNow(); });
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({
      rawSource: "**answer**", renderedSource: "**answer**", bodyHtml: "<p><strong>answer</strong></p>",
      bodyText: "answer", quotedReplyHtml: "<blockquote>original</blockquote>",
    }));
  });
  it("keeps imported HTML source intact while saving a decoded plain-text alternative", async () => {
    saveDraftMock.mockResolvedValue("imported");
    const source = '<table><tr><td>A &amp; B</td><td>Second</td></tr></table><img src="cid:image-1">';
    const { result } = renderHook(() => useComposeDraft(baseProps({
      existingDraftId: "imported", existingDraftAccountId: "account-1",
      editorMode: "html", rawSource: source, richTextHtml: "<p>Lossy editor representation</p>",
    })));
    await act(async () => { expect(await result.current.saveNow()).toBe(true); });
    expect(saveDraftMock).toHaveBeenCalledWith(expect.objectContaining({
      rawSource: source, bodyHtml: source, bodyText: "A & B\tSecond", existingDraftId: "imported",
    }));
  });

  it("renders each Markdown snapshot without replacing its source or stale rich body", async () => {
    saveDraftMock.mockResolvedValue("existing");
    const { result, rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({
        existingDraftId: "existing", existingDraftAccountId: "account-1",
        editorMode: "markdown", rawSource: "# New title\n\n**Body** & details",
        richTextHtml: "<p>Stale rich content</p>",
      }),
    });
    await act(async () => { expect(await result.current.saveNow()).toBe(true); });
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      existingDraftId: "existing", rawSource: "# New title\n\n**Body** & details",
      renderedSource: "# New title\n\n**Body** & details",
      bodyHtml: "<h1>New title</h1><p><strong>Body</strong> &amp; details</p>",
    }));
    rerender(baseProps({
      existingDraftId: "existing", existingDraftAccountId: "account-1",
      editorMode: "markdown", rawSource: "**Edited**", richTextHtml: "<p>Still stale</p>",
    }));
    await act(async () => { expect(await result.current.saveNow()).toBe(true); });
    expect(saveDraftMock).toHaveBeenLastCalledWith(expect.objectContaining({
      existingDraftId: "existing", renderedSource: "**Edited**",
      bodyHtml: "<p><strong>Edited</strong></p>", bodyText: "Edited",
    }));
  });

  it("uses the same idempotency key after a failed create", async () => {
    saveDraftMock.mockRejectedValueOnce(new Error("response lost")).mockResolvedValue("draft-1");
    renderHook(() => useComposeDraft(baseProps({ subject: "retry" })));
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(saveDraftMock.mock.calls[0][0].clientKey).toBe(saveDraftMock.mock.calls[1][0].clientKey);
  });

  it("waits for an in-flight save and cancels future saves during discard", async () => {
    const first = deferred<string>();
    saveDraftMock.mockReturnValueOnce(first.promise);
    const { result, rerender } = renderHook((props) => useComposeDraft(props), {
      initialProps: baseProps({ subject: "Discard me" }) as Parameters<typeof useComposeDraft>[0],
    });
    act(() => vi.advanceTimersByTime(3000));
    expect(saveDraftMock).toHaveBeenCalledOnce();

    const cleanupPromise = result.current.cancelPendingDraftSaves();
    await act(async () => {
      first.resolve("draft-1");
      await first.promise;
    });

    await expect(cleanupPromise).resolves.toEqual({ "account-1": "draft-1" });

    rerender(baseProps({ subject: "Should not save" }) as Parameters<typeof useComposeDraft>[0]);
    act(() => vi.advanceTimersByTime(3000));
    expect(saveDraftMock).toHaveBeenCalledOnce();
  });
});
