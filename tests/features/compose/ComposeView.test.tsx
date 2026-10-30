import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ComposeView from "../../../src/features/compose/ComposeView";
import { listTemplates, saveTemplate } from "@/lib/templates";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
import {
  cleanupStagedComposeAttachment,
  deleteDraft,
  stageComposeAttachment,
} from "../../../src/lib/api";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  closeCompose: vi.fn(),
  setComposeDirty: vi.fn(),
  addToast: vi.fn(),
  loadDraftFromStorage: vi.fn(),
  cancelPendingDraftSaves: vi.fn(),
  saveNow: vi.fn(),
  resumeDraftSaves: vi.fn(),
  confirmCloseCompose: vi.fn(),
  cancelCloseCompose: vi.fn(),
  showComposeLeaveConfirm: false,
  quotedReplyHtml: "",
  editorReady: true,
  editorMode: "rich",
  rawSource: "",
  signatureError: false,
  retrySignature: vi.fn(),
  draftReadiness: [] as boolean[],
  accountsQuery: {
    data: [{ id: "account-1", email: "me@example.com", display_name: "Me" }],
    isLoading: false,
    isSuccess: true,
    isError: false,
  },
  recipients: {
    to: ["to@example.com"] as string[],
    cc: [] as string[],
    bcc: [] as string[],
  },
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (_key: string, fallback?: string) => fallback ?? _key }),
}));

vi.mock("../../../src/stores/mail.store", () => ({
  useMailStore: (selector: (state: { activeAccountId: string }) => unknown) =>
    selector({ activeAccountId: "account-1" }),
}));

vi.mock("../../../src/stores/compose.store", () => ({
  useComposeStore: Object.assign(
    (selector: (state: {
      composeMode: string;
      composeReplyTo: null;
      closeCompose: () => void;
      showComposeLeaveConfirm: boolean;
      confirmCloseCompose: () => void;
      cancelCloseCompose: () => void;
    }) => unknown) =>
      selector({
        composeMode: "new",
        composeReplyTo: null,
        closeCompose: mocks.closeCompose,
        showComposeLeaveConfirm: mocks.showComposeLeaveConfirm,
        confirmCloseCompose: mocks.confirmCloseCompose,
        cancelCloseCompose: mocks.cancelCloseCompose,
      }),
    {
      getState: () => ({ setComposeDirty: mocks.setComposeDirty }),
    },
  ),
}));

vi.mock("../../../src/features/compose/OutboxPanel", () => ({ default: () => null }));

vi.mock("../../../src/hooks/queries", () => ({
  useAccountsQuery: () => mocks.accountsQuery,
}));

vi.mock("../../../src/hooks/mutations", () => ({
  useSendEmailMutation: () => ({
    isPending: false,
    mutate: mocks.mutate,
  }),
}));

vi.mock("../../../src/hooks/useComposeRecipients", () => ({
  useComposeRecipients: () => ({
    fromAccountId: "account-1",
    setFromAccountId: vi.fn(),
    to: mocks.recipients.to,
    setTo: vi.fn(),
    cc: mocks.recipients.cc,
    setCc: vi.fn(),
    bcc: mocks.recipients.bcc,
    setBcc: vi.fn(),
    showCc: false,
    setShowCc: vi.fn(),
    showBcc: false,
    setShowBcc: vi.fn(),
  }),
}));

vi.mock("../../../src/hooks/useComposeDraft", () => ({
  useComposeDraft: ({ editorReady }: { editorReady: boolean }) => {
    mocks.draftReadiness.push(editorReady);
    return {
      draftIdRef: { current: "draft-1" },
      draftIdsByAccountRef: { current: { "account-1": "draft-1" } },
      cancelPendingDraftSaves: mocks.cancelPendingDraftSaves,
      saveNow: mocks.saveNow,
      saveStatus: "idle",
      resumeDraftSaves: mocks.resumeDraftSaves,
    };
  },
  loadDraftFromStorage: mocks.loadDraftFromStorage,
  clearDraftStorage: vi.fn(),
}));

