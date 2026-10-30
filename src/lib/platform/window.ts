import { currentWindow } from "mygo-runtime";
import { AppService } from "@/mygo";
import { PhysicalPosition, PhysicalSize } from "./dpi";
function action(action: string, dimensions: Partial<PhysicalSize & PhysicalPosition> = {}) {
  return AppService.windowAction({ action, width: dimensions.width ?? 0, height: dimensions.height ?? 0, x: dimensions.x ?? 0, y: dimensions.y ?? 0 });
}
export async function setClosePreference(profileNamespace: string, keepRunningInBackground: boolean): Promise<void> {
  await AppService.closePreference({ profileNamespace, keepRunningInBackground });
}
export function getCurrentWindow() {
  return {
    ...currentWindow,
    show: async () => { await action("show"); },
    hide: async () => { await action("hide"); },
    setFocus: async () => { await action("focus"); },
    // Closing must go through Go's native OnClose, not a pretend JS listener.
    close: async () => { await action("close"); },
    restore: async () => { await action("restore"); },
    innerSize: async () => { const geometry = await AppService.geometry({}); return new PhysicalSize(geometry.width, geometry.height); },
    outerPosition: async () => { const geometry = await AppService.geometry({}); return new PhysicalPosition(geometry.x, geometry.y); },
    scaleFactor: () => Promise.resolve(window.devicePixelRatio),
    setSize: async (size: PhysicalSize) => { await action("resize", size); },
    setPosition: async (pos: PhysicalPosition) => { await action("move", pos); },
  };
}
