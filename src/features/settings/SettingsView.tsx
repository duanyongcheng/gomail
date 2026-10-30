import { useTranslation } from "react-i18next";
import { useEffect } from "react";
import { isSettingsTabAvailable, unavailableReason } from "@/lib/availability";
import { useUIStore, type SettingsTab } from "@/stores/ui.store";
import AccountsTab from "./AccountsTab";
import GeneralTab from "./GeneralTab";
import ProxyTab from "./ProxyTab";
import AppearanceTab from "./AppearanceTab";
import CloudSyncTab from "./CloudSyncTab";
import RulesTab from "./RulesTab";
import PendingOpsTab from "./PendingOpsTab";
import OutboxPanel from "../compose/OutboxPanel";
import ShortcutsTab from "./ShortcutsTab";
import TranslateTab from "./TranslateTab";
import AiTab from "./AiTab";
import PrivacyTab from "./PrivacyTab";
import AboutTab from "./AboutTab";

const TAB_IDS = ["accounts", "general", "proxy", "appearance", "privacy", "rules", "remoteWrites", "translation", "ai", "shortcuts", "cloudSync", "about"] as const;

const TAB_LABEL_KEYS: Record<string, string> = {
  accounts: "settings.accounts",
  general: "settings.general",
  proxy: "settings.proxy",
  appearance: "settings.appearance",
  privacy: "settings.privacy",
  rules: "settings.rules",
  remoteWrites: "settings.remoteWrites",
  translation: "settings.translation",
  ai: "settings.ai",
  shortcuts: "settings.shortcuts",
  cloudSync: "settings.cloudSync",
  about: "settings.about",
};

export default function SettingsView() {
  const { t } = useTranslation();
  const restoredTab = useUIStore((s) => s.settingsTab);
  const activeTab = isSettingsTabAvailable(restoredTab) ? restoredTab : "accounts";
  const availableTabs = TAB_IDS.filter(isSettingsTabAvailable);
  useEffect(() => {
    if (!isSettingsTabAvailable(restoredTab)) useUIStore.getState().setSettingsTab("accounts");
  }, [restoredTab]);
  const setSettingsTab = useUIStore((s) => s.setSettingsTab);

  function handleTabChange(id: SettingsTab) {
    setSettingsTab(id);
  }

  return (
    <div style={{ display: "flex", height: "100%" }}>
      {/* Tab sidebar */}
      <div
        role="tablist"
        aria-orientation="vertical"
        aria-label={t("settings.tabs", "Settings tabs")}
        style={{
          width: "180px",
          borderRight: "1px solid var(--color-border)",
          padding: "16px 0",
          flexShrink: 0,
        }}
      >
        {TAB_IDS.map((id) => (
          <button
            key={id}
            id={`settings-tab-${id}`}
            role="tab"
            disabled={!isSettingsTabAvailable(id)}
            title={!isSettingsTabAvailable(id) ? unavailableReason() : undefined}
            aria-selected={activeTab === id}
            aria-controls={`settings-tabpanel-${id}`}
            tabIndex={activeTab === id ? 0 : -1}
            onClick={() => handleTabChange(id)}
            onKeyDown={(e) => {
              const index = availableTabs.indexOf(id);
              let nextIndex = index;
              if (e.key === "ArrowDown") { nextIndex = (index + 1) % availableTabs.length; }
              else if (e.key === "ArrowUp") { nextIndex = (index - 1 + availableTabs.length) % availableTabs.length; }
              else if (e.key === "Home") { nextIndex = 0; }
              else if (e.key === "End") { nextIndex = availableTabs.length - 1; }
              else { return; }
              e.preventDefault();
              handleTabChange(availableTabs[nextIndex]);
              document.getElementById(`settings-tab-${availableTabs[nextIndex]}`)?.focus();
            }}
            style={{
              display: "block",
              width: "100%",
              textAlign: "left",
              padding: "8px 20px",
              border: "none",
              background: activeTab === id ? "var(--color-bg-hover)" : "none",
              color: activeTab === id ? "var(--color-text-primary)" : "var(--color-text-secondary)",
              fontWeight: activeTab === id ? 600 : 400,
              fontSize: "13px",
              cursor: isSettingsTabAvailable(id) ? "pointer" : "not-allowed",
              opacity: isSettingsTabAvailable(id) ? 1 : 0.5,
              borderRight: activeTab === id ? "2px solid var(--color-accent)" : "2px solid transparent",
              transition: "background-color 0.15s ease, color 0.15s ease, border-color 0.15s ease",
            }}
          >
            {t(TAB_LABEL_KEYS[id])}
          </button>
        ))}
      </div>
      {/* Tab content */}
      <div
        id={`settings-tabpanel-${activeTab}`}
        className="scroll-region settings-panel-scroll"
        role="tabpanel"
        aria-labelledby={`settings-tab-${activeTab}`}
        style={{
          flex: 1,
          minWidth: 0,
          padding: "32px",
          maxWidth: activeTab === "remoteWrites" ? "980px" : "640px",
          boxSizing: "border-box",
          overflowY: "auto",
          overflowX: "hidden",
        }}
      >
        <p style={{ fontSize: "12px", color: "var(--color-text-secondary)" }}>
          {t("settings.unavailableHint", "Disabled settings are not available in Pebble Go yet.")}
        </p>
        {activeTab === "accounts" && <AccountsTab />}
        {activeTab === "general" && <GeneralTab />}
        {activeTab === "proxy" && <ProxyTab />}
        {activeTab === "appearance" && <AppearanceTab />}
        {activeTab === "rules" && <RulesTab />}
        {activeTab === "remoteWrites" && <><OutboxPanel /><PendingOpsTab /></>}
        {activeTab === "translation" && <TranslateTab />}
        {activeTab === "ai" && <AiTab />}
        {activeTab === "shortcuts" && <ShortcutsTab />}
        {activeTab === "privacy" && <PrivacyTab />}
        {activeTab === "cloudSync" && <CloudSyncTab />}
        {activeTab === "about" && <AboutTab />}
      </div>
    </div>
  );
}
