import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AiSummaryCard from "../../../src/features/ai/AiSummaryCard";
import { profileLocalStorage } from "../../../src/lib/profileStorage";

const mocks = vi.hoisted(() => {
  let streamListener: ((ev: { requestId: string; kind: string; delta: string; done: boolean }) => void) | null = null;
  return {
    getConfig: vi.fn(),
    summarizeMessage: vi.fn(),
    summarizeThread: vi.fn(),
    cancel: vi.fn(),
    onStream: vi.fn((cb: typeof streamListener) => {
      streamListener = cb;
      return () => { streamListener = null; };
    }),
    emitStream: (ev: { requestId: string; kind: string; delta: string; done: boolean }) => streamListener?.(ev),
  };
});

vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({
    t: (key: string, fallbackOrOpts?: string | Record<string, unknown>) =>
      typeof fallbackOrOpts === "string" ? fallbackOrOpts : key,
  }),
}));

vi.mock("@/mygo", () => ({
  AIService: {
    getConfig: mocks.getConfig,
    summarizeMessage: mocks.summarizeMessage,
    summarizeThread: mocks.summarizeThread,
    cancel: mocks.cancel,
  },
  events: { aiStream: { on: mocks.onStream } },
}));

const ENABLED_CONFIG = {
  configured: true,
  enabled: true,
  provider: "openai_compatible",
  preset: "openai",
  endpoint: "https://api.openai.com/v1",
  endpointHost: "api.openai.com",
  isLocalEndpoint: false,
  hasApiKey: true,
  maskedApiKey: "••••1234",
  model: "gpt-4o-mini",
  apiMode: "completions",
  proxyMode: "inherit",
  allowInsecureHttp: false,
  summaryLanguage: "",
  summaryStyle: "standard",
  summaryModel: "",
  translateLanguage: "",
  translateDisplayMode: "bilingual",
  translateModel: "",
  keyringError: false,
  summaryCacheCount: 0,
  translateCacheCount: 0,
};

const PRIVACY_ACK_KEY = "gomail-ai-privacy-ack";

