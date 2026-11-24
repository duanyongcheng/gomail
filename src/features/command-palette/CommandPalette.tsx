import { useEffect, useRef, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useCommandStore } from "@/stores/command.store";

export default function CommandPalette() {
  const { t } = useTranslation();
  const { isOpen, query, filteredCommands, close, setQuery, execute } = useCommandStore();
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (isOpen) {
      previousFocusRef.current = document.activeElement as HTMLElement;
      setSelectedIndex(0);
    } else if (previousFocusRef.current) {
      previousFocusRef.current.focus();
      previousFocusRef.current = null;
    }
  }, [isOpen]);

  // Focus input when dialog opens
  useEffect(() => {
    if (isOpen) {
      inputRef.current?.focus();
    }
  }, [isOpen]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  // Focus trap: keep Tab cycling within the dialog
  const handleTabTrap = useCallback((e: React.KeyboardEvent) => {
    if (e.key !== "Tab") return;
    const dialog = dialogRef.current;
    if (!dialog) return;

    const focusable = dialog.querySelectorAll<HTMLElement>(
      'input, button, [tabindex]:not([tabindex="-1"])',
    );
    if (focusable.length === 0) return;

    const first = focusable[0];
    const last = focusable[focusable.length - 1];

    if (e.shiftKey) {
      if (document.activeElement === first) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }, []);

  if (!isOpen) return null;

  function handleKeyDown(e: React.KeyboardEvent) {
    handleTabTrap(e);
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((i) => Math.min(i + 1, filteredCommands.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (filteredCommands[selectedIndex]) {
        execute(filteredCommands[selectedIndex].id);
      }
    } else if (e.key === "Escape") {
      close();
    }
  }

  return (
    <div
      className="overlay-backdrop overlay-backdrop--palette"
      onClick={close}
      role="dialog"
      aria-modal="true"
      aria-label={t("commandPalette.title", "Command Palette")}
    >
      <div
        ref={dialogRef}
        className="command-palette-surface"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("commandPalette.placeholder")}
          aria-label={t("commandPalette.placeholder", "Type a command...")}
          aria-controls="command-listbox"
          aria-activedescendant={
            filteredCommands[selectedIndex]
              ? `cmd-${filteredCommands[selectedIndex].id}`
              : undefined
          }
          role="combobox"
          aria-expanded="true"
          aria-autocomplete="list"
          className="command-palette-input"
        />
        <div
          id="command-listbox"
          className="scroll-region command-list-scroll command-palette-list"
          role="listbox"
          aria-label={t("commandPalette.results", "Commands")}
        >
          {filteredCommands.map((cmd, i) => (
            <div
              key={cmd.id}
              id={`cmd-${cmd.id}`}
              role="option"
              aria-selected={i === selectedIndex}
              onClick={() => execute(cmd.id)}
              className="command-palette-item"
            >
              <div>
                <span style={{ fontSize: "13.5px", fontWeight: 500 }}>{cmd.name}</span>
                <span
                  className="command-palette-category"
                  style={{
                    fontSize: "11.5px",
                    color: "var(--color-text-secondary)",
                    marginLeft: "8px",
                  }}
                >
                  {cmd.category}
                </span>
              </div>
              {cmd.shortcut && (
                <span className="command-palette-kbd">
                  {cmd.shortcut}
                </span>
              )}
            </div>
          ))}
          {filteredCommands.length === 0 && (
            <div
              style={{
                padding: "24px 16px",
                textAlign: "center",
                color: "var(--color-text-secondary)",
                fontSize: "13px",
              }}
            >
              {t("commandPalette.noResults")}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
