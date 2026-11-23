import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAccountsQuery } from "@/hooks/queries";
import { profileLocalStorage } from "@/lib/profileStorage";
import { senderIdentityLabel } from "@/lib/accountIdentity";

const NOTICE_KEY = "gomail-sender-identity-notice-v1";

export default function SenderIdentityNotice() {
  const { data: accounts = [] } = useAccountsQuery();
  const { t } = useTranslation();
  const titleId = useId();
  const [dismissed, setDismissed] = useState(() => profileLocalStorage.getItem(NOTICE_KEY) === "seen");
  const senders = accounts.filter((account) => account.provider !== "outlook" && account.display_name.trim());
  if (dismissed || senders.length === 0) return null;
  return (
    <aside
      aria-labelledby={titleId}
      style={{
        minWidth: 0,
        marginBottom: "16px",
        padding: "14px 16px",
        border: "1px solid var(--color-border)",
        borderRadius: "var(--ui-radius-md, 8px)",
        background: "var(--color-bg-secondary)",
        color: "var(--color-text-primary)",
        fontSize: "12px",
        lineHeight: 1.6,
        overflowWrap: "anywhere",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: "8px 12px", marginBottom: "6px" }}>
        <h3 id={titleId} style={{ flex: 1, minWidth: 0, margin: 0, fontSize: "13px", fontWeight: 600 }}>
          {t("accountSetup.senderNoticeTitle", "Review your sender names")}
        </h3>
        <button
          type="button"
          onClick={() => {
            profileLocalStorage.setItem(NOTICE_KEY, "seen");
            setDismissed(true);
          }}
          style={{
            flexShrink: 0,
            padding: "4px 10px",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--ui-radius-sm, 6px)",
            background: "var(--color-bg)",
            color: "var(--color-text-primary)",
            font: "inherit",
            cursor: "pointer",
          }}
        >
          {t("accountSetup.senderNoticeDismiss", "Got it")}
        </button>
      </div>
      <p style={{ margin: 0, color: "var(--color-text-secondary)" }}>
        {t("accountSetup.senderNotice", "Sender names now appear in outgoing mail. Review these names, and use Account label for private account descriptions.")}
      </p>
      <details style={{ marginTop: "8px" }}>
        <summary style={{ color: "var(--color-accent)", cursor: "pointer" }}>
          {t("accountSetup.senderPreview", "Preview sender identities")}
        </summary>
        <ul style={{ margin: "8px 0 0", paddingLeft: "18px", maxHeight: "120px", overflowY: "auto", overflowWrap: "anywhere" }}>
          {senders.map((account) => <li key={account.id}>{senderIdentityLabel(account)}</li>)}
        </ul>
      </details>
    </aside>
  );
}
