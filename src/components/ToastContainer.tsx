import { useToastStore } from "@/stores/toast.store";
import { Check, X, AlertTriangle, Info } from "lucide-react";
import { useTranslation } from "react-i18next";

const iconMap = {
  success: Check,
  error: AlertTriangle,
  info: Info,
};

export default function ToastContainer() {
  const { t } = useTranslation();
  const toasts = useToastStore((s) => s.toasts);
  const removeToast = useToastStore((s) => s.removeToast);

  if (toasts.length === 0) return null;

  return (
    <div
      className="toast-region"
      role="region"
      aria-label={t("common.notifications", "Notifications")}
    >
      {toasts.map((toast) => {
        const Icon = iconMap[toast.type];
        const isError = toast.type === "error";
        return (
          <div
            key={toast.id}
            className={`toast-item${isError ? " toast-item--error" : ""}`}
            role={isError ? "alert" : "status"}
            aria-live={isError ? "assertive" : "polite"}
          >
            <span className="toast-icon-badge" aria-hidden="true">
              <Icon
                size={14}
                strokeWidth={toast.type === "success" ? 2.4 : 2}
              />
            </span>
            <span style={{ flex: 1, lineHeight: 1.45 }}>{toast.message}</span>
            {toast.action && (
              <button
                type="button"
                className="settings-button"
                onClick={() => {
                  toast.action!.onClick();
                  removeToast(toast.id);
                }}
                style={{
                  padding: "3px 9px",
                  fontSize: "12px",
                  fontWeight: 600,
                  color: "var(--color-accent)",
                  flexShrink: 0,
                }}
              >
                {toast.action.label}
              </button>
            )}
            <button
              type="button"
              aria-label={t("common.closeNotification", "Close notification")}
              onClick={() => removeToast(toast.id)}
              style={{
                background: "none",
                border: "none",
                cursor: "pointer",
                padding: "4px",
                borderRadius: "var(--ui-radius-sm)",
                color: "currentColor",
                display: "flex",
                flexShrink: 0,
                opacity: 0.7,
              }}
            >
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
