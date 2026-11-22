import { useTranslation } from "react-i18next";
import { useEffect, useState } from "react";
import { isSettingsTabAvailable, unavailableReason } from "@/lib/availability";
import { useUIStore, type SettingsTab } from "@/stores/ui.store";
import { isUpdateReminderVisible, useUpdateStore } from "@/stores/update.store";
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
  const [compactNavigation, setCompactNavigation] = useState(() =>
    typeof window.matchMedia === "function" && window.matchMedia("(max-width: 760px)").matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const query = window.matchMedia("(max-width: 760px)");
    const changed = () => setCompactNavigation(query.matches);
    changed();
    query.addEventListener("change", changed);
    return () => query.removeEventListener("change", changed);
  }, []);
  const restoredTab = useUIStore((s) => s.settingsTab);
  const activeTab = isSettingsTabAvailable(restoredTab) ? restoredTab : "accounts";
  const availableTabs = TAB_IDS.filter(isSettingsTabAvailable);
  useEffect(() => {
    if (!isSettingsTabAvailable(restoredTab)) useUIStore.getState().setSettingsTab("accounts");
  }, [restoredTab]);
  useEffect(() => {
    if (!compactNavigation) return;
    document.getElementById(`settings-tab-${activeTab}`)?.scrollIntoView?.({
      inline: "nearest",
      block: "nearest",
    });
  }, [activeTab, compactNavigation]);
  const setSettingsTab = useUIStore((s) => s.setSettingsTab);
  const hasUpdateDot = useUpdateStore((s) => isUpdateReminderVisible(s));

  function handleTabChange(id: SettingsTab) {
    setSettingsTab(id);
  }

  return (
    <div className="settings-workspace">
      {/* Tab sidebar */}
      <div
        role="tablist"
        className="settings-navigation scroll-region"
        aria-orientation={compactNavigation ? "horizontal" : "vertical"}
        aria-label={t("settings.tabs", "Settings tabs")}
      >
        {TAB_IDS.map((id) => (
          <button
            key={id}
            className={`settings-navigation-tab${activeTab === id ? " settings-navigation-tab--active" : ""}`}
            id={`settings-tab-${id}`}
            role="tab"
            disabled={!isSettingsTabAvailable(id)}
            title={!isSettingsTabAvailable(id) ? unavailableReason() : undefined}
            aria-selected={activeTab === id}
            aria-controls={`settings-tabpanel-${id}`}
            tabIndex={activeTab === id ? 0 : -1}
            onClick={() => handleTabChange(id)}
            style={{
              transition: "background-color 0s, color 0s, border-color 0s",
            }}
            onKeyDown={(e) => {
              const index = availableTabs.indexOf(id);
              let nextIndex = index;
              if (e.key === "ArrowDown" || (compactNavigation && e.key === "ArrowRight")) { nextIndex = (index + 1) % availableTabs.length; }
              else if (e.key === "ArrowUp" || (compactNavigation && e.key === "ArrowLeft")) { nextIndex = (index - 1 + availableTabs.length) % availableTabs.length; }
              else if (e.key === "Home") { nextIndex = 0; }
              else if (e.key === "End") { nextIndex = availableTabs.length - 1; }
              else { return; }
              e.preventDefault();
              handleTabChange(availableTabs[nextIndex]);
              document.getElementById(`settings-tab-${availableTabs[nextIndex]}`)?.focus();
            }}
          >
            <span>{t(TAB_LABEL_KEYS[id])}</span>
            {id === "about" && hasUpdateDot && (
              <span className="settings-navigation-dot" aria-hidden="true" />
            )}
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
          overflowY: "auto",
          overflowX: "hidden",
          boxSizing: "border-box",
        }}
      >
        <div className={`settings-panel-inner${activeTab === "remoteWrites" || activeTab === "appearance" ? " settings-panel-inner--wide" : ""}`}>
          <header className="settings-panel-heading">
            <h1>{t(TAB_LABEL_KEYS[activeTab])}</h1>
          </header>
          <p className="settings-availability-note">
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
    </div>
  );
}
