import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { trustSender } from "@/lib/api";
import type { Message } from "@/lib/api";
import { useConfirmStore } from "@/stores/confirm.store";

// The shared confirmation is also used by Settings when choosing full trust.
export async function confirmFullSenderTrust(email: string, t: ReturnType<typeof useTranslation>["t"]) {
  return useConfirmStore.getState().confirm({
    title: t("privacy.confirmTrustTitle", "Fully trust this sender?"),
    message: `${email}\n${t("privacy.confirmTrustMessage", "External images and trackers may reveal your IP address and when you read mail. This does not verify the sender's identity. Trust applies only to this account.")}`,
    confirmLabel: t("privacy.trustAll", "Trust sender"),
  });
}

export function useSenderTrust(message: Pick<Message, "id" | "account_id" | "from_address"> | null, onSaved: () => void) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const locked = useRef(false);
  const current = useRef(message?.id);
  current.current = message?.id;
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => { setError(null); }, [message?.id]);
  async function save(type: "images" | "all") {
    if (!message || locked.current) return;
    const target = message;
    locked.current = true; setBusy(true); setError(null);
    try {
      if (type === "all" && !await confirmFullSenderTrust(target.from_address, t)) return;
      if (!alive.current || current.current !== target.id) return;
      await trustSender(target.account_id, target.from_address, type);
      if (alive.current && current.current === target.id) onSaved();
    } catch {
      if (alive.current && current.current === target.id) setError(t("privacy.saveTrustFailed", "Could not save sender trust. Privacy protection was not relaxed."));
    } finally { locked.current = false; if (alive.current) setBusy(false); }
  }
  return { save, busy, error };
}
