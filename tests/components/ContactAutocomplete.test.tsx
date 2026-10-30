import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContactAutocomplete from "@/components/ContactAutocomplete";
import { searchContactSuggestions, suppressContactSuggestion } from "@/lib/api";
vi.mock("@/lib/i18n", () => ({ default: { t: (key: string, fallback?: string) => fallback ?? key } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/lib/api", () => ({ searchContactSuggestions: vi.fn(), suppressContactSuggestion: vi.fn() }));
vi.mock("@/stores/toast.store", () => ({ useToastStore: { getState: () => ({ addToast: vi.fn() }) } }));
beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.useRealTimers());

describe("Recipient entry without contacts", () => {
  it("preserves accessible form identity and explains missing suggestions", () => {
    render(<><label id="to-label" htmlFor="to-input">To</label><ContactAutocomplete id="to-input" name="to" ariaLabelledBy="to-label" value={[]} onChange={vi.fn()} accountId="a" /></>);
    const input = screen.getByRole("combobox", { name: "To" });
    expect(input).toHaveAttribute("id", "to-input");
    expect(input).toHaveAttribute("name", "to");
    expect(input).toHaveAttribute("autocomplete", "email");
    expect(input).toHaveAttribute("aria-autocomplete", "none");
    expect(input.getAttribute("title")).toMatch(/not available/);
    expect(input).toBeEnabled();
  });

  it("preserves pending typed input for draft/send consumers", () => {
    const changed = vi.fn();
    const { rerender } = render(<ContactAutocomplete value={[]} onChange={vi.fn()} accountId="a" inputValue="" onInputValueChange={changed} />);
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "typed@example.com" } });
    expect(changed).toHaveBeenCalledWith("typed@example.com");
    rerender(<ContactAutocomplete value={[]} onChange={vi.fn()} accountId="a" inputValue="typed@example.com" onInputValueChange={changed} />);
    expect(screen.getByRole("combobox")).toHaveValue("typed@example.com");
  });

  it.each(["Enter", ",", "Tab"])("accepts a freely typed address with %s without a contact lookup", async (key) => {
    vi.useFakeTimers();
    const changed = vi.fn();
    render(<ContactAutocomplete value={[]} onChange={changed} accountId="a" />);
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "typed@example.com" } });
    await act(async () => vi.advanceTimersByTime(250));
    fireEvent.keyDown(input, { key });
    expect(changed).toHaveBeenCalledWith(["typed@example.com"]);
    expect(searchContactSuggestions).not.toHaveBeenCalled();
    expect(suppressContactSuggestion).not.toHaveBeenCalled();
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("keeps chip removal and case-insensitive duplicate prevention", () => {
    const changed = vi.fn();
    render(<ContactAutocomplete value={["alice@example.com"]} onChange={changed} accountId="a" />);
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "ALICE@example.com" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(changed).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Backspace" });
    expect(changed).toHaveBeenCalledWith([]);
  });

  it("does not query contacts after an account change or leave a debounce after unmount", async () => {
    vi.useFakeTimers();
    const view = render(<ContactAutocomplete value={[]} onChange={vi.fn()} accountId="a" />);
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "alice" } });
    view.rerender(<ContactAutocomplete value={[]} onChange={vi.fn()} accountId="b" />);
    await act(async () => vi.advanceTimersByTime(250));
    expect(searchContactSuggestions).not.toHaveBeenCalled();
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "bob" } });
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
