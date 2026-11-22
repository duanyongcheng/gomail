import { create } from "zustand";
import i18next from "i18next";
import { profileLocalStorage } from "@/lib/profileStorage";
import { UpdateService, type UpdateProgress, type UpdateStatus } from "@/mygo";
import { isComposeDirty } from "./compose.store";
import { useConfirmStore } from "./confirm.store";

export const AUTO_CHECK_UPDATES_KEY = "pebble-auto-check-updates";
export const SKIPPED_UPDATE_VERSION_KEY = "pebble-skipped-update-version";
export const LAST_UPDATE_CHECK_AT_KEY = "pebble-last-update-check-at";
export const AUTO_UPDATE_CHECK_COOLDOWN_MS = 4 * 60 * 60 * 1000;

export type UpdateStateKind =
  | "idle"
  | "checking"
  | "available"
  | "up_to_date"
  | "downloading"
  | "installed"
  | "error";

export function readAutoCheckUpdatesPreference(): boolean {
  return profileLocalStorage.getItem(AUTO_CHECK_UPDATES_KEY) !== "false";
}

export function readSkippedUpdateVersion(): string | null {
  const stored = profileLocalStorage.getItem(SKIPPED_UPDATE_VERSION_KEY)?.trim();
  return stored ? stored : null;
}

