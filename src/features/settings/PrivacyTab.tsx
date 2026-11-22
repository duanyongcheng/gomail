import { useState } from "react";
import { useTranslation } from "react-i18next";
import TrustedSenders from "./TrustedSenders";
import { PRIVACY_SETTINGS_CHANGED } from "@/lib/privacyEvents";
import { profileLocalStorage } from "@/lib/profileStorage";
import {
  PRIVACY_MODE_KEY,
  readStoredPrivacyMode,
  type StoredPrivacyMode,
} from "@/lib/privacyMode";

export default function PrivacyTab() {
  const { t } = useTranslation();
  const [privacyMode, setPrivacyMode] = useState<StoredPrivacyMode>(() =>
    readStoredPrivacyMode(),
  );

  function handlePrivacyModeChange(mode: StoredPrivacyMode) {
    setPrivacyMode(mode);
    profileLocalStorage.setItem(PRIVACY_MODE_KEY, mode);
    window.dispatchEvent(new Event(PRIVACY_SETTINGS_CHANGED));
  }

  return (
    <div>
      <h2 className="settings-section-title settings-section-title--first" style={{ fontSize: "16px" }}>
        {t("privacy.settingsTitle", "Privacy & Tracking")}
      </h2>

      {/* Global privacy mode */}
      <div style={{ marginBottom: "24px" }}>
        <label style={{ fontSize: "13px", fontWeight: 600, display: "block", marginBottom: "10px" }}>
          {t("privacy.defaultMode", "Default privacy mode")}
        </label>
        <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
          <button
            type="button"
            aria-pressed={privacyMode === "strict"}
            onClick={() => handlePrivacyModeChange("strict")}
            className={`settings-button${privacyMode === "strict" ? " settings-button--primary" : ""}`}
          >
            {t("privacy.strict", "Strict")}
          </button>
          <button
            type="button"
            aria-pressed={privacyMode === "relaxed"}
            onClick={() => handlePrivacyModeChange("relaxed")}
            className={`settings-button${privacyMode === "relaxed" ? " settings-button--primary" : ""}`}
          >
            {t("privacy.relaxed", "Relaxed")}
          </button>
          <button
            type="button"
            aria-pressed={privacyMode === "off"}
            onClick={() => handlePrivacyModeChange("off")}
            className={`settings-button${privacyMode === "off" ? " settings-button--primary" : ""}`}
          >
            {t("privacy.off", "Off")}
          </button>
        </div>
        <p className="settings-section-desc" style={{ marginTop: "8px", marginBottom: 0 }}>
          {privacyMode === "strict"
            ? t("privacy.strictDesc", "Block all external images and trackers by default. You can load images per-message.")
            : privacyMode === "relaxed"
            ? t("privacy.relaxedDesc", "Load external images by default. Trackers are still blocked.")
            : t("privacy.offDesc", "No blocking. All external images and trackers are loaded directly.")}
        </p>
      </div>

      {/* Tracker blocking info */}
      <div className={`settings-callout${privacyMode === "off" ? " settings-callout--signal" : ""}`} style={{ marginBottom: "24px" }}>
        <strong>{t("privacy.trackerBlocking", "Tracker blocking")}</strong>
        <p style={{ margin: "4px 0 0", color: "inherit", opacity: 0.85, fontSize: "12.5px" }}>
          {privacyMode === "off"
            ? t("privacy.trackerBlockingOff", "Tracker blocking is disabled in Off mode. All images and trackers are loaded directly.")
            : t("privacy.trackerBlockingDesc", "Known tracking pixels and tracker domains are blocked unless privacy is Off or the sender is fully trusted.")}
        </p>
      </div>

      <TrustedSenders />
    </div>
  );
}
