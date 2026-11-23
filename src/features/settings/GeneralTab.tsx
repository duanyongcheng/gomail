import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { isCommandAvailable, isFeatureAvailable, unavailableReason } from "@/lib/availability";
import {
  showTestNotification,
  openDefaultMailSettings,
  getAutostartEnabled,
  setAutostartEnabled,
} from "@/lib/api";
import { useToastStore } from "@/stores/toast.store";
import { useUIStore, type RealtimePreference } from "@/stores/ui.store";

const REALTIME_OPTIONS: Array<{
  mode: RealtimePreference;
  labelKey: string;
  fallback: string;
  descriptionKey: string;
  descriptionFallback: string;
}> = [
  {
    mode: "realtime",
    labelKey: "settings.realtimeModeRealtime",
    fallback: "Realtime (recommended)",
    descriptionKey: "settings.realtimeModeRealtimeDesc",
    descriptionFallback: "Gmail checks about every 10 seconds while recently active, then every 30 seconds while idle.",
  },
  {
    mode: "balanced",
    labelKey: "settings.realtimeModeBalanced",
    fallback: "Balanced",
    descriptionKey: "settings.realtimeModeBalancedDesc",
    descriptionFallback: "Gmail checks about every 30 seconds in the foreground.",
  },
  {
    mode: "battery",
    labelKey: "settings.realtimeModeBattery",
    fallback: "Battery saver",
    descriptionKey: "settings.realtimeModeBatteryDesc",
    descriptionFallback: "Gmail checks about every 60 seconds in the foreground. Background checks slow to about every 2 minutes.",
  },
  {
    mode: "manual",
    labelKey: "settings.realtimeModeManual",
    fallback: "Manual only",
    descriptionKey: "settings.realtimeModeManualDesc",
    descriptionFallback: "Stops background checks. Use Sync now to run a single pass.",
  },
];

