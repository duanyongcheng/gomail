import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Attachment } from "@/lib/api";

const mocks = vi.hoisted(() => ({
  list: vi.fn(), download: vi.fn(), directory: vi.fn(), save: vi.fn(), toast: vi.fn(), unlisten: vi.fn(),
  progress: undefined as undefined | ((event: { payload: { attachment_id: string; bytes_copied: number; total_bytes: number } }) => void),
}));
vi.mock("@/lib/api", () => ({ listAttachments: mocks.list, downloadAttachment: mocks.download }));
vi.mock("@/lib/platform/attachments", () => ({ saveAttachmentAs: mocks.save }));
vi.mock("@tauri-apps/api/path", () => ({ downloadDir: mocks.directory }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn((_name, callback) => { mocks.progress = callback; return Promise.resolve(mocks.unlisten); }) }));
vi.mock("@/stores/toast.store", () => ({ useToastStore: { getState: () => ({ addToast: mocks.toast }) } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
import AttachmentList from "@/components/AttachmentList";

const attachment: Attachment = { id: "att", message_id: "m", filename: "report.txt", mime_type: "text/plain", size: 10, local_path: null, content_id: null, is_inline: false };
const download = () => screen.getByRole("button", { name: "attachments.download: report.txt" });
const save = () => screen.getByRole("button", { name: "attachments.saveAs: report.txt" });

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([attachment, { ...attachment, id: "inline", filename: "logo.png", is_inline: true }]);
  mocks.directory.mockResolvedValue("/Downloads/");
  mocks.download.mockResolvedValue("/Downloads/report (1).txt");
  mocks.save.mockResolvedValue(null);
});
afterEach(cleanup);

describe("AttachmentList export", () => {
  it("downloads cached files and shows actual unique path, hiding inline attachments", async () => {
    render(<AttachmentList messageId="m" />);
    await screen.findByText("report.txt");
    expect(screen.queryByText("logo.png")).toBeNull();
    fireEvent.click(download());
    await waitFor(() => expect(mocks.download).toHaveBeenCalledWith("att", "/Downloads/report.txt"));
    await waitFor(() => expect(download().getAttribute("title")).toBe("/Downloads/report (1).txt"));
  });

  it("treats save dialog cancellation as normal and supports Save As", async () => {
    render(<AttachmentList messageId="m" />);
    await screen.findByText("report.txt");
    fireEvent.click(save());
    await waitFor(() => expect((save() as HTMLButtonElement).disabled).toBe(false));
    expect(mocks.save).toHaveBeenCalledWith("att");
    expect(mocks.download).not.toHaveBeenCalled();
    expect(mocks.toast).not.toHaveBeenCalled();
    expect(download().getAttribute("title")).toBe("attachments.download");
    mocks.save.mockResolvedValueOnce("/chosen/report.txt");
    fireEvent.click(save());
    await waitFor(() => expect(download().getAttribute("title")).toBe("/chosen/report.txt"));
  });

  it("disables both actions while copying, reports progress, and allows retry on failure", async () => {
    let reject!: (error: Error) => void;
    mocks.download.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; }));
    render(<AttachmentList messageId="m" />);
    await screen.findByText("report.txt");
    fireEvent.click(download());
    await waitFor(() => expect(mocks.download).toHaveBeenCalled());
    expect((download() as HTMLButtonElement).disabled).toBe(true);
    expect((save() as HTMLButtonElement).disabled).toBe(true);
    act(() => mocks.progress?.({ payload: { attachment_id: "att", bytes_copied: 5, total_bytes: 10 } }));
    expect(screen.getByText("50%")).toBeTruthy();
    await act(async () => reject(new Error("disk full")));
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ type: "error" }));
    expect(screen.queryByText("50%")).toBeNull();
    expect((save() as HTMLButtonElement).disabled).toBe(false);
  });

  it("uses Windows separators and drops stale results on a message change", async () => {
    mocks.directory.mockResolvedValue("C:\\Users\\test\\Downloads\\");
    let resolve!: (path: string) => void;
    mocks.download.mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    const view = render(<AttachmentList messageId="m" />);
    await screen.findByText("report.txt");
    fireEvent.click(download());
    await waitFor(() => expect(mocks.download).toHaveBeenCalledWith("att", "C:\\Users\\test\\Downloads\\report.txt"));
    view.rerender(<AttachmentList messageId="other" />);
    await screen.findByText("report.txt");
    await act(async () => resolve("C:\\old-result.txt"));
    expect(download().getAttribute("title")).toBe("attachments.download");
    expect((download() as HTMLButtonElement).disabled).toBe(false);
  });
});
