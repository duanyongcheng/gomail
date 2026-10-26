// Isolated Gmail adapter; no real mutation through legacy automatic hooks.
import { call, on, currentWindow, runtime } from "mygo-runtime";
export function invoke<T>(command: string, args: Record<string, unknown> = {}): Promise<T> {
  return call<T>("Backend.Invoke", command, args);
}
export async function listen<T>(name: string, listener: (event: { payload: T }) => void) { return on<T>(name, (payload) => listener({ payload })); }
export const getVersion = () => Promise.resolve(runtime().version);
export const downloadDir = () => Promise.reject(new Error("Gmail attachment download UI not implemented"));
export const convertFileSrc = (_path: string) => "";
export class PhysicalPosition { constructor(public x: number, public y: number) {} }
export class PhysicalSize { constructor(public width: number, public height: number) {} }
export function getCurrentWindow() {
  return {
    ...currentWindow,
    show: () => call<void>("Backend.WindowAction", "show"),
    hide: () => call<void>("Backend.WindowAction", "hide"),
    setFocus: () => call<void>("Backend.WindowAction", "focus"),
    onCloseRequested: async (_fn: unknown) => () => {},
    onFocusChanged: async (_fn: unknown) => () => {},
    innerSize: () => call<PhysicalSize>("Backend.WindowSize"),
    outerPosition: () => call<PhysicalPosition>("Backend.WindowPosition"),
    scaleFactor: () => Promise.resolve(window.devicePixelRatio),
    setSize: (size: PhysicalSize) => call<void>("Backend.Resize", size.width, size.height),
    setPosition: (pos: PhysicalPosition) => call<void>("Backend.Move", pos.x, pos.y),
  };
}
