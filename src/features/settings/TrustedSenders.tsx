import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAccountsQuery } from "@/hooks/queries";
import { useMailStore } from "@/stores/mail.store";
import { listTrustedSenders, removeTrustedSender, trustSender } from "@/lib/api";
import type { TrustedSender } from "@/lib/api";
import { subscribePrivacyChanges } from "@/lib/privacyEvents";
import { confirmFullSenderTrust } from "@/hooks/useSenderTrust";
import { inputStyle } from "@/styles/form";

const buttonStyle = { border: "1px solid var(--color-border)", borderRadius: 6, padding: "6px 10px", background: "var(--color-bg)", color: "var(--color-text-primary)", fontSize: 12 };

export default function TrustedSenders() {
  const { t } = useTranslation();
  const activeAccountId = useMailStore((s) => s.activeAccountId);
  const { data: accounts = [] } = useAccountsQuery();
  const [selected, setSelected] = useState<string | null>(null);
  const candidate = selected ?? activeAccountId;
  const accountId = accounts.some((a) => a.id === candidate) ? candidate! : accounts[0]?.id ?? "";
  return <section aria-labelledby="trusted-senders-title" style={{ marginTop: 24 }}>
    <h3 id="trusted-senders-title" className="settings-section-title" style={{ marginTop: 0 }}>{t("privacy.trustedSenders", "Trusted Senders")}</h3>
    <p className="settings-section-desc">{t("privacy.trustScope", "Trust is an exact-address preference for one account, not verification of the sender. Images-only trust still blocks trackers.")}</p>
    <select aria-label={t("privacy.trustAccount", "Trust account")} value={accountId} style={inputStyle} onChange={(e) => setSelected(e.target.value)} disabled={!accounts.length}>
      {accounts.map((a) => <option key={a.id} value={a.id}>{a.account_label || a.email}</option>)}
    </select>
    {accountId ? <TrustManager key={accountId} accountId={accountId} /> : <p className="settings-section-desc" style={{ marginTop: 10 }}>{t("privacy.selectTrustAccount", "Add an account to manage trusted senders.")}</p>}
  </section>;
}

function TrustManager({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const [senders, setSenders] = useState<TrustedSender[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState(false);
  const [version, setVersion] = useState(0);
  const [email, setEmail] = useState("");
  const [level, setLevel] = useState<"images" | "all">("images");
  const [busy, setBusy] = useState(false);
  const acting = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => subscribePrivacyChanges((change) => { if (change.accountId === accountId) setVersion((v) => v + 1); }), [accountId]);
  useEffect(() => {
    let cancelled = false;
    setLoading(true); setLoadError(false);
    listTrustedSenders(accountId).then((rows) => { if (!cancelled) setSenders(rows); })
      .catch(() => { if (!cancelled) setLoadError(true); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [accountId, version]);

  async function change(remove?: string) {
    if (acting.current) return;
    acting.current = true; setBusy(true); setError("");
    try {
      if (!remove && level === "all" && !await confirmFullSenderTrust(email, t)) return;
      if (!alive.current) return;
      if (remove) await removeTrustedSender(accountId, remove);
      else await trustSender(accountId, email, level);
      if (alive.current) { setEmail(""); setVersion((v) => v + 1); }
    } catch {
      if (alive.current) setError(t(remove ? "privacy.removeTrustFailed" : "privacy.saveTrustFailed", remove ? "Failed to remove trusted sender" : "Could not save sender trust. Privacy protection was not relaxed."));
    } finally { acting.current = false; if (alive.current) setBusy(false); }
  }
  return <div style={{ fontSize: 13 }}>
    <form onSubmit={(e) => { e.preventDefault(); void change(); }} style={{ display: "flex", flexWrap: "wrap", gap: 8, margin: "12px 0" }}>
      <input aria-label={t("privacy.senderAddress", "Sender email address")} placeholder="sender@example.com" type="email" required value={email} maxLength={320} disabled={busy} style={{ ...inputStyle, flex: 1, minWidth: 170 }} onChange={(e) => setEmail(e.target.value)} />
      <select aria-label={t("privacy.trustLevel", "Trust level")} value={level} disabled={busy} onChange={(e) => setLevel(e.target.value as "images" | "all")} style={{ ...inputStyle, width: "auto" }}>
        <option value="images">{t("privacy.trustImages", "Trust images")}</option>
        <option value="all">{t("privacy.trustAll", "Trust sender")}</option>
      </select>
      <button type="submit" className="settings-button settings-button--primary" disabled={busy || !email.trim()}>{t("privacy.saveTrust", "Save trust")}</button>
    </form>
    {error && <p role="alert">{error}</p>}
    {loading ? <p role="status">{t("common.loading", "Loading...")}</p> : loadError ? <p role="alert">{t("privacy.loadTrustedFailed", "Failed to load trusted senders")} <button onClick={() => setVersion((v) => v + 1)}>{t("common.retry", "Retry")}</button></p> : senders.length === 0 ? <p>{t("privacy.noTrustedSenders", "No trusted senders yet. Trust a sender from the privacy banner in a message.")}</p> : <ul style={{ listStyle: "none", padding: 0 }}>
      {senders.map((sender) => <li key={sender.email} style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "center", padding: 10, borderBottom: "1px solid var(--color-border)" }}>
        <span style={{ flex: 1, overflowWrap: "anywhere" }}>{sender.email}</span>
        <span>{sender.trust_type === "all" ? t("privacy.trustAll", "Trust sender") : t("privacy.trustImages", "Trust images")}</span>
        <button style={buttonStyle} disabled={busy} aria-label={`${t("common.edit", "Edit")}: ${sender.email}`} onClick={() => { setEmail(sender.email); setLevel(sender.trust_type); }}>{t("common.edit", "Edit")}</button>
        <button style={buttonStyle} disabled={busy} aria-label={`${t("privacy.removeTrust", "Remove trust")}: ${sender.email}`} onClick={() => void change(sender.email)}>{t("privacy.removeTrust", "Remove trust")}</button>
      </li>)}
    </ul>}
  </div>;
}
