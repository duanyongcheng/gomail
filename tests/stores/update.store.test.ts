import { beforeEach, describe, expect, it, vi } from "vitest";
import { profileLocalStorage } from "@/lib/profileStorage";
import { useComposeStore } from "@/stores/compose.store";
import { useConfirmStore } from "@/stores/confirm.store";
import { useUIStore } from "@/stores/ui.store";
import {
  AUTO_CHECK_UPDATES_KEY,
  isUpdateReminderVisible,
  SKIPPED_UPDATE_VERSION_KEY,
  useUpdateStore,
} from "@/stores/update.store";

const mocks = vi.hoisted(() => ({ call: vi.fn() }));
vi.mock("mygo-runtime", () => ({ call: mocks.call, event: vi.fn() }));

describe("useUpdateStore", () => {
  beforeEach(() => {
    mocks.call.mockReset();
    localStorage.clear();
    useComposeStore.setState({ composeDirty: false });
    useUpdateStore.setState({
      currentVersion: "0.2.0",
      canAutoUpdate: true,
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

  it("hides background reminder for skipped or dismissed version but shows newer or installed versions", () => {
    expect(isUpdateReminderVisible({
      state: "available",
      availableVersion: "0.2.1",
      installedVersion: null,
      skippedVersion: null,
      dismissedVersion: null,
    })).toBe(true);

    expect(isUpdateReminderVisible({
      state: "available",
      availableVersion: "0.2.1",
      installedVersion: null,
      skippedVersion: "0.2.1",
      dismissedVersion: null,
    })).toBe(false);

    expect(isUpdateReminderVisible({
      state: "available",
      availableVersion: "0.2.2",
      installedVersion: null,
      skippedVersion: "0.2.1",
      dismissedVersion: null,
    })).toBe(true);

    expect(isUpdateReminderVisible({
      state: "installed",
      availableVersion: "0.2.1",
      installedVersion: "0.2.1",
      skippedVersion: "0.2.1",
      dismissedVersion: "0.2.1",
    })).toBe(true);
  });

  it("persists autoCheckUpdates and skipVersion per profile", () => {
    useUpdateStore.getState().setAutoCheckUpdates(false);
    expect(profileLocalStorage.getItem(AUTO_CHECK_UPDATES_KEY)).toBe("false");
    expect(useUpdateStore.getState().autoCheckUpdates).toBe(false);

    useUpdateStore.getState().skipVersion("0.2.1");
    expect(profileLocalStorage.getItem(SKIPPED_UPDATE_VERSION_KEY)).toBe("0.2.1");
    expect(useUpdateStore.getState().skippedVersion).toBe("0.2.1");
  });

  it("suppresses background check errors while surfacing manual check errors", async () => {
    mocks.call.mockRejectedValueOnce(new Error("offline"));
    await useUpdateStore.getState().checkForUpdates({ manual: false });
    expect(useUpdateStore.getState().state).toBe("idle");
    expect(useUpdateStore.getState().error).toBeNull();

    mocks.call.mockRejectedValueOnce(new Error("Could not check for updates."));
    await useUpdateStore.getState().checkForUpdates({ manual: true });
    expect(useUpdateStore.getState().state).toBe("error");
    expect(useUpdateStore.getState().error).toBe("Could not check for updates.");
  });

  it("updates progress cleanly including delta-to-full archive resets", () => {
    useUpdateStore.getState().applyProgress({
      phase: "downloading",
      version: "0.2.1",
      downloaded: 500,
      total: 1000,
      percent: 50,
    });
    expect(useUpdateStore.getState().percent).toBe(50);

    // Fallback to full archive resets downloaded/percent cleanly.
    useUpdateStore.getState().applyProgress({
      phase: "downloading",
      version: "0.2.1",
      downloaded: 100,
      total: 5000,
      percent: 2,
    });
    expect(useUpdateStore.getState().percent).toBe(2);
    expect(useUpdateStore.getState().total).toBe(5000);
  });

  it("asks for confirmation before relaunching when compose has unsaved changes", async () => {
    useUIStore.setState({ activeView: "compose" });
    useComposeStore.setState({ composeDirty: true });
    const confirmSpy = vi.spyOn(useConfirmStore.getState(), "confirm").mockResolvedValueOnce(false);
    await useUpdateStore.getState().relaunchToUpdate();
    expect(confirmSpy).toHaveBeenCalledOnce();
    expect(mocks.call).not.toHaveBeenCalledWith("UpdateService.Relaunch", {});

    confirmSpy.mockResolvedValueOnce(true);
    mocks.call.mockResolvedValueOnce({});
    await useUpdateStore.getState().relaunchToUpdate();
    expect(mocks.call).toHaveBeenCalledWith("UpdateService.Relaunch", {});
    confirmSpy.mockRestore();
    useUIStore.setState({ activeView: "inbox" });
  });
});
