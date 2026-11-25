import { createEvent, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  clearSelectionWithin,
  isSecondaryContextMouseDown,
  usePreventChromeRightClickSelection,
} from "../../src/app/usePreventChromeRightClickSelection";

function TestHarness() {
  usePreventChromeRightClickSelection();
  return (
    <div>
      <aside style={{ userSelect: "none" }}>
        <span data-testid="chrome-label">Inbox</span>
        <span data-testid="opt-in-selectable" style={{ userSelect: "text" }}>
          Selectable note
        </span>
      </aside>
      <article data-testid="reader-body">Email paragraph</article>
      <input data-testid="search-input" defaultValue="query" />
      <textarea data-testid="compose-textarea" defaultValue="body" />
      <div data-testid="editable-div" contentEditable suppressContentEditableWarning>
        Rich text
      </div>
    </div>
  );
}

describe("usePreventChromeRightClickSelection", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("identifies right-clicks everywhere and Ctrl+click only on macOS", () => {
    expect(isSecondaryContextMouseDown({ button: 2, ctrlKey: false }, false)).toBe(true);
    expect(isSecondaryContextMouseDown({ button: 2, ctrlKey: false }, true)).toBe(true);
    expect(isSecondaryContextMouseDown({ button: 0, ctrlKey: true }, true)).toBe(true);
    expect(isSecondaryContextMouseDown({ button: 0, ctrlKey: true }, false)).toBe(false);
    expect(isSecondaryContextMouseDown({ button: 0, ctrlKey: false }, true)).toBe(false);
  });

  it("prevents right-click mousedown inside non-selectable UI chrome", () => {
    render(<TestHarness />);
    const label = screen.getByTestId("chrome-label");

    const rightPress = createEvent.mouseDown(label, { button: 2 });
    const leftPress = createEvent.mouseDown(label, { button: 0 });
    fireEvent(label, rightPress);
    fireEvent(label, leftPress);

    expect(rightPress.defaultPrevented).toBe(true);
    expect(leftPress.defaultPrevented).toBe(false);
  });

  it("leaves selectable content and editable controls untouched on right-click", () => {
    render(<TestHarness />);

    for (const testId of [
      "reader-body",
      "opt-in-selectable",
      "search-input",
      "compose-textarea",
      "editable-div",
    ]) {
      const target = screen.getByTestId(testId);
      const rightPress = createEvent.mouseDown(target, { button: 2 });
      fireEvent(target, rightPress);
      expect(rightPress.defaultPrevented).toBe(false);
    }
  });

  it("clears selection only when it falls inside the target container", () => {
    const inside = document.createElement("button");
    const insideText = document.createTextNode("Inbox");
    inside.appendChild(insideText);
    const outside = document.createElement("p");
    const outsideText = document.createTextNode("Message body");
    outside.appendChild(outsideText);
    document.body.append(inside, outside);

    const removeAllRanges = vi.fn();
    vi.spyOn(window, "getSelection").mockReturnValue({
      rangeCount: 1,
      isCollapsed: false,
      anchorNode: insideText,
      focusNode: insideText,
      removeAllRanges,
    } as unknown as Selection);

    clearSelectionWithin(inside);
    expect(removeAllRanges).toHaveBeenCalledTimes(1);

    removeAllRanges.mockClear();
    vi.spyOn(window, "getSelection").mockReturnValue({
      rangeCount: 1,
      isCollapsed: false,
      anchorNode: outsideText,
      focusNode: outsideText,
      removeAllRanges,
    } as unknown as Selection);

    clearSelectionWithin(inside);
    expect(removeAllRanges).not.toHaveBeenCalled();

    inside.remove();
    outside.remove();
  });
});
