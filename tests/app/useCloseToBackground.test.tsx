import { StrictMode } from "react";
import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  call: vi.fn().mockResolvedValue({}),
  uiState: { keepRunningInBackground: false },
}));

// Keep the real window adapter AND generated service; mock only native transport.
vi.mock("mygo-runtime", () => ({ call: mocks.call, currentWindow: {}, event: vi.fn() }));
vi.mock("@/stores/ui.store", () => ({
  useUIStore: (selector: (state: typeof mocks.uiState) => unknown) => selector(mocks.uiState),
}));

import { useCloseToBackground } from "@/app/useCloseToBackground";
import { initializeProfileStorageNamespace, resetProfileStorageNamespaceForTests } from "@/lib/profileStorage";
import { getCurrentWindow } from "@/lib/platform/window";
import { useToastStore } from "@/stores/toast.store";

function Harness() { useCloseToBackground(); return null; }

describe("native close preference adapter", () => {
  beforeEach(() => {
    mocks.call.mockReset().mockResolvedValue({});
    mocks.uiState.keepRunningInBackground = false;
    resetProfileStorageNamespaceForTests();
    localStorage.clear();
    useToastStore.setState({ toasts: [] });
  });

  it("does not send an unscoped preference before profile readiness", async () => {
    render(<Harness />);
    await act(async () => {});
    expect(mocks.call).not.toHaveBeenCalled();
  });

  it("sends the hydrated close preference once through the generated service, including StrictMode", async () => {
    await initializeProfileStorageNamespace(async () => "synthetic-profile");
    const view = render(<StrictMode><Harness /></StrictMode>);
    await waitFor(() => expect(mocks.call).toHaveBeenCalledExactlyOnceWith("AppService.ClosePreference", {
      profileNamespace: "synthetic-profile", keepRunningInBackground: false,
    }));
    mocks.uiState.keepRunningInBackground = true;
    view.rerender(<StrictMode><Harness /></StrictMode>);
    await waitFor(() => expect(mocks.call).toHaveBeenLastCalledWith("AppService.ClosePreference", {
      profileNamespace: "synthetic-profile", keepRunningInBackground: true,
    }));
    expect(mocks.call).toHaveBeenCalledTimes(2);
  });

  it("serializes updates so slow old calls cannot overwrite new preferences", async () => {
    await initializeProfileStorageNamespace(async () => "synthetic-profile");
    let finish!: (value: object) => void;
    mocks.call.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const view = render(<Harness />);
    await waitFor(() => expect(mocks.call).toHaveBeenCalledTimes(1));
    mocks.uiState.keepRunningInBackground = true;
    view.rerender(<Harness />);
    await act(async () => {});
    expect(mocks.call).toHaveBeenCalledTimes(1);
    await act(async () => finish({}));
    expect(mocks.call).toHaveBeenLastCalledWith("AppService.ClosePreference", {
      profileNamespace: "synthetic-profile", keepRunningInBackground: true,
    });
  });

  it("shows only a static error if preference sync fails", async () => {
    await initializeProfileStorageNamespace(async () => "synthetic-profile");
    mocks.call.mockRejectedValueOnce(new Error("private transport diagnostic"));
    render(<Harness />);
    await waitFor(() => expect(useToastStore.getState().toasts).toHaveLength(1));
    expect(useToastStore.getState().toasts[0].message).toContain("Could not apply");
    expect(useToastStore.getState().toasts[0].message).not.toContain("private");
  });

  it("routes close/restore to real Go window actions without JS close/focus shims or sync restarts", async () => {
    const window = getCurrentWindow();
    expect(window).not.toHaveProperty("onCloseRequested");
    expect(window).not.toHaveProperty("onFocusChanged");
    await window.close();
    await window.restore();
    expect(mocks.call.mock.calls).toEqual([
      ["AppService.WindowAction", { action: "close", width: 0, height: 0, x: 0, y: 0 }],
      ["AppService.WindowAction", { action: "restore", width: 0, height: 0, x: 0, y: 0 }],
    ]);
  });
});