vi.mock("../../../src/hooks/useComposeEditor", () => ({
  useComposeEditor: () => ({
    editorReady: mocks.editorReady,
    signatureError: mocks.signatureError,
    retrySignature: mocks.retrySignature,
    editor: {
      getHTML: () => "<p>Hello</p>",
      getText: () => "Hello",
      commands: { setContent: vi.fn() },
    },
    editorMode: mocks.editorMode,
    rawSource: mocks.rawSource,
    setRawSource: vi.fn(),
    richTextHtml: "<p>Hello</p>",
    htmlPreview: false,
    setHtmlPreview: vi.fn(),
    switchMode: vi.fn(),
    textareaRef: { current: null },
    quotedReplyHtml: mocks.quotedReplyHtml,
  }),
  appendReplyQuoteHtml: (bodyHtml: string, quotedReplyHtml: string) =>
    quotedReplyHtml.trim() ? `${bodyHtml}<br/><br/>${quotedReplyHtml.trim()}` : bodyHtml,
}));

vi.mock("../../../src/components/ContactAutocomplete", () => ({
  default: ({
    id,
    name,
    ariaLabelledBy,
    inputValue,
    placeholder,
    onInputValueChange,
  }: {
    id?: string;
    name?: string;
    ariaLabelledBy?: string;
    inputValue?: string;
    placeholder?: string;
    onInputValueChange?: (value: string) => void;
  }) => (
    <input
      id={id}
      name={name}
      role="combobox"
      aria-labelledby={ariaLabelledBy}
      value={inputValue ?? ""}
      placeholder={placeholder}
      onChange={(event) => onInputValueChange?.(event.currentTarget.value)}
    />
  ),
}));

vi.mock("../../../src/features/compose/ComposeToolbar", () => ({
  ModeButton: ({ label, onClick }: { label: string; onClick: () => void }) => (
    <button type="button" onClick={onClick}>{label}</button>
  ),
  EditorToolbar: () => <div />,
  MarkdownToolbar: () => <div />,
  composeStyles: {
    backBtn: {},
    fieldRow: {},
    fieldLabel: {},
    toggleBtn: {},
  },
}));

vi.mock("@tiptap/react", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tiptap/react")>(),
  EditorContent: () => <div data-testid="editor" />,
}));

vi.mock("../../../src/lib/templates", () => ({
  listTemplates: vi.fn(() => []),
  saveTemplate: vi.fn(),
  deleteTemplate: vi.fn(),
}));

vi.mock("../../../src/stores/confirm.store", () => ({
  useConfirmStore: { getState: () => ({ confirm: vi.fn().mockResolvedValue(true) }) },
}));

vi.mock("../../../src/stores/toast.store", () => ({
  useToastStore: { getState: () => ({ addToast: mocks.addToast }) },
}));

vi.mock("../../../src/lib/api", () => ({
  cleanupStagedComposeAttachment: vi.fn(),
  deleteDraft: vi.fn(),
  stageComposeAttachment: vi.fn(),
}));

