import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TranslatePopover from "../../../src/features/translate/TranslatePopover";
import { profileLocalStorage } from "../../../src/lib/profileStorage";
import { useUIStore } from "../../../src/stores/ui.store";

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  translateText: vi.fn(),
  cancel: vi.fn(),
}));

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
    translateText: mocks.translateText,
    cancel: mocks.cancel,
  },
}));

const ENABLED_CONFIG = {
  configured: true,
  enabled: true,
  endpointHost: "api.deepseek.com",
  isLocalEndpoint: false,
  keyringError: false,
  translateLanguage: "",
};

describe("TranslatePopover", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    useUIStore.setState({ language: "zh" });
    mocks.getConfig.mockResolvedValue(ENABLED_CONFIG);
    mocks.cancel.mockResolvedValue({});
  });

  it("translates selected text with autoFlip and displays flipped hint when language switched", async () => {
    profileLocalStorage.setItem("gomail-ai-privacy-ack", "1");
    mocks.translateText.mockResolvedValue({
      translated: "Hello world",
      targetLang: "en",
      model: "deepseek-chat",
    });

    render(<TranslatePopover text="你好世界" position={{ x: 100, y: 100 }} onClose={vi.fn()} />);

    expect(await screen.findByText("Hello world")).toBeTruthy();
    expect(mocks.translateText).toHaveBeenCalledWith(
      expect.objectContaining({ text: "你好世界", autoFlip: true }),
    );
    expect(screen.getByText("ai.autoFlippedHint")).toBeTruthy();
  });

  it("shows settings button when AI is not configured and closes on Escape", async () => {
    const onClose = vi.fn();
    mocks.getConfig.mockResolvedValue({ ...ENABLED_CONFIG, configured: false, enabled: false });

    render(<TranslatePopover text="Hello" position={{ x: 50, y: 50 }} onClose={onClose} />);
    expect(await screen.findByText("ai.unconfiguredTitle")).toBeTruthy();
    expect(mocks.translateText).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });
});
