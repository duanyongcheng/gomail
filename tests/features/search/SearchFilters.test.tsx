import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import SearchFilters from "../../../src/features/search/SearchFilters";
import { listAccounts, listFolders } from "../../../src/lib/api";
import { unavailableReason } from "../../../src/lib/availability";

vi.mock("@/lib/api", () => ({ listAccounts: vi.fn(), listFolders: vi.fn() }));

describe("SearchFilters", () => {
  it("refuses restored advanced filters without exposing an editable form or loading folders", () => {
    const onChange = vi.fn();
    const onClear = vi.fn();
    render(<SearchFilters filters={{ from: "sender@example.invalid", hasAttachment: true }} onChange={onChange} onClear={onClear} />);
    expect(screen.getByRole("status").textContent).toBe(unavailableReason());
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
    expect(onClear).not.toHaveBeenCalled();
    expect(listAccounts).not.toHaveBeenCalled();
    expect(listFolders).not.toHaveBeenCalled();
  });
});