function attachmentFile(name: string, contents = name): File {
  const file = new File([contents], name);
  Object.defineProperty(file, "arrayBuffer", {
    value: vi.fn().mockResolvedValue(new TextEncoder().encode(contents).buffer),
  });
  return file;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

describe("ComposeView", () => {
  it("blocks unsupported templates and AI while keeping compose controls available", () => {
    render(<ComposeView />);
    for (const name of ["Templates", "ai.menuTooltip"]) {
      const button = screen.getByRole("button", { name }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      expect(button.title).toMatch(/not available/);
      fireEvent.click(button);
    }
    expect(listTemplates).not.toHaveBeenCalled();
    expect(saveTemplate).not.toHaveBeenCalled();
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(screen.queryByRole("menu")).toBeNull();
    expect((screen.getByRole("button", { name: "Attach file" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Save and close" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false);
  });
  it("blocks send, save and draft snapshots while signature initialization is pending", () => {
    mocks.editorReady = false;
    const { rerender } = render(<ComposeView />);
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Save and close" }) as HTMLButtonElement).disabled).toBe(true);
    expect(mocks.draftReadiness.every((ready) => !ready)).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.saveNow).not.toHaveBeenCalled();
    expect(screen.getByText("Loading signature…")).toBeTruthy();
    mocks.signatureError = true;
    rerender(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.retrySignature).toHaveBeenCalledOnce();
    mocks.signatureError = false;
    mocks.editorReady = true;
    rerender(<ComposeView />);
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false);
    expect(mocks.draftReadiness.at(-1)).toBe(true);
  });

  it("uses rendered plain text instead of signature HTML markers for Markdown sends", async () => {
    mocks.editorMode = "markdown";
    mocks.rawSource = '<p data-pebble-signature="auto">Hello</p>';
    render(<ComposeView />);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Signature" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledOnce());
    const request = mocks.mutate.mock.calls[0][0];
    expect(request.localDraftId).toBe("draft-1");
    expect(request.bodyText.trim()).toBe("Hello");
    expect(request.bodyText).not.toContain("<");
    expect(request.bodyHtml).toContain(">Hello</p>");
  });

  let warnSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    mocks.mutate.mockReset();
    mocks.closeCompose.mockReset();
    mocks.setComposeDirty.mockReset();
    mocks.addToast.mockReset();
    mocks.loadDraftFromStorage.mockReset();
    mocks.cancelPendingDraftSaves.mockReset();
    mocks.cancelPendingDraftSaves.mockResolvedValue({ "account-1": "draft-1" });
    mocks.saveNow.mockReset();
    mocks.saveNow.mockResolvedValue(true);
    mocks.resumeDraftSaves.mockReset();
    mocks.confirmCloseCompose.mockReset();
    mocks.cancelCloseCompose.mockReset();
    mocks.showComposeLeaveConfirm = false;
    mocks.quotedReplyHtml = "";
    mocks.editorReady = true;
    mocks.editorMode = "rich";
    mocks.rawSource = "";
    mocks.signatureError = false;
    mocks.retrySignature.mockReset();
    mocks.draftReadiness = [];
    mocks.accountsQuery.data = [{ id: "account-1", email: "me@example.com", display_name: "Me" }];
    mocks.accountsQuery.isLoading = false;
    mocks.accountsQuery.isSuccess = true;
    mocks.accountsQuery.isError = false;
    mocks.recipients.to = ["to@example.com"];
    mocks.recipients.cc = [];
    mocks.recipients.bcc = [];
    mocks.loadDraftFromStorage.mockReturnValue(null);
    vi.mocked(deleteDraft).mockReset();
    vi.mocked(deleteDraft).mockResolvedValue(undefined);
    vi.mocked(cleanupStagedComposeAttachment).mockReset();
    vi.mocked(cleanupStagedComposeAttachment).mockResolvedValue(undefined);
    vi.mocked(stageComposeAttachment).mockReset();
    vi.mocked(stageComposeAttachment).mockImplementation(async (filename) => `staged/${filename}`);
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it("shows a user-visible error when sent draft cleanup fails", async () => {
    vi.mocked(deleteDraft).mockRejectedValue(new Error("remote draft delete failed"));
    mocks.mutate.mockImplementation((_params, options) => options.onSuccess());

    render(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(deleteDraft).toHaveBeenCalledWith("account-1", "draft-1"));
    await waitFor(() => expect(mocks.addToast).toHaveBeenCalledWith(expect.objectContaining({
      type: "error",
    })));
  });

  it("does not queue or remove the local draft when its final snapshot cannot be saved", async () => {
    mocks.saveNow.mockResolvedValue(false);
    render(<ComposeView />);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Keep this" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText("Could not save this draft. Nothing was queued; keep editing or retry.");
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.cancelPendingDraftSaves).not.toHaveBeenCalled();
    expect(deleteDraft).not.toHaveBeenCalled();
    expect(mocks.closeCompose).not.toHaveBeenCalled();
    expect(mocks.resumeDraftSaves).toHaveBeenCalledOnce();
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("waits for the final snapshot and passes only the selected account's local draft identity", async () => {
    const saved = deferred<boolean>();
    mocks.saveNow.mockReturnValue(saved.promise);
    mocks.cancelPendingDraftSaves.mockResolvedValue({ "account-1": "correct-draft", "account-2": "other-draft" });
    render(<ComposeView />);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Linked send" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.cancelPendingDraftSaves).not.toHaveBeenCalled();
    await act(async () => { saved.resolve(true); });
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledWith(expect.objectContaining({
      accountId: "account-1", localDraftId: "correct-draft", subject: "Linked send",
    }), expect.any(Object)));
    expect(mocks.saveNow).toHaveBeenCalledWith(expect.objectContaining({
      to: ["to@example.com"], subject: "Linked send", bodyHtml: "<p>Hello</p>", bodyText: "Hello",
    }));
  });

  it("resumes saving and keeps the local draft when queueing fails", async () => {
    mocks.mutate.mockImplementation((_params, options) => options.onError(new Error("Queue unavailable")));
    render(<ComposeView />);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Retry later" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText("Queue unavailable");
    expect(mocks.resumeDraftSaves).toHaveBeenCalledOnce();
    expect(deleteDraft).not.toHaveBeenCalled();
    expect(mocks.closeCompose).not.toHaveBeenCalled();
  });

  it("keeps another account's saved snapshot when the selected account sends", async () => {
    mocks.cancelPendingDraftSaves.mockResolvedValue({ "account-1": "sent-draft", "account-2": "keep-draft" });
    mocks.mutate.mockImplementation((_params, options) => options.onSuccess());
    render(<ComposeView />);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Selected account only" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.closeCompose).toHaveBeenCalledOnce());
    expect(deleteDraft).toHaveBeenCalledExactlyOnceWith("account-1", "sent-draft");
  });

  it("prevents duplicate sends while successful-send cleanup is pending", async () => {
    const pendingDeletion = deferred<void>();
    vi.mocked(deleteDraft).mockReturnValue(pendingDeletion.promise);
    mocks.mutate.mockImplementation((_params, options) => options.onSuccess());

    render(<ComposeView />);
    const sendButton = screen.getByRole("button", { name: "Send" }) as HTMLButtonElement;
    fireEvent.click(sendButton);
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledOnce());
    fireEvent.click(sendButton);

    expect(mocks.mutate).toHaveBeenCalledOnce();
    expect(sendButton.disabled).toBe(true);

    await act(async () => pendingDeletion.resolve());
    await waitFor(() => expect(mocks.closeCompose).toHaveBeenCalledOnce());
  });

  it("validates a restored new-message draft against loaded account ids", () => {
    render(<ComposeView />);

    expect(mocks.loadDraftFromStorage).toHaveBeenCalledWith(["account-1"]);
  });

  it("preserves a legacy draft when the accounts query fails", () => {
    mocks.accountsQuery.data = [];
    mocks.accountsQuery.isSuccess = false;
    mocks.accountsQuery.isError = true;

    render(<ComposeView />);

    expect(mocks.loadDraftFromStorage).toHaveBeenCalledWith(undefined);
  });

  it("sends a typed valid recipient without requiring Enter first", async () => {
    mocks.recipients.to = [];

    render(<ComposeView />);

    const sendButton = screen.getByRole("button", { name: "Send" }) as HTMLButtonElement;
    expect(sendButton.disabled).toBe(true);

    fireEvent.change(screen.getByRole("combobox", { name: "To" }), {
      target: { value: "typed@example.com" },
    });

    await waitFor(() => expect(sendButton.disabled).toBe(false));
    fireEvent.click(sendButton);

    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ to: ["typed@example.com"] }),
      expect.any(Object),
    ));
  });

  it("keeps quoted replies collapsed until the user expands them", () => {
    mocks.quotedReplyHtml = "<blockquote><p>Original message body</p></blockquote>";

    render(<ComposeView />);

    expect(screen.getByRole("button", { name: "Show quoted message" })).toBeTruthy();
    expect(screen.queryByText("Original message body")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show quoted message" }));

    expect(screen.getByText("Original message body")).toBeTruthy();
  });

  it("uses the first attached filename as an empty subject", async () => {
    render(<ComposeView />);

    const first = attachmentFile("季度报告.pdf", "quarterly results");
    const second = attachmentFile("supporting-data.xlsx", "supporting data");

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    expect(fileInput).not.toBeNull();
    fireEvent.change(fileInput!, { target: { files: [first, second] } });

    await waitFor(() => expect(vi.mocked(stageComposeAttachment)).toHaveBeenCalledTimes(2));
    await waitFor(() => expect((screen.getByLabelText("Subject") as HTMLInputElement).value)
      .toBe("季度报告.pdf"));
  });

  it("does not let a later concurrent attachment refill a subject the user cleared", async () => {
    let resolveFirst!: (path: string) => void;
    let resolveSecond!: (path: string) => void;
    vi.mocked(stageComposeAttachment).mockImplementation((filename) => new Promise((resolve) => {
      if (filename === "first.pdf") resolveFirst = resolve;
      if (filename === "second.pdf") resolveSecond = resolve;
    }));

    render(<ComposeView />);

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    const subjectInput = screen.getByLabelText("Subject") as HTMLInputElement;
    expect(fileInput).not.toBeNull();

    fireEvent.change(fileInput!, { target: { files: [attachmentFile("first.pdf")] } });
    await waitFor(() => expect(stageComposeAttachment).toHaveBeenCalledWith("first.pdf", expect.any(Array)));
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("second.pdf")] } });
    await waitFor(() => expect(stageComposeAttachment).toHaveBeenCalledWith("second.pdf", expect.any(Array)));

    await act(async () => resolveFirst("staged/first.pdf"));
    await waitFor(() => expect(subjectInput.value).toBe("first.pdf"));
    fireEvent.change(subjectInput, { target: { value: "" } });

    await act(async () => resolveSecond("staged/second.pdf"));
    await waitFor(() => expect(screen.getByText("second.pdf")).toBeTruthy());
    expect(subjectInput.value).toBe("");
  });

  it("keeps successfully staged files when a later file in the batch fails", async () => {
    vi.mocked(stageComposeAttachment)
      .mockResolvedValueOnce("staged/first.pdf")
      .mockRejectedValueOnce(new Error("staging failed"));

    render(<ComposeView />);

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    expect(fileInput).not.toBeNull();
    fireEvent.change(fileInput!, {
      target: { files: [attachmentFile("first.pdf"), attachmentFile("broken.pdf")] },
    });

    await waitFor(() => expect(screen.getByText("first.pdf")).toBeTruthy());
    expect(screen.queryByText("broken.pdf")).toBeNull();
    expect((screen.getByLabelText("Subject") as HTMLInputElement).value).toBe("first.pdf");
    expect(screen.getByRole("alert").textContent).toContain("Failed to attach file");
  });

  it("disables send until attachment staging finishes and sends the staged path", async () => {
    const pendingStage = deferred<string>();
    vi.mocked(stageComposeAttachment).mockReturnValue(pendingStage.promise);
    render(<ComposeView />);

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("slow.pdf")] } });
    await waitFor(() => expect(stageComposeAttachment)
      .toHaveBeenCalledWith("slow.pdf", expect.any(Array)));

    const sendButton = screen.getByRole("button", { name: "Send" }) as HTMLButtonElement;
    expect(sendButton.disabled).toBe(true);
    expect(mocks.mutate).not.toHaveBeenCalled();

    await act(async () => pendingStage.resolve("staged/slow.pdf"));
    await waitFor(() => expect(sendButton.disabled).toBe(false));
    fireEvent.click(sendButton);

    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ attachmentPaths: ["staged/slow.pdf"] }),
      expect.any(Object),
    ));
  });

  it("disables discard until attachment staging finishes, then cleans the staged path", async () => {
    const pendingStage = deferred<string>();
    vi.mocked(stageComposeAttachment).mockReturnValue(pendingStage.promise);
    mocks.showComposeLeaveConfirm = true;
    render(<ComposeView />);

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("slow-discard.pdf")] } });
    await waitFor(() => expect(stageComposeAttachment)
      .toHaveBeenCalledWith("slow-discard.pdf", expect.any(Array)));

    const discardButton = screen.getByRole("button", { name: "Discard" }) as HTMLButtonElement;
    expect(discardButton.disabled).toBe(true);
    await act(async () => pendingStage.resolve("staged/slow-discard.pdf"));
    await waitFor(() => expect(discardButton.disabled).toBe(false));
    fireEvent.click(discardButton);

    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/slow-discard.pdf"));
    await waitFor(() => expect(mocks.confirmCloseCompose).toHaveBeenCalledOnce());
  });

  it("cleans an attachment that finishes staging after the compose view unmounts", async () => {
    const pendingStage = deferred<string>();
    vi.mocked(stageComposeAttachment).mockReturnValue(pendingStage.promise);
    const { unmount } = render(<ComposeView />);

    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("late.pdf")] } });
    await waitFor(() => expect(stageComposeAttachment)
      .toHaveBeenCalledWith("late.pdf", expect.any(Array)));
    unmount();

    await act(async () => pendingStage.resolve("staged/late.pdf"));
    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/late.pdf"));
  });

  it("deletes a staged file when the attachment is removed", async () => {
    render(<ComposeView />);
    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("remove-me.pdf")] } });
    await waitFor(() => expect(screen.getByText("remove-me.pdf")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: /Remove attachment/ }));

    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/remove-me.pdf"));
    expect(screen.queryByText("remove-me.pdf")).toBeNull();
  });

  it("deletes autosaved drafts and staged files before confirming discard", async () => {
    const { rerender } = render(<ComposeView />);
    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("discard-me.pdf")] } });
    await waitFor(() => expect(screen.getByText("discard-me.pdf")).toBeTruthy());

    mocks.showComposeLeaveConfirm = true;
    rerender(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));

    await waitFor(() => expect(mocks.cancelPendingDraftSaves).toHaveBeenCalledOnce());
    await waitFor(() => expect(deleteDraft).toHaveBeenCalledWith("account-1", "draft-1"));
    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/discard-me.pdf"));
    await waitFor(() => expect(mocks.confirmCloseCompose).toHaveBeenCalledOnce());
  });

  it("cleans restored compose-staging attachments when discarding a legacy draft", async () => {
    mocks.loadDraftFromStorage.mockReturnValue({
      accountId: "account-1",
      to: ["to@example.com"],
      cc: [],
      bcc: [],
      subject: "Restored",
      rawSource: "",
      richTextHtml: "<p>Restored</p>",
      editorMode: "rich",
      attachments: [{ name: "restored.pdf", path: "staged/restored.pdf", size: 42 }],
      savedAt: Date.now(),
    });
    mocks.showComposeLeaveConfirm = true;
    render(<ComposeView />);

    expect(screen.getByText("restored.pdf")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));

    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/restored.pdf"));
  });

  it("does not delete a restored attachment source on ordinary unmount", async () => {
    mocks.loadDraftFromStorage.mockReturnValue({
      accountId: "account-1",
      to: ["to@example.com"],
      cc: [],
      bcc: [],
      subject: "Restored",
      rawSource: "",
      richTextHtml: "<p>Restored</p>",
      editorMode: "rich",
      attachments: [{ name: "restored.pdf", path: "staged/restored.pdf", size: 42 }],
      savedAt: Date.now(),
    });
    const { unmount } = render(<ComposeView />);

    unmount();
    await act(async () => Promise.resolve());

    expect(cleanupStagedComposeAttachment).not.toHaveBeenCalled();
  });

  it("waits for autosave and draft deletion before cleaning staged files", async () => {
    const pendingCancellation = deferred<Record<string, string>>();
    const pendingDeletion = deferred<void>();
    mocks.cancelPendingDraftSaves.mockReturnValue(pendingCancellation.promise);
    vi.mocked(deleteDraft).mockReturnValue(pendingDeletion.promise);

    const { rerender } = render(<ComposeView />);
    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("in-flight.pdf")] } });
    await waitFor(() => expect(screen.getByText("in-flight.pdf")).toBeTruthy());

    mocks.showComposeLeaveConfirm = true;
    rerender(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));

    expect(cleanupStagedComposeAttachment).not.toHaveBeenCalled();
    await act(async () => pendingCancellation.resolve({ "account-1": "draft-1" }));
    await waitFor(() => expect(deleteDraft).toHaveBeenCalledWith("account-1", "draft-1"));
    expect(cleanupStagedComposeAttachment).not.toHaveBeenCalled();

    await act(async () => pendingDeletion.resolve());
    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/in-flight.pdf"));
  });

  it("traps focus in the discard dialog and restores the opener after Escape", () => {
    const { rerender } = render(<ComposeView />);
    const backButton = screen.getByRole("button", { name: "Back" });
    backButton.focus();

    mocks.showComposeLeaveConfirm = true;
    rerender(<ComposeView />);

    const keepEditingButton = screen.getByRole("button", { name: "Keep editing" });
    const discardButton = screen.getByRole("button", { name: "Discard" });
    expect(document.activeElement).toBe(keepEditingButton);

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(discardButton);
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(keepEditingButton);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(mocks.cancelCloseCompose).toHaveBeenCalledOnce();
    mocks.showComposeLeaveConfirm = false;
    rerender(<ComposeView />);
    expect(document.activeElement).toBe(backButton);
  });

  it("ignores repeated discard actions while cleanup is pending", async () => {
    const pendingCancellation = deferred<Record<string, string>>();
    mocks.cancelPendingDraftSaves.mockReturnValue(pendingCancellation.promise);
    mocks.showComposeLeaveConfirm = true;
    render(<ComposeView />);

    const discardButton = screen.getByRole("button", { name: "Discard" });
    fireEvent.click(discardButton);
    fireEvent.click(discardButton);

    expect(mocks.cancelPendingDraftSaves).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(mocks.cancelCloseCompose).not.toHaveBeenCalled();

    await act(async () => {
      pendingCancellation.resolve({ "account-1": "draft-1" });
      await pendingCancellation.promise;
    });
    await waitFor(() => expect(mocks.confirmCloseCompose).toHaveBeenCalledOnce());
  });

  it("rejects an oversized attachment before reading it", async () => {
    render(<ComposeView />);
    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    const file = attachmentFile("too-large.bin");
    Object.defineProperty(file, "size", { value: 10 * 1024 * 1024 + 1 });
    fireEvent.change(fileInput!, { target: { files: [file] } });
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(stageComposeAttachment).not.toHaveBeenCalled();
  });

  it("saves and closes without deleting the persisted draft", async () => {
    mocks.saveNow.mockResolvedValue(true);
    render(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
    await waitFor(() => expect(mocks.closeCompose).toHaveBeenCalled());
    expect(deleteDraft).not.toHaveBeenCalled();
  });

  it("retains the compose when save or deletion fails", async () => {
    mocks.saveNow.mockResolvedValue(false);
    mocks.showComposeLeaveConfirm = true;
    vi.mocked(deleteDraft).mockRejectedValueOnce(new Error("disk failure"));
    render(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Save and close" }));
    await waitFor(() => expect(mocks.saveNow).toHaveBeenCalled());
    expect(mocks.closeCompose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(mocks.resumeDraftSaves).toHaveBeenCalled());
    expect(mocks.confirmCloseCompose).not.toHaveBeenCalled();
    expect(cleanupStagedComposeAttachment).not.toHaveBeenCalled();
  });

  it("announces queue acceptance rather than remote delivery and includes a stable request key", async () => {
    mocks.mutate.mockImplementation((_params, options) => options.onSuccess());
    render(<ComposeView />);
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.addToast).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("does not mean sent yet") })));
    expect(mocks.mutate).toHaveBeenCalledWith(expect.objectContaining({ clientKey: expect.any(String) }), expect.any(Object));
  });

  it("cleans staged files after a successful send", async () => {
    mocks.mutate.mockImplementation((_params, options) => options.onSuccess());
    render(<ComposeView />);
    const fileInput = document.querySelector<HTMLInputElement>('input[type="file"]');
    fireEvent.change(fileInput!, { target: { files: [attachmentFile("sent.pdf")] } });
    await waitFor(() => expect(screen.getByText("sent.pdf")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(cleanupStagedComposeAttachment)
      .toHaveBeenCalledWith("staged/sent.pdf"));
  });
});
