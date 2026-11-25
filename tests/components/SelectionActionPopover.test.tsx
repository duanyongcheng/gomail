import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SelectionActionPopover from "../../src/components/SelectionActionPopover";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}));

describe("SelectionActionPopover", () => {
  it("clears the copied feedback timer when unmounted", async () => {
    vi.useFakeTimers();
    try {
      const view = render(<SelectionActionPopover text="test" position={{ x: 0, y: 0 }}
        onTranslate={vi.fn()} onSearch={vi.fn()} onCreateRule={vi.fn()}
        onAddToKanbanNote={vi.fn()} onClose={vi.fn()} />);
      await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Copy selected text" })); });
      expect(vi.getTimerCount()).toBeGreaterThan(0);
      view.unmount();
      expect(vi.getTimerCount()).toBe(0);
    } finally { vi.useRealTimers(); }
  });
  beforeEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    });
  });

  it("offers copy as the primary selected-text action", async () => {
    render(
      <SelectionActionPopover
        text="selected email text"
        position={{ x: 100, y: 120 }}
        onTranslate={vi.fn()}
        onSearch={vi.fn()}
        onCreateRule={vi.fn()}
        onAddToKanbanNote={vi.fn()}
        onClose={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Copy selected text" }));

    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith("selected email text");
    });
    expect(await screen.findByRole("button", { name: "Copied selected text" })).toBeTruthy();
  });

  it("promotes translate to the primary toolbar while keeping unavailable actions disabled in the secondary menu", () => {
    const onTranslate = vi.fn();
    const onSearch = vi.fn();
    const onCreateRule = vi.fn();
    const onAddToKanbanNote = vi.fn();
    render(
      <SelectionActionPopover
        text="selected email text"
        position={{ x: 100, y: 120 }}
        onTranslate={onTranslate}
        onSearch={onSearch}
        onCreateRule={onCreateRule}
        onAddToKanbanNote={onAddToKanbanNote}
        onClose={vi.fn()}
      />,
    );

    const translateButton = screen.getByRole("button", { name: "Translate selected text" }) as HTMLButtonElement;
    expect(translateButton.disabled).toBe(false);
    fireEvent.click(translateButton);
    expect(onTranslate).toHaveBeenCalledWith("selected email text", { x: 100, y: 120 });

    expect(screen.queryByRole("menuitem", { name: "Search selected text" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Create rule from selected text" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Add selected text as kanban note" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "More selected-text actions" }));

    expect(screen.getByRole("menuitem", { name: "Search selected text" })).toBeTruthy();
    for (const label of ["Create rule from selected text", "Add selected text as kanban note"]) {
      const button = screen.getByRole("menuitem", { name: label }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      expect(button.title).toMatch(/not available/);
    }
    expect(onSearch).not.toHaveBeenCalled();
    expect(onCreateRule).not.toHaveBeenCalled();
    expect(onAddToKanbanNote).not.toHaveBeenCalled();
  });

  it("still searches selected text but never creates rules or kanban notes", () => {
    const onSearch = vi.fn();
    const onCreateRule = vi.fn();
    const onAddToKanbanNote = vi.fn();
    const props = {
      text: "selected email text",
      position: { x: 100, y: 120 },
      onTranslate: vi.fn(),
      onSearch,
      onCreateRule,
      onAddToKanbanNote,
      onClose: vi.fn(),
    };
    let view = render(<SelectionActionPopover {...props} />);

    fireEvent.click(screen.getByRole("button", { name: "More selected-text actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Search selected text" }));

    expect(onSearch).toHaveBeenCalledWith("selected email text");

    view.unmount();
    view = render(<SelectionActionPopover {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "More selected-text actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Create rule from selected text" }));

    expect(onCreateRule).not.toHaveBeenCalled();

    view.unmount();
    render(<SelectionActionPopover {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "More selected-text actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Add selected text as kanban note" }));

    expect(onAddToKanbanNote).not.toHaveBeenCalled();
  });
});
