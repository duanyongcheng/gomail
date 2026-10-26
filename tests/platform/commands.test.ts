import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/mygo", () => ({
  AccountService: { list: vi.fn(), login: vi.fn(), delete: vi.fn() },
  AppService: { profile: vi.fn(), quit: vi.fn() },
  FolderService: { list: vi.fn() },
  MessageService: { list: vi.fn(), get: vi.fn(), rendered: vi.fn(), flags: vi.fn(), attachments: vi.fn(), unreadCounts: vi.fn() },
  SearchService: { search: vi.fn() },
  SyncService: { sync: vi.fn().mockResolvedValue({ count: 1 }), trigger: vi.fn().mockResolvedValue({ count: 1 }), start: vi.fn().mockResolvedValue({ value: "a" }), stop: vi.fn().mockResolvedValue({}), preference: vi.fn().mockResolvedValue({}) },
}));
import { MessageService, SyncService } from "@/mygo";
import { invoke } from "../../src/lib/platform/core";
import { commands } from "../../src/lib/platform/manifest";

function frontendCommands(path: string): Set<string> {
  const names = new Set<string>();
  for (const entry of readdirSync(path, { withFileTypes: true })) {
    const name = join(path, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "platform") for (const command of frontendCommands(name)) names.add(command);
    } else if (/\.tsx?$/.test(name)) {
      for (const match of readFileSync(name, "utf8").matchAll(/\binvoke(?:<[^;]*?>)?\(\s*["']([^"']+)/g)) names.add(match[1]);
    }
  }
  return names;
}

describe("MyGO command adapter", () => {
  it("covers every invoke command and parity entry", () => {
    const names = [...frontendCommands("src")].sort();
    expect(Object.keys(commands).sort()).toEqual(names);
    const parity = readFileSync("docs/parity.md", "utf8");
    expect([...parity.matchAll(/^\| `([^`]+)`/gm)].map((match) => match[1]).sort()).toEqual(names);
  });
  it("rejects unknown names and action stubs with their command", async () => {
    await expect(invoke("__unknown__")).rejects.toThrow("NotImplemented: __unknown__");
    await expect(invoke("send_email")).rejects.toThrow("NotImplemented: send_email");
    expect(await invoke("list_contacts")).toEqual([]);
  });
  it("adapts a typed rendered message to the existing tuple contract and preserves privacy mode", async () => {
    vi.mocked(MessageService.rendered).mockResolvedValueOnce({ message: { id: "m" }, rendered: { html: "safe" } } as never);
    const request = { messageId: "m", privacyMode: "LoadOnce" };
    expect(await invoke("get_message_with_html", request)).toEqual([{ id: "m" }, { html: "safe" }]);
    expect(MessageService.rendered).toHaveBeenCalledWith(request);
  });
  it("routes trigger reasons and scheduler controls through typed services", async () => {
    vi.mocked(SyncService.trigger).mockClear();
    for (const reason of ["window_focus", "network_online", "manual"]) {
      await invoke("trigger_sync", { accountId: "a", reason });
      expect(SyncService.trigger).toHaveBeenLastCalledWith({ accountId: "a", reason });
    }
    expect(await invoke("start_sync", { accountId: "a", pollIntervalSecs: 30 })).toBe("a");
    expect(SyncService.start).toHaveBeenCalledWith({ accountId: "a", pollIntervalSecs: 30 });
    await invoke("stop_sync", { accountId: "a" });
    expect(SyncService.stop).toHaveBeenCalledWith({ accountId: "a" });
    await invoke("set_realtime_preference", { mode: "manual" });
    expect(SyncService.preference).toHaveBeenCalledWith({ mode: "manual" });
  });
});
