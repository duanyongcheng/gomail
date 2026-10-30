import { profileLocalStorage } from "@/lib/profileStorage";
import { isFeatureAvailable } from "@/lib/availability";

export const START_HIDDEN_TO_TRAY_KEY = "pebble-start-hidden-to-tray";

export function readStartHiddenToTrayPreference(storage: Pick<Storage, "getItem"> = profileLocalStorage): boolean {
  // Preserve legacy storage, but never leave the only window hidden without a tray.
  return isFeatureAvailable("startHidden") && storage.getItem(START_HIDDEN_TO_TRAY_KEY) === "true";
}

export function shouldShowMainWindowOnStartup(storage: Pick<Storage, "getItem"> = profileLocalStorage): boolean {
  return !readStartHiddenToTrayPreference(storage);
}
