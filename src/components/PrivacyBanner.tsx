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
    <div className="privacy-banner">
      <span className="privacy-banner-beacon" aria-hidden="true" />
      <Shield size={14} className="privacy-banner-icon" aria-hidden="true" />
      {error && <span role="alert" style={{ width: "100%" }}>{error}</span>}
      <span className="privacy-banner-text">
        {t("privacy.blocked", { items: parts.join(` ${t("privacy.and")} `) })}
      </span>
      <div className="privacy-banner-actions">
        {rendered.images_blocked > 0 && (
          <>
            <button
              className="privacy-banner-button"
              onClick={onLoadImages}
              disabled={busy}
            >
              {t("privacy.loadImages")}
            </button>
            <button
              className="privacy-banner-button"
              onClick={() => onTrustSender("images")}
              disabled={busy}
              title={t(
                "privacy.trustImagesHint",
                "Always load images from this sender. Trackers stay blocked.",
              )}
            >
              {t("privacy.trustImages", "Trust images")}
            </button>
          </>
        )}
        <button
          className="privacy-banner-button"
          onClick={() => onTrustSender("all")}
          disabled={busy}
          title={t(
            "privacy.trustAllHint",
            "Fully trust this sender: load images and stop stripping trackers.",
          )}
        >
          {t("privacy.trustAll", "Trust sender")}
        </button>
      </div>
    </div>
  );
}
