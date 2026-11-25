import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Message, RenderedHtml } from "../../src/lib/api";
import { useBilingualTranslation } from "../../src/hooks/useBilingualTranslation";
import { useUIStore } from "../../src/stores/ui.store";
import { profileLocalStorage } from "../../src/lib/profileStorage";

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getTranslationCache: vi.fn(),
  saveTranslationCache: vi.fn(),
  translateBatch: vi.fn(),
  cancel: vi.fn(),
}));

vi.mock("@/mygo", () => ({
  AIService: {
    getConfig: mocks.getConfig,
    getTranslationCache: mocks.getTranslationCache,
    saveTranslationCache: mocks.saveTranslationCache,
    translateBatch: mocks.translateBatch,
    cancel: mocks.cancel,
  },
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

const LONG_ENGLISH = "The report is attached, please review it before Friday.";

function makeMessage(overrides: Partial<Message> = {}): Message {
  return {
    id: "message-1",
    account_id: "account-1",
    subject: "Weekly Report",
    body_text: "Body",
    body_html_raw: "<p>Body</p>",
    ...overrides,
  } as Message;
}

function makeRendered(html: string): RenderedHtml {
  return { html, images_blocked: 0, trackers_blocked: [] };
}

async function toggle(hook: { current: ReturnType<typeof useBilingualTranslation> }) {
  await act(async () => {
    await hook.current.handleBilingualToggle();
  });
}

describe("useBilingualTranslation (block-level AI translation)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    profileLocalStorage.setItem("gomail-ai-privacy-ack", "1");
    useUIStore.setState({ language: "zh" });
    mocks.getConfig.mockResolvedValue(ENABLED_CONFIG);
    mocks.getTranslationCache.mockResolvedValue({ found: false, subject: "", segments: {}, model: "", generatedAt: 0 });
    mocks.saveTranslationCache.mockResolvedValue({});
    mocks.cancel.mockResolvedValue({});
  });

  it("translates both subject and plain-text paragraphs into the interface language and caches them", async () => {
    mocks.translateBatch.mockResolvedValue({
      items: [
        { id: "subject", text: "每周报告" },
        { id: "0", text: "报告已随附，请在周五前查阅。" },
      ],
      translated: 2,
      total: 2,
      model: "gpt-4o-mini",
    });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-plain", null, makeMessage({ id: "msg-plain", body_text: LONG_ENGLISH })),
    );

    await toggle(result);

    expect(mocks.translateBatch).toHaveBeenCalledWith(
      expect.objectContaining({
        fromLang: "auto",
        toLang: "zh",
        items: [
          { id: "subject", text: "Weekly Report" },
          { id: "0", text: LONG_ENGLISH },
        ],
      }),
    );
    expect(result.current.bilingualResult?.translatedSubject).toBe("每周报告");
    expect(result.current.bilingualResult?.segments).toEqual([
      { source: LONG_ENGLISH, target: "报告已随附，请在周五前查阅。" },
    ]);
    expect(mocks.saveTranslationCache).toHaveBeenCalledOnce();
  });

  it("preserves inline HTML tags like <strong> and <a> intact while injecting one block translation", async () => {
    const paragraph =
      'We noticed you have not viewed the report <a href="https://example.com">here</a> by <strong>Skye Jia</strong>.';
    mocks.translateBatch.mockResolvedValue({
      items: [
        { id: "subject", text: "每周报告" },
        { id: "0", text: "我们注意到你尚未查看 Skye Jia 在此处分享的报告。" },
      ],
      translated: 2,
      total: 2,
      model: "gpt-4o-mini",
    });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-html", makeRendered(`<p>${paragraph}</p>`), makeMessage({ id: "msg-html" })),
    );

    await toggle(result);

    const html = result.current.bilingualResult?.translated ?? "";
    expect(html).toContain("<strong>Skye Jia</strong>");
    expect(html).toContain("https://example.com");
    expect(html).toContain("gomail-bilingual-translation");
    expect(html).toContain("gomail-translated-block");
    expect(html).toContain("我们注意到你尚未查看 Skye Jia 在此处分享的报告。");
    expect(html.match(/gomail-bilingual-translation/g)).toHaveLength(1);
  });

  it("serves cached translations immediately without invoking the AI provider", async () => {
    mocks.getTranslationCache.mockResolvedValue({
      found: true,
      subject: "缓存主题",
      segments: { "0": "缓存译文" },
      model: "gpt-4o-mini",
      generatedAt: 100,
    });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-cached", makeRendered(`<p>${LONG_ENGLISH}</p>`), makeMessage({ id: "msg-cached" })),
    );

    await toggle(result);

    expect(mocks.translateBatch).not.toHaveBeenCalled();
    expect(result.current.bilingualResult?.cached).toBe(true);
    expect(result.current.bilingualResult?.translatedSubject).toBe("缓存主题");
    expect(result.current.bilingualResult?.translated).toContain("缓存译文");
  });

  it("surfaces unconfigured AI settings without sending any translation request", async () => {
    mocks.getConfig.mockResolvedValue({ ...ENABLED_CONFIG, configured: false, enabled: false });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-unconf", makeRendered(`<p>${LONG_ENGLISH}</p>`), makeMessage({ id: "msg-unconf" })),
    );

    await toggle(result);

    expect(result.current.configIssue).toBe("unconfigured");
    expect(mocks.translateBatch).not.toHaveBeenCalled();
  });

  it("waits for remote privacy acceptance before translating", async () => {
    localStorage.clear();
    mocks.translateBatch.mockResolvedValue({
      items: [{ id: "0", text: "译文" }],
      translated: 1,
      total: 1,
      model: "gpt-4o-mini",
    });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-priv", makeRendered(`<p>${LONG_ENGLISH}</p>`), makeMessage({ id: "msg-priv", subject: "" })),
    );

    await toggle(result);
    expect(result.current.needsPrivacyAck).toBe(true);
    expect(mocks.translateBatch).not.toHaveBeenCalled();

    await act(async () => {
      result.current.acceptPrivacyAndTranslate();
    });
    expect(mocks.translateBatch).toHaveBeenCalledOnce();
  });

  it("reports unchanged and partial translations accurately", async () => {
    mocks.translateBatch.mockResolvedValueOnce({
      items: [{ id: "0", text: LONG_ENGLISH }],
      translated: 1,
      total: 1,
      model: "gpt-4o-mini",
    });

    const { result } = renderHook(() =>
      useBilingualTranslation("msg-same", makeRendered(`<p>${LONG_ENGLISH}</p>`), makeMessage({ id: "msg-same", subject: "" })),
    );

    await toggle(result);
    expect(result.current.bilingualError?.code).toBe("unchanged");
  });
});
