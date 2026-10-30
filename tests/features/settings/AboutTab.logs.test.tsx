import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AboutTab from "@/features/settings/AboutTab";
import ToastContainer from "@/components/ToastContainer";
import { useToastStore } from "@/stores/toast.store";

const mocks = vi.hoisted(() => ({ call: vi.fn() }));
vi.mock("mygo-runtime", () => ({ call: mocks.call, event: vi.fn() }));
vi.mock("@tauri-apps/api/app", () => ({ getVersion: vi.fn().mockResolvedValue("1.2.3") }));
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (_key: string, fallback?: string) => fallback ?? _key }),
}));

describe("AboutTab real platform adapter", () => {
  beforeEach(() => {
    mocks.call.mockReset().mockResolvedValue({});
    useToastStore.setState({ toasts: [] });
  });

  it("disables unavailable diagnostic logs and update checks rather than faking successful data", async () => {
    render(<AboutTab />);
    const logs = screen.getByRole("button", { name: "Open diagnostic log" }) as HTMLButtonElement;
    const updates = screen.getByRole("button", { name: "Check for updates" }) as HTMLButtonElement;
    expect(logs.disabled).toBe(true);
    expect(updates.disabled).toBe(true);
    expect(updates.title).toContain("not available");
    for (let i = 0; i < 5; i++) fireEvent.click(logs);
    fireEvent.click(updates);
    await waitFor(() => expect(screen.getByText(/Version.*1.2.3/)).toBeTruthy());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.call).not.toHaveBeenCalled();
    expect(screen.queryByText(/built with Rust/)).toBeNull();
  });

  it("opens external links through the generated service exactly once and surfaces static failure", async () => {
    mocks.call.mockRejectedValueOnce(new Error("OS error includes private URL"));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      render(<><AboutTab /><ToastContainer /></>);
      fireEvent.click(screen.getByRole("link", { name: "Source Code" }));
      const alert = await screen.findByRole("alert");
      expect(alert.textContent).toContain("Could not open this link");
      expect(alert.textContent).not.toContain("private");
      expect(mocks.call).toHaveBeenCalledExactlyOnceWith("AppService.OpenExternal", { url: "https://github.com/QingJ01/Pebble" });
      expect(warn).not.toHaveBeenCalled();
    } finally { warn.mockRestore(); }
  });
});
