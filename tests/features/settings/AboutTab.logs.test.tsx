import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AboutTab from "@/features/settings/AboutTab";
import ToastContainer from "@/components/ToastContainer";
import { useToastStore } from "@/stores/toast.store";
import { useUpdateStore } from "@/stores/update.store";

const mocks = vi.hoisted(() => ({ call: vi.fn() }));
vi.mock("mygo-runtime", () => ({ call: mocks.call, event: vi.fn() }));
vi.mock("@tauri-apps/api/app", () => ({ getVersion: vi.fn().mockResolvedValue("1.2.3") }));
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (_key: string, fallback?: string, vars?: Record<string, string | number>) => {
    let text = fallback ?? _key;
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        text = text.replaceAll(`{{${k}}}`, String(v));
      }
    }
    return text;
  } }),
}));

describe("AboutTab real platform adapter", () => {
  beforeEach(() => {
    mocks.call.mockReset().mockResolvedValue({
      currentVersion: "1.2.3",
      canAutoUpdate: true,
      state: "idle",
    });
    useToastStore.setState({ toasts: [] });
    useUpdateStore.setState({
      currentVersion: "",
      canAutoUpdate: false,
      state: "idle",
      availableVersion: null,
      releaseNotes: null,
      releaseDate: null,
      releaseUrl: null,
      installedVersion: null,
      downloaded: 0,
      total: 0,
      percent: 0,
      error: null,
      autoCheckUpdates: true,
      skippedVersion: null,
      dismissedVersion: null,
      lastCheckedAt: null,
      lastCheckWasManual: false,
    });
  });

  it("keeps diagnostic logs disabled while wiring real update checks through UpdateService", async () => {
    mocks.call.mockImplementation(async (method: string) => {
      if (method === "UpdateService.Status") {
        return { currentVersion: "1.2.3", canAutoUpdate: true, state: "idle" };
      }
      if (method === "UpdateService.Check") {
        return {
          currentVersion: "1.2.3",
          canAutoUpdate: true,
          state: "available",
          availableVersion: "1.3.0",
          releaseNotes: "Signed delta update notes",
          releaseUrl: "https://github.com/duanyongcheng/pebble-go/releases/tag/v1.3.0",
        };
      }
      if (method === "UpdateService.Install") {
        return {
          currentVersion: "1.2.3",
          canAutoUpdate: true,
          state: "installed",
          installedVersion: "1.3.0",
          percent: 100,
        };
      }
      if (method === "UpdateService.Relaunch") {
        return {};
      }
      return {};
    });

    render(<AboutTab />);
    const logs = screen.getByRole("button", { name: "Open diagnostic log" }) as HTMLButtonElement;
    const updates = screen.getByRole("button", { name: "Check for updates" }) as HTMLButtonElement;
    expect(logs.disabled).toBe(true);
    expect(updates.disabled).toBe(false);
    for (let i = 0; i < 5; i++) fireEvent.click(logs);
    await waitFor(() => expect(screen.getByText(/Version.*1\.2\.3/)).toBeTruthy());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText(/built with Rust/)).toBeNull();

    fireEvent.click(updates);
    await waitFor(() => expect(screen.getByText("New version available: v1.3.0")).toBeTruthy());
    expect(screen.getByText("Signed delta update notes")).toBeTruthy();
    expect(screen.getByText("Incremental update")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Download & install" }));
    await waitFor(() => expect(screen.getByText("v1.3.0 is ready to install")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));
    await waitFor(() => expect(mocks.call).toHaveBeenCalledWith("UpdateService.Relaunch", {}));
  });

  it("shows download progress bar while an incremental update is downloading", async () => {
    mocks.call.mockResolvedValue({
      currentVersion: "1.2.3",
      canAutoUpdate: true,
      state: "downloading",
      availableVersion: "1.3.0",
      downloaded: 2 * 1024 * 1024,
      total: 4 * 1024 * 1024,
      percent: 50,
    });
    render(<AboutTab />);
    const progress = await screen.findByRole("progressbar");
    expect(progress.getAttribute("aria-valuenow")).toBe("50");
    expect(screen.getByText(/2\.0 MB \/ 4\.0 MB/)).toBeTruthy();
  });

  it("opens external links through the generated service exactly once and surfaces static failure", async () => {
    mocks.call.mockImplementation(async (method: string) => {
      if (method === "AppService.OpenExternal") {
        throw new Error("OS error includes private URL");
      }
      return { currentVersion: "1.2.3", canAutoUpdate: false, state: "idle" };
    });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      render(<><AboutTab /><ToastContainer /></>);
      expect(screen.getByRole("link", { name: "GitHub Releases" }).getAttribute("href")).toBe(
        "https://github.com/duanyongcheng/pebble-go/releases",
      );
      expect(screen.getByRole("link", { name: "Report an Issue" }).getAttribute("href")).toBe(
        "https://github.com/duanyongcheng/pebble-go/issues",
      );
      fireEvent.click(screen.getByRole("link", { name: "Source Code" }));
      const alert = await screen.findByRole("alert");
      expect(alert.textContent).toContain("Could not open this link");
      expect(alert.textContent).not.toContain("private");
      expect(mocks.call).toHaveBeenCalledWith("AppService.OpenExternal", {
        url: "https://github.com/duanyongcheng/pebble-go",
      });
      expect(warn).not.toHaveBeenCalled();
    } finally { warn.mockRestore(); }
  });
});
