import TitleBar from "../components/TitleBar";
import WindowResizeHandles from "../components/WindowResizeHandles";
import Sidebar from "../components/Sidebar";
import StatusBar from "../components/StatusBar";
import InboxView from "../features/inbox/InboxView";
import CommandPalette from "../features/command-palette/CommandPalette";
import ToastContainer from "../components/ToastContainer";
import ConfirmDialog from "../components/ConfirmDialog";
import { useConfirmStore } from "../stores/confirm.store";
import { useComposeStore } from "../stores/compose.store";
import { useUIStore, applyThemeToDom, resolveTheme } from "../stores/ui.store";
import { useCommandStore } from "../stores/command.store";
import { useKanbanStore } from "../stores/kanban.store";
import { useKeyboard } from "../hooks/useKeyboard";
import { useNetworkStatus } from "../hooks/useNetworkStatus";
import { buildCommands } from "../features/command-palette/commands";
import { useEffect, lazy, Suspense, Component, type ReactNode, type ErrorInfo } from "react";
import { createLazyViewPreloader, scheduleLazyViewPreload } from "./lazyViewPreload";
import { useRealtimePreferenceSync } from "./useRealtimePreferenceSync";
import { useRealtimeSyncTriggers } from "./useRealtimeSyncTriggers";
import { useNotificationOpenNavigation } from "./useNotificationOpenNavigation";
import { useCloseToBackground } from "./useCloseToBackground";
import { useTrayI18n } from "./useTrayI18n";
import { useMailtoOpen } from "./useMailtoOpen";
import { useAutoUpdateCheck } from "./useAutoUpdateCheck";
import AppBackground from "./AppBackground";

const loadSettingsView = () => import("../features/settings/SettingsView");
const loadComposeView = () => import("../features/compose/ComposeView");
const loadKanbanView = () => import("../features/kanban/KanbanView");
const loadContactsView = () => import("../features/contacts/ContactsView");
const loadSearchView = () => import("../features/search/SearchView");
const loadSnoozedView = () => import("../features/snoozed/SnoozedView");
const loadStarredView = () => import("../features/starred/StarredView");
const preloadLazyViews = createLazyViewPreloader([
  loadSettingsView,
  loadComposeView,
  ...(isFeatureAvailable("kanban") ? [loadKanbanView] : []),
  ...(isFeatureAvailable("contacts") ? [loadContactsView] : []),
  loadSearchView,
  ...(isFeatureAvailable("snooze") ? [loadSnoozedView] : []),
  loadStarredView,
]);

const SettingsView = lazy(loadSettingsView);
const ComposeView = lazy(loadComposeView);
const KanbanView = lazy(loadKanbanView);
const ContactsView = lazy(loadContactsView);
const SearchView = lazy(loadSearchView);
const SnoozedView = lazy(loadSnoozedView);
const StarredView = lazy(loadStarredView);
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { WifiOff } from "lucide-react";
import { listen } from "@tauri-apps/api/event";
import { useQueryClient } from "@tanstack/react-query";
import { syncTitlebarTheme } from "@/lib/api";
import { isCommandAvailable, isFeatureAvailable, isViewAvailable, unavailableReason } from "@/lib/availability";
import { useToastStore } from "@/stores/toast.store";

