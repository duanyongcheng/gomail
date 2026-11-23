import { beforeEach, describe, expect, it } from "vitest";
import {
  getProfileStorageNamespace,
  initializeProfileStorageNamespace,
  profileLocalStorage,
  profileSessionStorage,
  resetProfileStorageNamespaceForTests,
} from "../../src/lib/profileStorage";

describe("profileStorage", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    resetProfileStorageNamespaceForTests();
  });

  it("scopes local storage values by the active profile namespace", async () => {
    await initializeProfileStorageNamespace(async () => "alpha");
    profileLocalStorage.setItem("gomail-theme", "dark");

    await initializeProfileStorageNamespace(async () => "beta");
    profileLocalStorage.setItem("gomail-theme", "light");

    await initializeProfileStorageNamespace(async () => "alpha");

    expect(profileLocalStorage.getItem("gomail-theme")).toBe("dark");
    expect(localStorage.getItem("gomail:profile:alpha:gomail-theme")).toBe("dark");
    expect(localStorage.getItem("gomail:profile:beta:gomail-theme")).toBe("light");
    expect(localStorage.getItem("gomail-theme")).toBeNull();
  });

  it("scopes session storage values by the active profile namespace", async () => {
    await initializeProfileStorageNamespace(async () => "alpha");
    profileSessionStorage.setItem("gomail-settings-tab", "appearance");

    await initializeProfileStorageNamespace(async () => "beta");

    expect(profileSessionStorage.getItem("gomail-settings-tab")).toBeNull();
    expect(sessionStorage.getItem("gomail:profile:alpha:gomail-settings-tab")).toBe("appearance");
  });

  it("falls back to legacy keys when profile namespace lookup fails", async () => {
    localStorage.setItem("gomail-theme", "dark");

    await initializeProfileStorageNamespace(async () => {
      throw new Error("not running in Tauri");
    });

    expect(getProfileStorageNamespace()).toBeNull();
    expect(profileLocalStorage.getItem("gomail-theme")).toBe("dark");
  });

  it("migrates legacy localStorage keys into the profile namespace once", async () => {
    localStorage.setItem("gomail-theme", "dark");
    localStorage.setItem("gomail-language", "zh");
    localStorage.setItem("gomail-unrelated-key", "untouched");

    await initializeProfileStorageNamespace(async () => "alpha");

    expect(profileLocalStorage.getItem("gomail-theme")).toBe("dark");
    expect(profileLocalStorage.getItem("gomail-language")).toBe("zh");
    expect(localStorage.getItem("gomail-theme")).toBeNull();
    expect(localStorage.getItem("gomail-language")).toBeNull();
    expect(localStorage.getItem("gomail-unrelated-key")).toBe("untouched");

    // A legacy key re-appearing later (e.g. written by a downgrade) must not
    // overwrite the namespaced value, because the migration already ran.
    localStorage.setItem("gomail-theme", "light");
    await initializeProfileStorageNamespace(async () => "alpha");
    expect(profileLocalStorage.getItem("gomail-theme")).toBe("dark");
  });

  it("does not overwrite an existing scoped value during migration", async () => {
    localStorage.setItem("gomail-theme", "legacy-dark");
    localStorage.setItem("gomail:profile:alpha:gomail-theme", "scoped-light");

    await initializeProfileStorageNamespace(async () => "alpha");

    expect(profileLocalStorage.getItem("gomail-theme")).toBe("scoped-light");
  });

  it("migrates legacy sessionStorage keys into the profile namespace", async () => {
    sessionStorage.setItem("gomail-settings-tab", "appearance");

    await initializeProfileStorageNamespace(async () => "alpha");

    expect(profileSessionStorage.getItem("gomail-settings-tab")).toBe("appearance");
    expect(sessionStorage.getItem("gomail-settings-tab")).toBeNull();
  });
});
