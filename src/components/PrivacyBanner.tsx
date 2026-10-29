import { Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { RenderedHtml } from "@/lib/api";

interface Props {
  rendered: RenderedHtml;
  onLoadImages: () => void;
  onTrustSender: (trustType: "images" | "all") => void;
  busy?: boolean;
  error?: string | null;
}

export default function PrivacyBanner({ rendered, onLoadImages, onTrustSender, busy = false, error }: Props) {
  const { t } = useTranslation();
  const totalBlocked = rendered.trackers_blocked.length + rendered.images_blocked;

  if (totalBlocked === 0 && !error) {
    return null;
  }

  const parts: string[] = [];
  if (rendered.trackers_blocked.length > 0) {
    parts.push(t("privacy.tracker", { count: rendered.trackers_blocked.length }));
  }
  if (rendered.images_blocked > 0) {
    parts.push(t("privacy.image", { count: rendered.images_blocked }));
  }

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: "10px",
        flexWrap: "wrap",
        padding: "8px 14px",
        backgroundColor: "rgba(245, 158, 11, 0.08)",
        borderBottom: "1px solid var(--color-border)",
        fontSize: "12px",
        color: "var(--color-text-secondary)",
      }}
    >
      <Shield size={14} color="#f59e0b" />
      {error && <span role="alert" style={{ width: "100%" }}>{error}</span>}
      <span style={{ flex: 1 }}>
        {t("privacy.blocked", { items: parts.join(` ${t("privacy.and")} `) })}
      </span>
      {rendered.images_blocked > 0 && (
        <>
          <button
            onClick={onLoadImages}
            disabled={busy}
            style={{
              fontSize: "12px",
              padding: "2px 8px",
              borderRadius: "4px",
              border: "1px solid var(--color-border)",
              backgroundColor: "transparent",
              color: "var(--color-text-primary)",
              cursor: "pointer",
            }}
          >
            {t("privacy.loadImages")}
          </button>
          <button
            onClick={() => onTrustSender("images")}
            disabled={busy}
            title={t(
              "privacy.trustImagesHint",
              "Always load images from this sender. Trackers stay blocked.",
            )}
            style={{
              fontSize: "12px",
              padding: "2px 8px",
              borderRadius: "4px",
              border: "1px solid var(--color-border)",
              backgroundColor: "transparent",
              color: "var(--color-text-primary)",
              cursor: "pointer",
            }}
          >
            {t("privacy.trustImages", "Trust images")}
          </button>
        </>
      )}
      <button
        onClick={() => onTrustSender("all")}
        disabled={busy}
        title={t(
          "privacy.trustAllHint",
          "Fully trust this sender: load images and stop stripping trackers.",
        )}
        style={{
          fontSize: "12px",
          padding: "2px 8px",
          borderRadius: "4px",
          border: "1px solid var(--color-border)",
          backgroundColor: "transparent",
          color: "var(--color-text-primary)",
          cursor: "pointer",
        }}
      >
        {t("privacy.trustAll", "Trust sender")}
      </button>
    </div>
  );
}
