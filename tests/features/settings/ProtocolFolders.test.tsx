import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ProtocolFolders from "@/features/settings/ProtocolFolders";
import { FolderService } from "@/mygo";
import { useConfirmStore } from "@/stores/confirm.store";
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }) }));
vi.mock("@/mygo", () => ({ FolderService: { management: vi.fn(), manage: vi.fn() } }));
const folder = { id: "f", account_id: "a", remote_id: "Projects", name: "Projects", is_system: false };
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(FolderService.management).mockResolvedValue({ folders: [folder], pending: null } as never);
  vi.mocked(FolderService.manage).mockResolvedValue({ folders: [folder], pending: null } as never);
});
function setup() {
  const client = new QueryClient();
  return render(<QueryClientProvider client={client}><ProtocolFolders accountId="a" /></QueryClientProvider>);
}
it("uses server IDs and asks before destructive folder writes", async () => {
  vi.spyOn(useConfirmStore.getState(), "confirm").mockResolvedValue(false);
  setup(); await screen.findByRole("option", { name: "Projects" });
  fireEvent.change(screen.getByLabelText("Folder"), { target: { value: "f" } });
  fireEvent.click(screen.getByRole("button", { name: "Delete folder" }));
  await waitFor(() => expect(useConfirmStore.getState().confirm).toHaveBeenCalled());
  expect(FolderService.manage).not.toHaveBeenCalled();
});
it("blocks new writes while an earlier receipt is unresolved and refresh remains available", async () => {
  vi.mocked(FolderService.management).mockResolvedValueOnce({ folders: [folder], pending: { id: "intent", action: "create", name: "Maybe created", remoteId: "", createdAt: 1 } } as never);
  setup(); await screen.findByRole("status");
  fireEvent.change(screen.getByLabelText("New folder name"), { target: { value: "Another" } });
  expect((screen.getByRole("button", { name: "Create folder" }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Refresh server folders" }));
  await waitFor(() => expect(FolderService.manage).toHaveBeenCalledWith({ accountId: "a", action: "refresh", folderId: "", name: "Another", pendingId: "intent" }));
});