describe("AiSummaryCard", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getConfig.mockResolvedValue(ENABLED_CONFIG);
    mocks.cancel.mockResolvedValue({});
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it("shows settings CTA instead of failing when AI is unconfigured", async () => {
    mocks.getConfig.mockResolvedValue({ ...ENABLED_CONFIG, configured: false, enabled: false });
    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);

    expect(await screen.findByText("ai.unconfiguredTitle")).toBeTruthy();
    expect(screen.getByRole("button", { name: /ai\.openSettings/ })).toBeTruthy();
    expect(mocks.summarizeMessage).not.toHaveBeenCalled();
  });

  it("keeps the email on the device until the user accepts the privacy notice for remote endpoints", async () => {
    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);

    expect(await screen.findByText("ai.privacyNoticeHost")).toBeTruthy();
    expect(mocks.summarizeMessage).not.toHaveBeenCalled();

    mocks.summarizeMessage.mockResolvedValue({
      tldr: "Overview",
      key_points: ["first point"],
      action_items: [],
      raw_text: "Overview\n\n- first point",
      model: "gpt-4o-mini",
      target_lang: "zh",
      cached: false,
      truncated: false,
      generated_at: 1,
    });
    fireEvent.click(screen.getByRole("button", { name: "translate.acceptAndContinue" }));

    await waitFor(() => {
      expect(mocks.summarizeMessage).toHaveBeenCalledWith(
        expect.objectContaining({ messageId: "message-1", forceRefresh: false }),
      );
    });
  });

  it("skips external privacy warning automatically for local loopback endpoints", async () => {
    mocks.getConfig.mockResolvedValue({
      ...ENABLED_CONFIG,
      endpoint: "http://127.0.0.1:11434/v1",
      endpointHost: "127.0.0.1:11434",
      isLocalEndpoint: true,
    });
    mocks.summarizeMessage.mockResolvedValue({
      tldr: "Local overview",
      key_points: [],
      action_items: [],
      raw_text: "Local overview",
      model: "qwen2.5",
      target_lang: "zh",
      cached: false,
      truncated: false,
      generated_at: 1,
    });

    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);
    expect(await screen.findByText("Local overview")).toBeTruthy();
  });

  it("renders structured summary with action items, cached badge, and copies formatted text", async () => {
    profileLocalStorage.setItem(PRIVACY_ACK_KEY, "1");
    mocks.summarizeMessage.mockResolvedValue({
      tldr: "Budget review ready.",
      key_points: ["Revenue grew 18%", "Anna owns the numbers"],
      action_items: [{ text: "Approve sheet", due: "by Friday" }],
      raw_text: "Budget review ready.",
      model: "gpt-4o-mini",
      target_lang: "zh",
      cached: true,
      truncated: false,
      generated_at: 1,
    });

    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);

    expect(await screen.findByText("Budget review ready.")).toBeTruthy();
    expect(screen.getByText("Anna owns the numbers")).toBeTruthy();
    expect(screen.getByText("by Friday")).toBeTruthy();
    expect(screen.getByText("ai.cachedBadge")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /ai\.copy/ }));
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expect.stringContaining("Approve sheet (by Friday)"));
  });

  it("streams incremental chunks and cancels in-flight request on stop", async () => {
    profileLocalStorage.setItem(PRIVACY_ACK_KEY, "1");
    let capturedReqId = "";
    mocks.summarizeMessage.mockImplementation(
      (req: { requestId: string }) => {
        capturedReqId = req.requestId;
        return new Promise(() => {});
      },
    );

    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);
    await waitFor(() => expect(capturedReqId).not.toBe(""));

    act(() => {
      mocks.emitStream({ requestId: capturedReqId, kind: "summary", delta: "Streaming text...", done: false });
    });
    expect(screen.getByText("Streaming text...")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "ai.stop" }));
    expect(mocks.cancel).toHaveBeenCalledWith({ requestId: capturedReqId });
  });

  it("summarizes a full thread when threadId is provided", async () => {
    profileLocalStorage.setItem(PRIVACY_ACK_KEY, "1");
    mocks.summarizeThread.mockResolvedValue({
      tldr: "Thread aligned on Friday launch.",
      key_points: ["QA signed off"],
      action_items: [],
      raw_text: "Thread aligned on Friday launch.",
      model: "gpt-4o-mini",
      target_lang: "zh",
      cached: false,
      truncated: false,
      generated_at: 1,
    });

    render(<AiSummaryCard threadId="thread:abc" onClose={vi.fn()} />);
    expect(await screen.findByText("Thread aligned on Friday launch.")).toBeTruthy();
    expect(mocks.summarizeThread).toHaveBeenCalledWith(expect.objectContaining({ threadId: "thread:abc" }));
  });

  it("reports a failure without closing the card, retries, and re-requests on language change", async () => {
    profileLocalStorage.setItem(PRIVACY_ACK_KEY, "1");
    mocks.summarizeMessage.mockRejectedValueOnce(new Error("AI service error 401"));

    render(<AiSummaryCard messageId="message-1" onClose={vi.fn()} />);
    expect(await screen.findByText("ai.summaryFailed")).toBeTruthy();
    expect(screen.getByText("AI service error 401")).toBeTruthy();

    mocks.summarizeMessage.mockResolvedValue({
      tldr: "Recovered",
      key_points: [],
      action_items: [],
      raw_text: "Recovered",
      model: "gpt-4o-mini",
      target_lang: "zh",
      cached: false,
      truncated: false,
      generated_at: 1,
    });
    fireEvent.click(screen.getByRole("button", { name: "ai.regenerate" }));
    expect(await screen.findByText("Recovered")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("ai.summaryLanguage"), { target: { value: "fr" } });
    await waitFor(() => {
      expect(mocks.summarizeMessage).toHaveBeenLastCalledWith(
        expect.objectContaining({ messageId: "message-1", targetLang: "fr" }),
      );
    });
  });

  it("closes from the close button", async () => {
    const onClose = vi.fn();
    profileLocalStorage.setItem(PRIVACY_ACK_KEY, "1");
    mocks.summarizeMessage.mockResolvedValue({
      tldr: "Short",
      key_points: [],
      action_items: [],
      raw_text: "Short",
      model: "gpt-4o-mini",
      target_lang: "zh",
      cached: false,
      truncated: false,
      generated_at: 1,
    });

    render(<AiSummaryCard messageId="message-1" onClose={onClose} />);
    await screen.findByText("Short");
    fireEvent.click(screen.getByRole("button", { name: "ai.close" }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