export default function Layout() {
  const activeView = useUIStore((s) => s.activeView);
  const displayedView = isViewAvailable(activeView) ? activeView : "inbox";
  const composeKey = useComposeStore((s) => s.composeKey);
  const setActiveView = useUIStore((s) => s.setActiveView);
  const theme = useUIStore((s) => s.theme);
  const backgroundImage = useUIStore((s) => s.backgroundImage);
  const { t } = useTranslation();
  const viewLabels = {
    inbox: t("sidebar.inbox", "Inbox"),
    compose: t("sidebar.compose", "Compose"),
    starred: t("sidebar.starred", "Starred"),
    search: t("sidebar.search", "Search"),
    settings: t("sidebar.settings", "Settings"),
    contacts: t("sidebar.contacts", "Contacts"),
    kanban: t("sidebar.kanban", "Kanban"),
    snoozed: t("sidebar.snoozed", "Snoozed"),
  };
  const queryClient = useQueryClient();

  useKeyboard();

  // Restored or external store state must not mount an unsupported view.
  useEffect(() => {
    if (isViewAvailable(activeView)) return;
    useUIStore.setState({ activeView: "inbox" });
    useToastStore.getState().addToast({ message: unavailableReason(), type: "info" });
  }, [activeView]);

  useEffect(() => {
    if (isFeatureAvailable("kanban")) useKanbanStore.getState().fetchCards();
  }, []);

  useEffect(() => scheduleLazyViewPreload(preloadLazyViews), []);

  useNetworkStatus();
  useRealtimePreferenceSync();
  useRealtimeSyncTriggers();
  useNotificationOpenNavigation();
  useCloseToBackground();
  useTrayI18n();
  useMailtoOpen();
  useAutoUpdateCheck();

  // Re-register commands when language changes
  useEffect(() => {
    useCommandStore.getState().registerCommands(buildCommands(t));
  }, [t]);

  // Global listener: refresh data when snoozed messages are restored
  useEffect(() => {
    if (!isFeatureAvailable("snooze")) return;
    const unlisten = listen<{ message_id: string; return_to?: string }>("mail:unsnoozed", (event) => {
      queryClient.invalidateQueries({ queryKey: ["messages"] });
      queryClient.invalidateQueries({ queryKey: ["snoozed"] });

      const { return_to } = event.payload;
      if (return_to) {
        if (return_to.startsWith("kanban")) {
          setActiveView("kanban");
        } else if (return_to === "inbox" || return_to === "starred" || return_to === "search") {
          setActiveView(return_to as "inbox" | "starred" | "search");
        }
      }
    });
    return () => { unlisten.then((fn) => fn()); };
  }, [queryClient, setActiveView]);

  useEffect(() => {
    applyThemeToDom(theme);
    if (isCommandAvailable("sync_titlebar_theme")) syncTitlebarTheme(resolveTheme(theme)).catch(() => {});
    if (theme === "system") {
      const mql = window.matchMedia("(prefers-color-scheme: dark)");
      const listener = () => {
        applyThemeToDom("system");
        if (isCommandAvailable("sync_titlebar_theme")) syncTitlebarTheme(resolveTheme("system")).catch(() => {});
      };
      mql.addEventListener("change", listener);
      return () => mql.removeEventListener("change", listener);
    }
  }, [theme]);

  return (
    <div
      className={`app-shell flex flex-col h-screen overflow-hidden${isFeatureAvailable("backgroundImage") && backgroundImage ? " app-shell--with-background" : ""}`}
    >
      <AppBackground image={isFeatureAvailable("backgroundImage") ? backgroundImage : null} />
      <WindowResizeHandles />
      <TitleBar viewLabel={viewLabels[displayedView]} />
      <div className="flex flex-1 min-h-0 app-shell-content">
        <Sidebar />
        <main className="flex-1 min-w-0 overflow-auto scroll-region app-main-scroll" style={{ position: "relative" }}>
          <OfflineBanner />
          <ViewErrorBoundary key={displayedView}>
            <Suspense fallback={<ViewLoadingFallback />}>
              {displayedView === "inbox" && <InboxView />}
              {displayedView === "kanban" && <KanbanView />}
              {displayedView === "contacts" && <ContactsView />}
              {displayedView === "settings" && <SettingsView />}
              {displayedView === "search" && <SearchView />}
              {displayedView === "snoozed" && <SnoozedView />}
              {displayedView === "starred" && <StarredView />}
              {displayedView === "compose" && <ComposeView key={composeKey} />}
            </Suspense>
          </ViewErrorBoundary>
        </main>
      </div>
      <StatusBar />
      <CommandPalette />
      <ToastContainer />
      <GlobalConfirmDialog />
    </div>
  );
}

function ViewLoadingFallback() {
  return (
    <div style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
      height: "100%",
      color: "var(--color-text-secondary)",
      fontSize: "13px",
    }}>
      {i18next.t("common.loading", "Loading...")}
    </div>
  );
}

class ViewErrorBoundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ViewError]", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{
          display: "flex", flexDirection: "column", alignItems: "center",
          justifyContent: "center", height: "100%", gap: 12, padding: 24,
          color: "var(--color-text-secondary)",
        }}>
          <p style={{ fontSize: 14, margin: 0 }}>{i18next.t("errorBoundary.title", "Something went wrong")}</p>
          <p style={{ fontSize: 12, margin: 0, color: "var(--color-text-secondary)" }}>
            {i18next.t("errorBoundary.description", "Please try again or refresh the application.")}
          </p>
          {this.state.error && import.meta.env.DEV && (
            <pre style={{ fontSize: 11, color: "var(--color-error)", maxWidth: "90%", overflow: "auto", whiteSpace: "pre-wrap", textAlign: "left" }}>
              {this.state.error.message}
              {"\n"}
              {this.state.error.stack}
            </pre>
          )}
          <button
            onClick={() => this.setState({ error: null })}
            style={{
              padding: "6px 16px", cursor: "pointer",
              backgroundColor: "var(--color-accent)", color: "var(--color-accent-contrast)",
              border: "none", borderRadius: "var(--ui-radius-md)", fontSize: 13, fontWeight: 600,
            }}
          >
            {i18next.t("errorBoundary.retry", "Retry")}
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

function GlobalConfirmDialog() {
  const { isOpen, title, message, destructive, confirmLabel, cancelLabel, handleConfirm, handleCancel } = useConfirmStore();
  if (!isOpen) return null;
  return (
    <ConfirmDialog
      title={title}
      message={message}
      destructive={destructive}
      confirmLabel={confirmLabel}
      cancelLabel={cancelLabel}
      onConfirm={handleConfirm}
      onCancel={handleCancel}
    />
  );
}

function OfflineBanner() {
  const networkStatus = useUIStore((s) => s.networkStatus);
  if (networkStatus === "online") return null;
  return (
    <div className="app-offline-banner" role="status">
      <WifiOff size={14} aria-hidden="true" />
      <span>{i18next.t("status.offline", "Offline")} — {i18next.t("status.offlineHint", "Mail sync is paused until you're back online")}</span>
    </div>
  );
}