export default function GeneralTab() {
  const { t } = useTranslation();
  const addToast = useToastStore((s) => s.addToast);
  const [testingNotification, setTestingNotification] = useState(false);
  const realtimeMode = useUIStore((s) => s.realtimeMode);
  const setRealtimeMode = useUIStore((s) => s.setRealtimeMode);
  const notificationsEnabled = useUIStore((s) => s.notificationsEnabled);
  const setNotificationsEnabled = useUIStore((s) => s.setNotificationsEnabled);
  const keepRunningInBackground = useUIStore((s) => s.keepRunningInBackground);
  const setKeepRunningInBackground = useUIStore((s) => s.setKeepRunningInBackground);
  const startHiddenToTray = useUIStore((s) => s.startHiddenToTray);
  const setStartHiddenToTray = useUIStore((s) => s.setStartHiddenToTray);

  const toggleNotifications = useCallback(() => {
    if (!isFeatureAvailable("notifications")) return;
    setNotificationsEnabled(!notificationsEnabled);
  }, [notificationsEnabled, setNotificationsEnabled]);

  const handleTestNotification = useCallback(async () => {
    if (!isFeatureAvailable("notifications")) return;
    setTestingNotification(true);
    try {
      await showTestNotification();
      addToast({
        message: t("settings.testNotificationSent", "Test notification sent"),
        type: "success",
      });
    } catch {
      addToast({
        message: t("settings.testNotificationFailed", "Failed to send test notification"),
        type: "error",
      });
    } finally {
      setTestingNotification(false);
    }
  }, [addToast, t]);

  const quitOnClose = !keepRunningInBackground;
  const toggleQuitOnClose = useCallback(() => {
    setKeepRunningInBackground(quitOnClose);
  }, [quitOnClose, setKeepRunningInBackground]);

  const toggleStartHiddenToTray = useCallback(() => {
    if (!isFeatureAvailable("startHidden")) return;
    setStartHiddenToTray(!startHiddenToTray);
  }, [setStartHiddenToTray, startHiddenToTray]);

  // Launch-at-startup state lives in the OS (registry / LaunchAgent / .desktop),
  // not in app storage, so read the real value from the backend on mount.
  const [launchAtStartup, setLaunchAtStartup] = useState(false);

  useEffect(() => {
    if (!isFeatureAvailable("autostart")) return;
    let active = true;
    getAutostartEnabled()
      .then((enabled) => {
        if (active) setLaunchAtStartup(enabled);
      })
      .catch((err) => {
        console.warn("Failed to read launch-at-startup state", err);
      });
    return () => {
      active = false;
    };
  }, []);

  const toggleLaunchAtStartup = useCallback(async () => {
    if (!isFeatureAvailable("autostart")) return;
    const next = !launchAtStartup;
    try {
      await setAutostartEnabled(next);
      setLaunchAtStartup(next);
    } catch {
      setLaunchAtStartup(!next);
      addToast({
        message: t("settings.autostartFailed", "Failed to update launch at startup"),
        type: "error",
      });
    }
  }, [launchAtStartup, addToast, t]);

  const showUnreadCount = useUIStore((s) => s.showFolderUnreadCount);
  const setShowUnreadCount = useUIStore((s) => s.setShowFolderUnreadCount);
  const showSystemLabels = useUIStore((s) => s.showSystemLabels);
  const setShowSystemLabels = useUIStore((s) => s.setShowSystemLabels);

  const toggleUnreadCount = useCallback(() => {
    setShowUnreadCount(!showUnreadCount);
  }, [showUnreadCount, setShowUnreadCount]);

  const toggleSystemLabels = useCallback(() => {
    setShowSystemLabels(!showSystemLabels);
  }, [showSystemLabels, setShowSystemLabels]);

  return (
    <div>
      <h3 className="settings-section-title settings-section-title--first">
        {t("settings.realtimeMode", "Realtime Mode")}
      </h3>
      <p className="settings-section-desc">
        {t("settings.realtimeModeDesc", "Choose how aggressively GoMail checks for new mail.")}
      </p>
      <div
        role="group"
        aria-label={t("settings.realtimeMode", "Realtime Mode")}
        className="settings-choice-grid"
      >
        {REALTIME_OPTIONS.map((option) => {
          const selected = realtimeMode === option.mode;
          const label = t(option.labelKey, option.fallback);
          return (
            <button
              key={option.mode}
              type="button"
              aria-label={label}
              aria-pressed={selected}
              onClick={() => setRealtimeMode(option.mode)}
              className="settings-choice-card"
            >
              <span className="settings-choice-card-title">{label}</span>
              <span className="settings-choice-card-desc">
                {t(option.descriptionKey, option.descriptionFallback)}
              </span>
            </button>
          );
        })}
      </div>

      <h3 className="settings-section-title">
        {t("settings.notifications")}
      </h3>
      <label className="settings-checkbox-row">
        <input type="checkbox" disabled={!isFeatureAvailable("notifications")} title={unavailableReason()} checked={isFeatureAvailable("notifications") && notificationsEnabled} onChange={toggleNotifications} />
        <span>{t("settings.enableNotifications")}</span>
      </label>
      <button
        type="button"
        aria-label={t("settings.testNotification", "Send test notification")}
        onClick={handleTestNotification}
        disabled={!isFeatureAvailable("notifications") || !notificationsEnabled || testingNotification}
        title={!isFeatureAvailable("notifications") ? unavailableReason() : undefined}
        className="settings-button"
        style={{ marginTop: "12px" }}
      >
        {testingNotification
          ? t("settings.testNotificationSending", "Sending test notification...")
          : t("settings.testNotification", "Send test notification")}
      </button>

      {!isFeatureAvailable("notifications") && <p className="settings-section-desc" style={{ marginTop: "8px" }}>{unavailableReason()}</p>}

      <h3 className="settings-section-title">
        {t("settings.startupBehavior", "Startup Behavior")}
      </h3>
      <label className="settings-checkbox-row">
        <input
          type="checkbox"
          disabled={!isFeatureAvailable("autostart")}
          title={!isFeatureAvailable("autostart") ? unavailableReason() : undefined}
          checked={isFeatureAvailable("autostart") && launchAtStartup}
          onChange={toggleLaunchAtStartup}
        />
        <span>{t("settings.launchAtStartup", "Launch GoMail at system startup")}</span>
      </label>
      <label className="settings-checkbox-row">
        <input
          type="checkbox"
          disabled={!isFeatureAvailable("startHidden")}
          title={!isFeatureAvailable("startHidden") ? unavailableReason() : undefined}
          checked={isFeatureAvailable("startHidden") && startHiddenToTray}
          onChange={toggleStartHiddenToTray}
        />
        <span>{t("settings.startHiddenToTray", "Start hidden to tray")}</span>
      </label>

      {(!isFeatureAvailable("autostart") || !isFeatureAvailable("startHidden")) && <p className="settings-section-desc" style={{ marginTop: "8px" }}>{unavailableReason()}</p>}

      <h3 className="settings-section-title">
        {t("settings.closeBehavior", "Close Behavior")}
      </h3>
      <label className="settings-checkbox-row">
        <input
          type="checkbox"
          checked={quitOnClose}
          onChange={toggleQuitOnClose}
        />
        <span>{t("settings.quitOnClose", "Quit app when window is closed")}</span>
      </label>

      <h3 className="settings-section-title">
        {t("settings.folderCounts", "Folder Counts")}
      </h3>
      <label className="settings-checkbox-row">
        <input
          type="checkbox"
          checked={showUnreadCount}
          onChange={toggleUnreadCount}
        />
        <span>{t("settings.showUnreadCount", "Show unread count badges in sidebar")}</span>
      </label>

      <h3 className="settings-section-title">
        {t("settings.systemLabels", "System Labels")}
      </h3>
      <label className="settings-checkbox-row">
        <input
          type="checkbox"
          checked={showSystemLabels}
          onChange={toggleSystemLabels}
        />
        <span>{t("settings.showSystemLabels", "Show system labels in sidebar (e.g. CATEGORY_*, IMPORTANT, STARRED, UNREAD)")}</span>
      </label>

      {navigator.userAgent.includes("Windows") && (
        <>
          <h3 className="settings-section-title">
            {t("settings.defaultMailClient", "Default Email Client")}
          </h3>
          <button
            type="button"
            disabled={!isCommandAvailable("open_default_mail_settings")}
            title={!isCommandAvailable("open_default_mail_settings") ? unavailableReason() : undefined}
            onClick={() => {
              if (!isCommandAvailable("open_default_mail_settings")) return;
              void openDefaultMailSettings().catch(() => addToast({ message: unavailableReason(), type: "error" }));
            }}
            className="settings-button"
          >
            {t("settings.setDefaultMailClient", "Set GoMail as default email app")}
          </button>
          <p className="settings-section-desc" style={{ marginTop: "8px" }}>
            {t("settings.setDefaultMailClientHint", "Opens Windows Default Apps settings where you can select GoMail for email.")}
          </p>
        </>
      )}
    </div>
  );
}
