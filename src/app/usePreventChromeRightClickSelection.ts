import { useEffect } from "react";

function isMacPlatform(): boolean {
  return typeof navigator !== "undefined" && navigator.userAgent.includes("Macintosh");
}

/**
 * True when a mouse press opens a context menu: right-click everywhere, or
 * Control+primary click on macOS.
 */
export function isSecondaryContextMouseDown(
  event: Pick<MouseEvent, "button" | "ctrlKey">,
  isMac = isMacPlatform(),
): boolean {
  return event.button === 2 || (isMac && event.button === 0 && event.ctrlKey);
}

/**
 * Clear an active selection only if it landed inside `container`, leaving
 * selections elsewhere in the document (such as the message reader) intact.
 */
export function clearSelectionWithin(container: Node): void {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return;
  const insideAnchor = selection.anchorNode !== null && container.contains(selection.anchorNode);
  const insideFocus = selection.focusNode !== null && container.contains(selection.focusNode);
  if (insideAnchor || insideFocus) {
    selection.removeAllRanges();
  }
}

function isEditableElement(element: Element): boolean {
  if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
    return true;
  }
  return element instanceof HTMLElement && element.isContentEditable;
}

function isNonSelectableElement(element: Element): boolean {
  for (let current: Element | null = element; current; current = current.parentElement) {
    const style = window.getComputedStyle(current);
    const value = style.userSelect || style.getPropertyValue("-webkit-user-select");
    if (value === "none") return true;
    if (value === "text" || value === "all" || value === "contain") return false;
  }
  return false;
}

/**
 * Stop macOS WebKit from highlighting text under the pointer during a
 * secondary click on non-selectable UI chrome (sidebar rows, list rows,
 * buttons, menus) before `contextmenu` runs.
 *
 * Editable controls and selectable document content (message headers/bodies,
 * compose, contact details) are left untouched.
 */
export function usePreventChromeRightClickSelection(): void {
  useEffect(() => {
    const onMouseDown = (event: MouseEvent) => {
      if (!isSecondaryContextMouseDown(event)) return;
      const target =
        event.target instanceof Element
          ? event.target
          : event.target instanceof Node
            ? event.target.parentElement
            : null;
      if (!target || isEditableElement(target)) return;
      if (isNonSelectableElement(target)) {
        event.preventDefault();
      }
    };

    document.addEventListener("mousedown", onMouseDown);
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
    };
  }, []);
}
