import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  addToast: vi.fn(),
  getConfig: vi.fn(),
  saveConfig: vi.fn(),
  clearConfig: vi.fn(),
  testConnection: vi.fn(),
  listModels: vi.fn(),
  clearCache: vi.fn(),
}));

vi.mock("react-i18next", () => ({
  initReactI18next: { type: "3rdParty", init: vi.fn() },
  useTranslation: () => ({
    t: (key: string, fallbackOrOpts?: string | Record<string, unknown>) =>
      typeof fallbackOrOpts === "string" ? fallbackOrOpts : key,
  }),
}));

vi.mock("@/stores/toast.store", () => {
  const useToastStore = (selector: (state: { addToast: typeof mocks.addToast }) => unknown) =>
    selector({ addToast: mocks.addToast });
  return {
    useToastStore: Object.assign(useToastStore, {
      getState: () => ({ addToast: mocks.addToast }),
    }),
  };
});

vi.mock("@/mygo", () => ({
  AIService: {
    getConfig: mocks.getConfig,
    saveConfig: mocks.saveConfig,
    clearConfig: mocks.clearConfig,
    testConnection: mocks.testConnection,
    listModels: mocks.listModels,
    clearCache: mocks.clearCache,
  },
}));

import AiTab from "../../../src/features/settings/AiTab";

const STORED_VIEW = {
  configured: true,
  enabled: true,
  provider: "openai_compatible",
  preset: "openai",
  endpoint: "https://api.example.com/v1",
  endpointHost: "api.example.com",
  isLocalEndpoint: false,
  hasApiKey: true,
  maskedApiKey: "••••4321",
  model: "gpt-4o",
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
  summaryCacheCount: 3,
  translateCacheCount: 2,
};

function modelField(): HTMLSelectElement {
  return screen.getByLabelText("translate.model") as HTMLSelectElement;
}

function modelOptionValues(): string[] {
  return Array.from(modelField().querySelectorAll("option")).map((option) => option.value);
}

describe("AiTab unified settings & model picker", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getConfig.mockResolvedValue(STORED_VIEW);
    mocks.saveConfig.mockImplementation(async (req) => ({ ...STORED_VIEW, ...req }));
    mocks.listModels.mockResolvedValue(["gpt-4o", "brand-new-model"]);
    mocks.testConnection.mockResolvedValue({ ok: true, latencyMs: 120, model: "gpt-4o", message: "gpt-4o (120 ms)" });
    mocks.clearCache.mockResolvedValue({ deleted: 5 });
  });

  it("loads the saved model into the picker without exposing plaintext API key", async () => {
    render(<AiTab />);

    await waitFor(() => expect(modelField().value).toBe("gpt-4o"));
    expect(modelField().tagName).toBe("SELECT");
    const keyInput = screen.getByLabelText("translate.apiKey") as HTMLInputElement;
    expect(keyInput.value).toBe("");
  });

  it("fetches models using the endpoint on screen and keeps existing saved key when blank", async () => {
    render(<AiTab />);
    await waitFor(() => expect(mocks.getConfig).toHaveBeenCalled());

    fireEvent.change(screen.getByLabelText("ai.endpoint"), {
      target: { value: "https://api.other.example.com/v1" },
    });
    fireEvent.click(screen.getByTestId("ai-model-fetch"));

    await waitFor(() => expect(mocks.listModels).toHaveBeenCalledTimes(1));
    expect(mocks.listModels).toHaveBeenCalledWith(
      expect.objectContaining({
        endpoint: "https://api.other.example.com/v1",
        provider: "openai_compatible",
        keepExistingKey: true,
      }),
    );
  });

  it("saves the model chosen from the fetched list and preserves saved key", async () => {
    render(<AiTab />);
    await waitFor(() => expect(mocks.getConfig).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("ai-model-fetch"));
    await waitFor(() => expect(modelOptionValues()).toContain("brand-new-model"));

    fireEvent.change(modelField(), { target: { value: "brand-new-model" } });
    fireEvent.click(screen.getByText("common.save"));

    await waitFor(() => expect(mocks.saveConfig).toHaveBeenCalledTimes(1));
    expect(mocks.saveConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        provider: "openai_compatible",
        model: "brand-new-model",
        keepExistingKey: true,
      }),
    );
  });

  it("applies Ollama local preset with direct proxy mode and clears local cache", async () => {
    render(<AiTab />);
    await waitFor(() => expect(mocks.getConfig).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "ai.presetOllama" }));
    expect((screen.getByLabelText("ai.endpoint") as HTMLInputElement).value).toBe("http://127.0.0.1:11434/v1");
    expect((screen.getByLabelText("ai.proxyMode") as HTMLSelectElement).value).toBe("disabled");
    expect(screen.getByText("ai.localDirectHint")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "ai.clearCache" }));
    await waitFor(() => expect(mocks.clearCache).toHaveBeenCalledOnce());
  });
});
