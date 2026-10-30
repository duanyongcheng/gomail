import { beforeEach, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  initialize: vi.fn<() => Promise<void>>(),
  render: vi.fn(),
  show: vi.fn(),
  native: vi.fn(),
}));
vi.mock("react-dom/client", () => ({ createRoot: () => ({ render: mocks.render }) }));
vi.mock("@/lib/profileStorage", () => ({ initializeProfileStorageNamespace: mocks.initialize }));
vi.mock("@/lib/startupTiming", () => ({ logStartupTiming: vi.fn() }));
vi.mock("@/lib/i18n", () => ({}));
vi.mock("@/lib/query-client", () => ({ queryClient: {} }));
vi.mock("@/lib/showMainWindow", () => ({ showMainWindow: mocks.show }));
vi.mock("mygo-runtime", () => ({ call: mocks.native, event: vi.fn() }));
vi.mock("../../src/App", () => ({ default: () => null }));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  document.body.innerHTML = '<div id="root"></div>';
});

it("finishing delayed bootstrap renders the app without reopening a natively hidden window", async () => {
  let finish!: () => void;
  mocks.initialize.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
  await import("../../src/main");
  expect(mocks.initialize).toHaveBeenCalledOnce();
  expect(mocks.render).not.toHaveBeenCalled();
  // The native lifecycle can hide the window while these imports are pending.
  // Completing frontend startup must not send any Show/restore command.
  finish();
  await vi.waitFor(() => expect(mocks.render).toHaveBeenCalledOnce());
  expect(mocks.show).not.toHaveBeenCalled();
  expect(mocks.native).not.toHaveBeenCalled();
});