export function readLastUpdateCheckAt(): number | null {
  const raw = profileLocalStorage.getItem(LAST_UPDATE_CHECK_AT_KEY);
  if (!raw) return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

export interface UpdateStoreState {
  currentVersion: string;
  canAutoUpdate: boolean;
  state: UpdateStateKind;
  availableVersion: string | null;
  releaseNotes: string | null;
  releaseDate: string | null;
  releaseUrl: string | null;
  installedVersion: string | null;
  downloaded: number;
  total: number;
  percent: number;
  error: string | null;
  autoCheckUpdates: boolean;
  skippedVersion: string | null;
  dismissedVersion: string | null;
  lastCheckedAt: number | null;
  lastCheckWasManual: boolean;

  hydrateStatus: () => Promise<void>;
  applyServerStatus: (status: UpdateStatus, options?: { manual?: boolean }) => void;
  applyProgress: (progress: UpdateProgress) => void;
  checkForUpdates: (options?: { manual?: boolean }) => Promise<void>;
  installUpdate: () => Promise<void>;
  relaunchToUpdate: () => Promise<void>;
  setAutoCheckUpdates: (enabled: boolean) => void;
  skipVersion: (version: string) => void;
  dismissReminder: () => void;
}

function normalizeState(raw: string | undefined): UpdateStateKind {
  switch (raw) {
    case "checking":
    case "available":
    case "up_to_date":
    case "downloading":
    case "installed":
    case "error":
      return raw;
    default:
      return "idle";
  }
}

export function isUpdateReminderVisible(
  s: Pick<UpdateStoreState, "state" | "availableVersion" | "installedVersion" | "skippedVersion" | "dismissedVersion">,
): boolean {
  if (s.state === "installed" && s.installedVersion) return true;
  if (s.state === "downloading") return true;
  if (s.state === "available" && s.availableVersion) {
    return s.availableVersion !== s.skippedVersion && s.availableVersion !== s.dismissedVersion;
  }
  return false;
}

export const useUpdateStore = create<UpdateStoreState>((set, get) => ({
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
  autoCheckUpdates: readAutoCheckUpdatesPreference(),
  skippedVersion: readSkippedUpdateVersion(),
  dismissedVersion: null,
  lastCheckedAt: readLastUpdateCheckAt(),
  lastCheckWasManual: false,

  hydrateStatus: async () => {
    try {
      const status = await UpdateService.status({});
      get().applyServerStatus(status);
    } catch {
      // Non-fatal during startup or in isolated frontend tests without a Go backend.
    }
  },

  applyServerStatus: (status, options) => {
    const prev = get();
    const nextState = normalizeState(status.state);
    const manual = options?.manual ?? prev.lastCheckWasManual;

    // Background check errors stay silent so offline/transient failures never nag.
    if (nextState === "error" && !manual && prev.state !== "downloading") {
      set({
        currentVersion: status.currentVersion || prev.currentVersion,
        canAutoUpdate: Boolean(status.canAutoUpdate),
        state: prev.availableVersion ? "available" : "idle",
        error: null,
      });
      return;
    }

    const availableVersion = status.availableVersion?.trim() || null;
    const installedVersion = status.installedVersion?.trim() || null;
    set({
      currentVersion: status.currentVersion || prev.currentVersion,
      canAutoUpdate: Boolean(status.canAutoUpdate),
      state: nextState,
      availableVersion,
      releaseNotes: status.releaseNotes?.trim() || null,
      releaseDate: status.releaseDate?.trim() || null,
      releaseUrl: status.releaseUrl?.trim() || null,
      installedVersion,
      downloaded: Number.isFinite(status.downloaded) ? (status.downloaded ?? 0) : prev.downloaded,
      total: Number.isFinite(status.total) ? (status.total ?? 0) : prev.total,
      percent: Number.isFinite(status.percent) ? (status.percent ?? 0) : prev.percent,
      error: status.error?.trim() || null,
    });
  },

  applyProgress: (progress) => {
    set((prev) => {
      const nextState: UpdateStateKind =
        progress.phase === "installed"
          ? "installed"
          : progress.phase === "error"
            ? "error"
            : "downloading";
      return {
        state: nextState,
        downloaded: progress.downloaded ?? prev.downloaded,
        total: progress.total ?? prev.total,
        percent: Math.max(0, Math.min(100, progress.percent ?? prev.percent)),
        installedVersion: progress.phase === "installed"
          ? (progress.version || prev.availableVersion || prev.installedVersion)
          : prev.installedVersion,
      };
    });
  },

  checkForUpdates: async (options) => {
    const manual = Boolean(options?.manual);
    const prev = get();
    if (prev.state === "downloading" || prev.state === "installed") {
      return;
    }
    set({
      lastCheckWasManual: manual,
      ...(manual ? { state: "checking", error: null, dismissedVersion: null } : {}),
    });

    try {
      const status = await UpdateService.check({});
      const now = Date.now();
      profileLocalStorage.setItem(LAST_UPDATE_CHECK_AT_KEY, String(now));
      set({ lastCheckedAt: now });
      get().applyServerStatus(status, { manual });
    } catch (err) {
      if (!manual) {
        return;
      }
      const message = err instanceof Error && err.message.trim()
        ? err.message
        : i18next.t("about.checkFailed", "Failed to check for updates");
      set({
        state: "error",
        error: message,
      });
    }
  },

  installUpdate: async () => {
    const prev = get();
    if (prev.state === "downloading" || prev.state === "installed") {
      return;
    }
    set({
      lastCheckWasManual: true,
      state: "downloading",
      downloaded: 0,
      total: 0,
      percent: 0,
      error: null,
    });

    try {
      const status = await UpdateService.install({});
      get().applyServerStatus(status, { manual: true });
    } catch (err) {
      const message = err instanceof Error && err.message.trim()
        ? err.message
        : i18next.t("about.installFailed", "Could not download or verify the update.");
      set({
        state: "error",
        error: message,
      });
    }
  },

  relaunchToUpdate: async () => {
    if (isComposeDirty()) {
      const confirmed = await useConfirmStore.getState().confirm({
        title: i18next.t("about.restartConfirmTitle", "Restart to apply update?"),
        message: i18next.t(
          "about.restartConfirmMessage",
          "You have unsaved edits in Compose. Restarting now will discard unsaved changes.",
        ),
        confirmLabel: i18next.t("about.restartNow", "Restart now"),
        destructive: true,
      });
      if (!confirmed) return;
    }
    try {
      await UpdateService.relaunch({});
    } catch (err) {
      const message = err instanceof Error && err.message.trim()
        ? err.message
        : i18next.t("about.restartFailed", "Could not restart automatically. Quit and reopen Pebble Go to finish updating.");
      set({ error: message });
    }
  },

  setAutoCheckUpdates: (enabled) => {
    profileLocalStorage.setItem(AUTO_CHECK_UPDATES_KEY, String(enabled));
    set({ autoCheckUpdates: enabled });
  },

  skipVersion: (version) => {
    const clean = version.trim();
    if (!clean) return;
    profileLocalStorage.setItem(SKIPPED_UPDATE_VERSION_KEY, clean);
    set({ skippedVersion: clean, dismissedVersion: clean });
  },

  dismissReminder: () => {
    const available = get().availableVersion;
    if (available) {
      set({ dismissedVersion: available });
    }
  },
}));
