import { useEffect, useRef, useState } from "react";
import type { ReactNode, Ref } from "react";
import { useTranslation } from "react-i18next";
import { getVersion } from "@tauri-apps/api/app";
import { CheckCircle2, Copy, Download, ExternalLink, RefreshCw, RotateCw, X } from "lucide-react";
import iconUrl from "@/assets/app-icon.png";
import { readAppLog, type AppLogSnapshot } from "@/lib/api";
import { openExternalLink } from "@/lib/external-links";
import { isCommandAvailable, unavailableReason } from "@/lib/availability";
import { useUpdateStore } from "@/stores/update.store";

const REPO = "duanyongcheng/gomail";
const RELEASES_URL = `https://github.com/${REPO}/releases`;

function openUrl(url: string) {
  void openExternalLink(url);
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} B`;
}

function formatReleaseDate(iso: string | null): string | null {
  if (!iso) return null;
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return null;
  return parsed.toLocaleDateString();
}

type DiagnosticLogState =
  | { status: "loading" }
  | { status: "ready"; snapshot: AppLogSnapshot }
  | { status: "error"; error: string };

const DIAGNOSTIC_CLICK_TARGET = 5;
const DIAGNOSTIC_CLICK_WINDOW_MS = 1500;
const DIAGNOSTIC_LOG_MAX_BYTES = 64 * 1024;

export default function AboutTab() {
  const { t } = useTranslation();
  const diagnosticsAvailable = isCommandAvailable("read_app_log");
  const [fallbackVersion, setFallbackVersion] = useState<string>("");
  const [diagnosticLog, setDiagnosticLog] = useState<DiagnosticLogState | null>(null);
  const diagnosticClickCount = useRef(0);
  const diagnosticClickTimer = useRef<number | null>(null);

  const currentVersion = useUpdateStore((s) => s.currentVersion);
  const canAutoUpdate = useUpdateStore((s) => s.canAutoUpdate);
  const updateState = useUpdateStore((s) => s.state);
  const availableVersion = useUpdateStore((s) => s.availableVersion);
  const releaseNotes = useUpdateStore((s) => s.releaseNotes);
  const releaseDate = useUpdateStore((s) => s.releaseDate);
  const releaseUrl = useUpdateStore((s) => s.releaseUrl);
  const installedVersion = useUpdateStore((s) => s.installedVersion);
  const downloaded = useUpdateStore((s) => s.downloaded);
  const total = useUpdateStore((s) => s.total);
  const percent = useUpdateStore((s) => s.percent);
  const updateError = useUpdateStore((s) => s.error);
  const autoCheckUpdates = useUpdateStore((s) => s.autoCheckUpdates);
  const skippedVersion = useUpdateStore((s) => s.skippedVersion);
  const hydrateStatus = useUpdateStore((s) => s.hydrateStatus);
  const checkForUpdates = useUpdateStore((s) => s.checkForUpdates);
  const installUpdate = useUpdateStore((s) => s.installUpdate);
  const relaunchToUpdate = useUpdateStore((s) => s.relaunchToUpdate);
  const setAutoCheckUpdates = useUpdateStore((s) => s.setAutoCheckUpdates);
  const skipVersion = useUpdateStore((s) => s.skipVersion);

  useEffect(() => {
    void hydrateStatus();
    getVersion().then((v) => {
      if (v) setFallbackVersion(v);
    }).catch(() => {});
  }, [hydrateStatus]);

  useEffect(() => () => {
    if (diagnosticClickTimer.current !== null) {
      window.clearTimeout(diagnosticClickTimer.current);
    }
  }, []);

  const appVersion = currentVersion || fallbackVersion;
  const formattedDate = formatReleaseDate(releaseDate);
  const isBusy = updateState === "checking" || updateState === "downloading";

  async function loadDiagnosticLog() {
    if (!diagnosticsAvailable) return;
    setDiagnosticLog({ status: "loading" });
    try {
      const snapshot = await readAppLog(DIAGNOSTIC_LOG_MAX_BYTES);
      setDiagnosticLog({ status: "ready", snapshot });
    } catch (err) {
      setDiagnosticLog({
        status: "error",
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  function handleDiagnosticIconClick() {
    if (!diagnosticsAvailable) return;
    diagnosticClickCount.current += 1;

    if (diagnosticClickTimer.current !== null) {
      window.clearTimeout(diagnosticClickTimer.current);
    }

    if (diagnosticClickCount.current >= DIAGNOSTIC_CLICK_TARGET) {
      diagnosticClickCount.current = 0;
      void loadDiagnosticLog();
      return;
    }

    diagnosticClickTimer.current = window.setTimeout(() => {
      diagnosticClickCount.current = 0;
      diagnosticClickTimer.current = null;
    }, DIAGNOSTIC_CLICK_WINDOW_MS);
  }

  return (
    <div>
      {/* App info */}
      <div style={{ marginBottom: "28px" }}>
        <div style={{ display: "flex", alignItems: "center", gap: "14px", marginBottom: "16px" }}>
          <button
            type="button"
            onClick={handleDiagnosticIconClick}
            aria-label={t("about.openDiagnosticLog", "Open diagnostic log")}
            disabled={!diagnosticsAvailable}
            title={diagnosticsAvailable ? "GoMail" : unavailableReason()}
            style={{
              width: "56px",
              height: "56px",
              padding: 0,
              border: "none",
              borderRadius: "12px",
              background: "transparent",
              cursor: "default",
              lineHeight: 0,
            }}
          >
            <img
              src={iconUrl}
              alt=""
              draggable={false}
              style={{ width: "56px", height: "56px", objectFit: "contain" }}
            />
          </button>
          <div>
            <div style={{ fontSize: "17px", fontWeight: 650, color: "var(--color-text-primary)" }}>
              GoMail
            </div>
            <div style={{ fontSize: "13px", color: "var(--color-text-secondary)", marginTop: "2px", fontVariantNumeric: "tabular-nums" }}>
              {t("about.version", "Version")} {appVersion || "..."}
            </div>
          </div>
        </div>

        <p style={{ fontSize: "13px", color: "var(--color-text-secondary)", lineHeight: 1.7, margin: "0 0 8px" }}>
          {t(
            "about.goDescription",
            "A local-first Gmail desktop client built with Go, MyGO and React. Mail, search and encrypted drafts stay on your device. Sync, queued writes and optional signed update checks contact only their configured endpoints; clicked web links open in your default browser.",
          )}
        </p>
        <p style={{ fontSize: "13px", color: "var(--color-text-secondary)", lineHeight: 1.7, margin: "0 0 8px" }}>
          {t(
            "about.goFeatures",
            "Supports Gmail, Outlook, IMAP/POP3, local search, conversations, encrypted local drafts, explicit Gmail draft copies, durable send outbox, and signed incremental updates.",
          )}
        </p>
        <p style={{ fontSize: "12px", color: "var(--color-text-tertiary, var(--color-text-secondary))", lineHeight: 1.5, margin: 0 }}>
          {t("about.license", "Open source under AGPL-3.0 license.")}
        </p>
      </div>

      {/* Updates */}
      <div style={{ marginBottom: "28px" }}>
        <h3 className="settings-section-title settings-section-title--first" style={{ marginBottom: "12px" }}>
          {t("about.updates", "Updates")}
        </h3>

        <label className="settings-checkbox-row" style={{ marginBottom: "14px" }}>
          <input
            type="checkbox"
            checked={autoCheckUpdates}
            onChange={(e) => setAutoCheckUpdates(e.target.checked)}
          />
          <span>{t("about.autoCheckUpdates", "Automatically check for updates")}</span>
        </label>

        <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: "12px" }}>
          <button
            type="button"
            className="settings-button"
            onClick={() => void checkForUpdates({ manual: true })}
            disabled={isBusy || updateState === "installed"}
            style={{
              cursor: updateState === "checking" ? "wait" : undefined,
            }}
          >
            <RefreshCw
              size={14}
              aria-hidden="true"
              style={{ animation: updateState === "checking" ? "spin 0.9s linear infinite" : undefined }}
            />
            {updateState === "checking"
              ? t("about.checking", "Checking...")
              : t("about.checkUpdate", "Check for updates")}
          </button>

          {updateState === "up_to_date" && (
            <span style={{ display: "inline-flex", alignItems: "center", gap: "6px", fontSize: "13px", color: "var(--color-success)" }}>
              <CheckCircle2 size={14} aria-hidden="true" />
              {t("about.upToDate", "You're on the latest version")}
            </span>
          )}
        </div>

        {updateState === "available" && availableVersion && (
          <div className="settings-callout about-update-card" style={{ marginTop: "14px" }}>
            <div className="about-update-card-head">
              <div>
                <div className="about-update-card-title">
                  {t("about.newVersion", "New version available: {{version}}", {
                    version: `v${availableVersion}`,
                  })}
                </div>
                <div className="about-update-card-meta">
                  {formattedDate && <span>{formattedDate}</span>}
                  {skippedVersion === availableVersion && (
                    <span>{t("about.versionSkipped", "Skipped for background reminders")}</span>
                  )}
                </div>
              </div>
              <span className="about-update-badge">
                {canAutoUpdate
                  ? t("about.incrementalSupported", "Incremental update")
                  : t("about.manualUpdateBadge", "Manual download")}
              </span>
            </div>

            <p className="about-update-hint">
              {canAutoUpdate
                ? t(
                    "about.incrementalHint",
                    "Downloads only the signed delta patch when available, verifies its signature, and installs cleanly for restart.",
                  )
                : t(
                    "about.manualHint",
                    "This installation cannot update itself (for example a package-manager or read-only install). Download the latest installer from GitHub Releases.",
                  )}
            </p>

            {releaseNotes && (
              <pre className="about-release-notes scroll-region">{releaseNotes}</pre>
            )}

            <div className="about-update-actions">
              {canAutoUpdate ? (
                <button
                  type="button"
                  className="settings-button settings-button--primary"
                  onClick={() => void installUpdate()}
                >
                  <Download size={14} aria-hidden="true" />
                  {t("about.downloadAndInstall", "Download & install")}
                </button>
              ) : (
                <button
                  type="button"
                  className="settings-button settings-button--primary"
                  onClick={() => openUrl(releaseUrl || RELEASES_URL)}
                >
                  <ExternalLink size={14} aria-hidden="true" />
                  {t("about.download", "Download")}
                </button>
              )}

              {canAutoUpdate && (
                <button
                  type="button"
                  className="settings-button"
                  onClick={() => openUrl(releaseUrl || RELEASES_URL)}
                >
                  <ExternalLink size={14} aria-hidden="true" />
                  {t("about.viewRelease", "View release")}
                </button>
              )}

              {skippedVersion !== availableVersion && (
                <button
                  type="button"
                  className="settings-button"
                  onClick={() => skipVersion(availableVersion)}
                >
                  {t("about.skipVersion", "Skip this version")}
                </button>
              )}
            </div>
          </div>
        )}

        {updateState === "downloading" && (
          <div className="settings-callout about-update-card" style={{ marginTop: "14px" }}>
            <div className="about-update-card-head">
              <div className="about-update-card-title">
                {t("about.downloadingVersion", "Downloading v{{version}}...", {
                  version: availableVersion ?? "",
                })}
              </div>
              <span className="about-update-progress-text">
                {total > 0
                  ? `${formatBytes(downloaded)} / ${formatBytes(total)} · ${percent}%`
                  : `${percent}%`}
              </span>
            </div>
            <div
              className="about-update-progress-track"
              role="progressbar"
              aria-label={t("about.downloadProgressLabel", "Update download progress")}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={percent}
            >
              <div className="about-update-progress-fill" style={{ width: `${percent}%` }} />
            </div>
            <p className="about-update-hint" style={{ margin: 0 }}>
              {t(
                "about.verifyingHint",
                "Verifying Ed25519 signature and preparing update without interrupting your current session.",
              )}
            </p>
          </div>
        )}

        {updateState === "installed" && installedVersion && (
          <div className="settings-callout about-update-card" style={{ marginTop: "14px" }}>
            <div className="about-update-card-head">
              <div className="about-update-card-title" style={{ display: "inline-flex", alignItems: "center", gap: "7px" }}>
                <CheckCircle2 size={16} aria-hidden="true" style={{ color: "var(--color-success)" }} />
                {t("about.installedTitle", "v{{version}} is ready to install", {
                  version: installedVersion,
                })}
              </div>
            </div>
            <p className="about-update-hint">
              {t(
                "about.installedHint",
                "The signed update has been verified and staged. Restart GoMail now, or keep working and apply it automatically next time you launch.",
              )}
            </p>
            <div className="about-update-actions">
              <button
                type="button"
                className="settings-button settings-button--primary"
                onClick={() => void relaunchToUpdate()}
              >
                <RotateCw size={14} aria-hidden="true" />
                {t("about.restartNow", "Restart now")}
              </button>
            </div>
          </div>
        )}

        {updateState === "error" && updateError && (
          <div
            role="alert"
            className="settings-callout settings-callout--signal about-update-card"
            style={{ marginTop: "14px" }}
          >
            <div>
              {t("about.checkFailed", "Failed to check for updates")}: {updateError}
            </div>
            <div className="about-update-actions">
              <button
                type="button"
                className="settings-button"
                onClick={() => {
                  if (availableVersion && canAutoUpdate) {
                    void installUpdate();
                  } else {
                    void checkForUpdates({ manual: true });
                  }
                }}
              >
                {t("common.retry", "Retry")}
              </button>
              <button
                type="button"
                className="settings-button"
                onClick={() => openUrl(releaseUrl || RELEASES_URL)}
              >
                <ExternalLink size={14} aria-hidden="true" />
                {t("about.download", "Download")}
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Links */}
      <div>
        <h3 className="settings-section-title" style={{ marginTop: 0, marginBottom: "12px" }}>
          {t("about.links", "Links")}
        </h3>
        <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
          {[
            { label: "GitHub Releases", url: RELEASES_URL },
            { label: t("about.sourceCode", "Source Code"), url: `https://github.com/${REPO}` },
            { label: t("about.reportIssue", "Report an Issue"), url: `https://github.com/${REPO}/issues` },
          ].map((link) => (
            <a
              key={link.url}
              href={link.url}
              onClick={(e) => { e.preventDefault(); openUrl(link.url); }}
              style={{
                fontSize: "13px",
                color: "var(--color-accent)",
                textDecoration: "none",
                cursor: "pointer",
              }}
            >
              {link.label}
            </a>
          ))}
        </div>
      </div>

      {diagnosticLog && (
        <DiagnosticLogDialog
          state={diagnosticLog}
          title={t("about.diagnosticLog", "Diagnostic log")}
          loadingLabel={t("common.loading", "Loading...")}
          emptyLabel={t("about.noDiagnosticLog", "No log entries yet.")}
          truncatedLabel={t("about.diagnosticLogTruncated", "Showing latest 64 KB")}
          refreshLabel={t("common.retry", "Retry")}
          copyPathLabel={t("about.copyLogPath", "Copy path")}
          closeLabel={t("common.close", "Close")}
          onRefresh={() => void loadDiagnosticLog()}
          onClose={() => setDiagnosticLog(null)}
        />
      )}
    </div>
  );
}

