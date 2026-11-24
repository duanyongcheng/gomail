import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

interface Props {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  busy?: boolean;
  confirmDisabled?: boolean;
  busyConfirmLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

function getFocusableElements(container: HTMLElement | null): HTMLElement[] {
  if (!container) {
    return [];
  }

  return Array.from(
    container.querySelectorAll<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((element) => !element.hasAttribute("disabled"));
}

export default function ConfirmDialog({
  title,
  message,
  confirmLabel,
  cancelLabel,
  destructive,
  busy = false,
  confirmDisabled = false,
  busyConfirmLabel,
  onConfirm,
  onCancel,
}: Props) {
  const { t } = useTranslation();
  const dialogRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const onConfirmRef = useRef(onConfirm);
  const onCancelRef = useRef(onCancel);
  const busyRef = useRef(busy);
  busyRef.current = busy;

  useEffect(() => { onConfirmRef.current = onConfirm; }, [onConfirm]);
  useEffect(() => { onCancelRef.current = onCancel; }, [onCancel]);

  useEffect(() => {
    const previousFocus =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    // For destructive actions, focus Cancel by default to prevent accidental confirmation
    if (destructive) {
      cancelRef.current?.focus();
    } else {
      confirmRef.current?.focus();
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        if (!busyRef.current) {
          onCancelRef.current();
        }
        return;
      }

      if (e.key !== "Tab") {
        return;
      }

      const focusable = getFocusableElements(dialogRef.current);
      if (focusable.length === 0) {
        return;
      }

      const currentIndex = focusable.indexOf(document.activeElement as HTMLElement);
      const nextIndex = e.shiftKey
        ? (currentIndex <= 0 ? focusable.length - 1 : currentIndex - 1)
        : (currentIndex === focusable.length - 1 ? 0 : currentIndex + 1);

      e.preventDefault();
      focusable[nextIndex]?.focus();
    }

    document.addEventListener("keydown", handleKey);

    return () => {
      document.removeEventListener("keydown", handleKey);
      previousFocus?.focus();
    };
  }, [destructive]);

  return (
    <div
      className="overlay-backdrop overlay-backdrop--dialog"
      role="dialog"
      aria-modal="true"
      aria-busy={busy || undefined}
      aria-labelledby="confirm-dialog-title"
      aria-describedby="confirm-dialog-message"
    >
      <div
        ref={dialogRef}
        className="confirm-dialog-surface"
        style={{
          color: "var(--color-text-primary)",
        }}
      >
        <h3
          id="confirm-dialog-title"
          className="confirm-dialog-title"
        >
          {title}
        </h3>
        <p
          id="confirm-dialog-message"
          className="confirm-dialog-message"
        >
          {message}
        </p>
        <div className="confirm-dialog-actions">
          <button
            ref={cancelRef}
            type="button"
            className="confirm-dialog-btn"
            onClick={() => { if (!busy) onCancel(); }}
            aria-disabled={busy || undefined}
            style={{
              cursor: busy ? "wait" : "pointer",
              opacity: busy ? 0.65 : 1,
            }}
          >
            {cancelLabel || t("common.cancel", "Cancel")}
          </button>
          <button
            ref={confirmRef}
            type="button"
            className={`confirm-dialog-btn ${destructive ? "confirm-dialog-btn--destructive" : "confirm-dialog-btn--primary"}`}
            onClick={() => { if (!busy && !confirmDisabled) onConfirm(); }}
            disabled={confirmDisabled}
            aria-disabled={busy || confirmDisabled || undefined}
            style={{
              cursor: busy ? "wait" : confirmDisabled ? "not-allowed" : "pointer",
              opacity: busy || confirmDisabled ? 0.65 : 1,
            }}
          >
            {busy
              ? busyConfirmLabel || confirmLabel || t("common.confirm", "Confirm")
              : confirmLabel || t("common.confirm", "Confirm")}
          </button>
        </div>
      </div>
    </div>
  );
}
