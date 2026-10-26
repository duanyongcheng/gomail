import { JSDOM } from "jsdom";
import { afterAll } from "vitest";

// Node 25's process Web Storage placeholder can shadow jsdom's Window storage
// during Vitest environment population. Use a real jsdom origin for both
// globals so browser persistence tests keep their actual Web Storage semantics.
const storageWindow = new JSDOM("", { url: "http://localhost/" }).window;
for (const name of ["localStorage", "sessionStorage"] as const) {
  const storage = storageWindow[name];
  Object.defineProperty(globalThis, name, { configurable: true, value: storage });
  Object.defineProperty(window, name, { configurable: true, value: storage });
}
afterAll(() => storageWindow.close());
