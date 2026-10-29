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
      <h2 style={{ fontSize: "18px", fontWeight: 600, marginBottom: "20px" }}>
        {t("privacy.settingsTitle", "Privacy & Tracking")}
      </h2>

      {/* Global privacy mode */}
      <div style={{ marginBottom: "24px" }}>
        <label style={{ fontSize: "13px", fontWeight: 600, display: "block", marginBottom: "8px" }}>
          {t("privacy.defaultMode", "Default privacy mode")}
        </label>
        <div style={{ display: "flex", gap: "8px" }}>
          <button
            onClick={() => handlePrivacyModeChange("strict")}
            style={{
              padding: "8px 16px",
              borderRadius: "6px",
              border: "1px solid var(--color-border)",
              backgroundColor: privacyMode === "strict" ? "var(--color-accent)" : "var(--color-bg)",
              color: privacyMode === "strict" ? "#fff" : "var(--color-text-primary)",
              cursor: "pointer",
              fontSize: "13px",
            }}
          >
            {t("privacy.strict", "Strict")}
          </button>
          <button
            onClick={() => handlePrivacyModeChange("relaxed")}
            style={{
              padding: "8px 16px",
              borderRadius: "6px",
              border: "1px solid var(--color-border)",
              backgroundColor: privacyMode === "relaxed" ? "var(--color-accent)" : "var(--color-bg)",
              color: privacyMode === "relaxed" ? "#fff" : "var(--color-text-primary)",
              cursor: "pointer",
              fontSize: "13px",
            }}
          >
            {t("privacy.relaxed", "Relaxed")}
          </button>
          <button
            onClick={() => handlePrivacyModeChange("off")}
            style={{
              padding: "8px 16px",
              borderRadius: "6px",
              border: "1px solid var(--color-border)",
              backgroundColor: privacyMode === "off" ? "var(--color-accent)" : "var(--color-bg)",
              color: privacyMode === "off" ? "#fff" : "var(--color-text-primary)",
              cursor: "pointer",
              fontSize: "13px",
            }}
          >
            {t("privacy.off", "Off")}
          </button>
        </div>
        <p style={{ fontSize: "12px", color: "var(--color-text-secondary)", marginTop: "6px" }}>
          {privacyMode === "strict"
            ? t("privacy.strictDesc", "Block all external images and trackers by default. You can load images per-message.")
            : privacyMode === "relaxed"
            ? t("privacy.relaxedDesc", "Load external images by default. Trackers are still blocked.")
            : t("privacy.offDesc", "No blocking. All external images and trackers are loaded directly.")}
        </p>
      </div>

      {/* Tracker blocking info */}
      <div style={{
        padding: "12px 16px",
        borderRadius: "6px",
        backgroundColor: "var(--color-bg-hover)",
        marginBottom: "24px",
        fontSize: "13px",
      }}>
        <strong>{t("privacy.trackerBlocking", "Tracker blocking")}</strong>
        <p style={{ margin: "4px 0 0", color: "var(--color-text-secondary)", fontSize: "12px" }}>
          {privacyMode === "off"
            ? t("privacy.trackerBlockingOff", "Tracker blocking is disabled in Off mode. All images and trackers are loaded directly.")
            : t("privacy.trackerBlockingDesc", "Known tracking pixels and tracker domains are blocked unless privacy is Off or the sender is fully trusted.")}
        </p>
      </div>

      <TrustedSenders />
    </div>
  );
}