function DiagnosticLogDialog({
  state,
  title,
  loadingLabel,
  emptyLabel,
  truncatedLabel,
  refreshLabel,
  copyPathLabel,
  closeLabel,
  onRefresh,
  onClose,
}: {
  state: DiagnosticLogState;
  title: string;
  loadingLabel: string;
  emptyLabel: string;
  truncatedLabel: string;
  refreshLabel: string;
  copyPathLabel: string;
  closeLabel: string;
  onRefresh: () => void;
  onClose: () => void;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const snapshot = state.status === "ready" ? state.snapshot : null;

  useEffect(() => {
    closeButtonRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  function handleCopyPath() {
    if (!snapshot) {
      return;
    }
    void navigator.clipboard?.writeText(snapshot.path);
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="diagnostic-log-title"
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 1200,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: "24px",
        backgroundColor: "rgba(0, 0, 0, 0.48)",
      }}
    >
      <div
        style={{
          width: "min(820px, calc(100vw - 48px))",
          maxHeight: "min(720px, calc(100vh - 48px))",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          border: "1px solid var(--color-border)",
          borderRadius: "10px",
          backgroundColor: "var(--color-bg)",
          boxShadow: "0 24px 70px rgba(0, 0, 0, 0.32)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "12px",
            padding: "16px 18px",
            borderBottom: "1px solid var(--color-border)",
          }}
        >
          <div style={{ minWidth: 0 }}>
            <h3
              id="diagnostic-log-title"
              style={{ margin: 0, fontSize: "15px", fontWeight: 600, color: "var(--color-text-primary)" }}
            >
              {title}
            </h3>
            {snapshot && (
              <div
                style={{
                  marginTop: "6px",
                  fontSize: "12px",
                  color: "var(--color-text-secondary)",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {snapshot.path}
              </div>
            )}
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "8px", flexShrink: 0 }}>
            <IconButton
              label={refreshLabel}
              onClick={onRefresh}
              disabled={state.status === "loading"}
              icon={<RefreshCw size={16} />}
            />
            <IconButton
              label={copyPathLabel}
              onClick={handleCopyPath}
              disabled={!snapshot}
              icon={<Copy size={16} />}
            />
            <IconButton
              buttonRef={closeButtonRef}
              label={closeLabel}
              onClick={onClose}
              icon={<X size={16} />}
            />
          </div>
        </div>

        {state.status === "loading" && (
          <div style={{ padding: "22px", fontSize: "13px", color: "var(--color-text-secondary)" }}>
            {loadingLabel}
          </div>
        )}

        {state.status === "error" && (
          <div style={{ padding: "22px", fontSize: "13px", color: "var(--color-error)", whiteSpace: "pre-wrap" }}>
            {state.error}
          </div>
        )}

        {snapshot && (
          <>
            {snapshot.truncated && (
              <div
                style={{
                  padding: "8px 18px",
                  borderBottom: "1px solid var(--color-border)",
                  fontSize: "12px",
                  color: "var(--color-text-secondary)",
                  backgroundColor: "var(--color-bg-hover)",
                }}
              >
                {truncatedLabel}
              </div>
            )}
            <pre
              style={{
                margin: 0,
                minHeight: "260px",
                maxHeight: "560px",
                overflow: "auto",
                padding: "18px",
                fontSize: "12px",
                lineHeight: 1.6,
                color: "var(--color-text-primary)",
                backgroundColor: "var(--color-bg)",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                fontFamily: "Consolas, 'SFMono-Regular', Menlo, monospace",
              }}
            >
              {snapshot.content || emptyLabel}
            </pre>
          </>
        )}
      </div>
    </div>
  );
}

const IconButton = ({
  icon,
  label,
  onClick,
  disabled,
  buttonRef,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  buttonRef?: Ref<HTMLButtonElement>;
}) => (
  <button
    ref={buttonRef}
    type="button"
    onClick={onClick}
    disabled={disabled}
    aria-label={label}
    title={label}
    style={{
      width: "32px",
      height: "32px",
      display: "inline-flex",
      alignItems: "center",
      justifyContent: "center",
      border: "1px solid var(--color-border)",
      borderRadius: "6px",
      backgroundColor: "var(--color-bg-hover)",
      color: "var(--color-text-primary)",
      cursor: disabled ? "not-allowed" : "pointer",
      opacity: disabled ? 0.55 : 1,
    }}
  >
    {icon}
  </button>
);
