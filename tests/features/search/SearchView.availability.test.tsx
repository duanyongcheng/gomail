import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { call } from "mygo-runtime";
import SearchView from "../../../src/features/search/SearchView";
import { useMailStore } from "../../../src/stores/mail.store";
import { useUIStore } from "../../../src/stores/ui.store";
import i18n from "../../../src/lib/i18n";
import { unavailableReason } from "../../../src/lib/availability";

vi.mock("mygo-runtime", () => ({ call: vi.fn(), event: vi.fn() }));
let client: QueryClient;

// No mock of query execution, API, invoke, manifest or generated bindings.
describe("SearchView production availability", () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    vi.mocked(call).mockImplementation(async (method) => {
      if (method === "SearchService.Search") return [];
      throw new Error(`Unexpected native request: ${method}`);
    });
    await i18n.changeLanguage("en");
    useMailStore.setState({ activeAccountId: "synthetic-a" });
    useUIStore.setState({ activeView: "search", searchQuery: "" });
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });
  afterEach(() => client.clear());

  it("disables advanced filters while retaining real account-scoped basic search", async () => {
    render(<QueryClientProvider client={client}><SearchView /></QueryClientProvider>);
    const filters = screen.getByRole("button", { name: "Filters" }) as HTMLButtonElement;
    expect(filters.disabled).toBe(true);
    expect(filters.title).toBe(unavailableReason());
    fireEvent.click(filters);
    expect(screen.queryByLabelText("From")).toBeNull();
    expect(call).not.toHaveBeenCalled();

    const input = screen.getByRole("textbox", { name: "Search", exact: true });
    fireEvent.change(input, { target: { value: "  invoice  " } });
    fireEvent.submit(input.closest("form")!);
    await waitFor(() => expect(call).toHaveBeenCalledWith("SearchService.Search", { query: "invoice", accountId: "synthetic-a", limit: 50 }));
    act(() => useMailStore.setState({ activeAccountId: "synthetic-b" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("SearchService.Search", { query: "invoice", accountId: "synthetic-b", limit: 50 }));
    expect(vi.mocked(call).mock.calls.every(([method]) => method === "SearchService.Search")).toBe(true);
  });
});
