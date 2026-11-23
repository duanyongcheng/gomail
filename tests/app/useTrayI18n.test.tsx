import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  language: "zh",
  setTrayMenuLabels: vi.fn().mockResolvedValue(undefined),
  translations: {
    "tray.show": "显示窗口",
    "tray.hide": "隐藏窗口",
    "tray.quit": "退出 GoMail",
  } as Record<string, string>,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, fallback: string) => mocks.translations[key] ?? fallback,
  }),
}));

vi.mock("../../src/stores/ui.store", () => ({
  useUIStore: (selector: (state: { language: string }) => unknown) =>
    selector({ language: mocks.language }),
}));

vi.mock("../../src/lib/api", () => ({
  setTrayMenuLabels: mocks.setTrayMenuLabels,
}));

import { useTrayI18n } from "../../src/app/useTrayI18n";

function Harness() {
  useTrayI18n();
  return null;
}

describe("useTrayI18n", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.language = "zh";
    mocks.translations = {
      "tray.show": "显示窗口",
      "tray.hide": "隐藏窗口",
      "tray.quit": "退出 GoMail",
    };
  });

  it("does not write localized labels to a startup stub", () => {
    render(<Harness />);

    expect(mocks.setTrayMenuLabels).not.toHaveBeenCalled();
  });

  it("does not claim tray support when the app language changes", () => {
    const view = render(<Harness />);

    expect(mocks.setTrayMenuLabels).not.toHaveBeenCalled();

    mocks.language = "en";
    mocks.translations = {
      "tray.show": "Show Window",
      "tray.hide": "Hide Window",
      "tray.quit": "Quit GoMail",
    };
    view.rerender(<Harness />);

    expect(mocks.setTrayMenuLabels).not.toHaveBeenCalled();
  });
});
