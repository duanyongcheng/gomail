import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ProxyTab from "@/features/settings/ProxyTab";
import { AccountService } from "@/mygo";
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/mygo", () => ({ AccountService: { network: vi.fn(), setNetwork: vi.fn() } }));
beforeEach(() => { vi.clearAllMocks(); vi.mocked(AccountService.network).mockResolvedValue({ proxy: null }); vi.mocked(AccountService.setNetwork).mockResolvedValue({}); });
it("loads and saves explicit HTTP CONNECT policy through typed service", async () => {
  render(<ProxyTab />);
  const save = screen.getByRole("button", { name: "Save global proxy" }) as HTMLButtonElement;
  await waitFor(() => expect(save.disabled).toBe(false));
  fireEvent.change(screen.getByLabelText("Proxy mode"), { target: { value: "custom" } });
  fireEvent.change(screen.getByLabelText("Proxy protocol"), { target: { value: "http" } });
  fireEvent.change(screen.getByLabelText("Proxy host"), { target: { value: "proxy.example" } });
  fireEvent.change(screen.getByLabelText("Proxy port"), { target: { value: "8080" } });
  fireEvent.click(save);
  await screen.findByText("Global proxy saved.");
  expect(AccountService.setNetwork).toHaveBeenCalledExactlyOnceWith({ proxy: { scheme: "http", host: "proxy.example", port: 8080 } });
});
it("cannot overwrite existing policy after load failure", async () => {
  vi.mocked(AccountService.network).mockRejectedValueOnce(new Error("private transport detail"));
  render(<ProxyTab />);
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Save global proxy" }));
  expect(AccountService.setNetwork).not.toHaveBeenCalled();
  expect(screen.queryByText("private transport detail")).toBeNull();
});
it("ignores a save result after unmount", async () => {
  let finish!: (value: {}) => void;
  vi.mocked(AccountService.setNetwork).mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  const { unmount } = render(<ProxyTab />);
  await waitFor(() => expect((screen.getByRole("button", { name: "Save global proxy" }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole("button", { name: "Save global proxy" }));
  fireEvent.click(screen.getByRole("button", { name: "Saving..." }));
  expect(AccountService.setNetwork).toHaveBeenCalledOnce();
  unmount(); finish({});
});
