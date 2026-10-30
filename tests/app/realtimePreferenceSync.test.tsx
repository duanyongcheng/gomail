import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setNotificationsEnabled, setRealtimePreference } from "../../src/lib/api";
import { useRealtimePreferenceSync } from "../../src/app/useRealtimePreferenceSync";

const mocks = vi.hoisted(() => ({
  realtimeMode: "battery" as "realtime" | "balanced" | "battery" | "manual",
  notificationsEnabled: false,
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

vi.mock("../../src/lib/api", () => ({
  setNotificationsEnabled: vi.fn(() => Promise.resolve()),
  setRealtimePreference: vi.fn(() => Promise.resolve()),
}));

vi.mock("../../src/stores/ui.store", () => ({
  useUIStore: (selector: (state: {
    realtimeMode: typeof mocks.realtimeMode;
    notificationsEnabled: boolean;
  }) => unknown) =>
    selector({
      realtimeMode: mocks.realtimeMode,
      notificationsEnabled: mocks.notificationsEnabled,
    }),
}));

describe("useRealtimePreferenceSync", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.realtimeMode = "battery";
    mocks.notificationsEnabled = false;
  });

  it("applies realtime preference without writing the notification startup stub", async () => {
    renderHook(() => useRealtimePreferenceSync());

    await waitFor(() => expect(setRealtimePreference).toHaveBeenCalledWith("battery"));
    expect(setNotificationsEnabled).not.toHaveBeenCalled();
  });

  it("ignores stale notification preference changes without restarting realtime", async () => {
    const { rerender } = renderHook(() => useRealtimePreferenceSync());
    await waitFor(() => expect(setRealtimePreference).toHaveBeenCalledTimes(1));
    vi.mocked(setNotificationsEnabled).mockClear();

    mocks.notificationsEnabled = true;
    rerender();

    expect(setNotificationsEnabled).not.toHaveBeenCalled();
    expect(setRealtimePreference).toHaveBeenCalledTimes(1);
  });

  it("serializes rapid realtime mode changes so the latest transition finishes last", async () => {
    const firstTransition = deferred<void>();
    vi.mocked(setRealtimePreference)
      .mockReturnValueOnce(firstTransition.promise)
      .mockResolvedValue(undefined);
    const { rerender } = renderHook(() => useRealtimePreferenceSync());
    await waitFor(() => expect(setRealtimePreference).toHaveBeenCalledWith("battery"));

    mocks.realtimeMode = "realtime";
    rerender();
    await act(async () => Promise.resolve());
    expect(setRealtimePreference).toHaveBeenCalledTimes(1);

    await act(async () => firstTransition.resolve());
    await waitFor(() => expect(setRealtimePreference).toHaveBeenLastCalledWith("realtime"));
    expect(setRealtimePreference).toHaveBeenCalledTimes(2);
  });
});
