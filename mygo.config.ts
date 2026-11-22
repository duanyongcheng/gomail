import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Pebble Go",
  identifier: "dev.bary.pebble.go",
  version: (process.env.PEBBLE_VERSION || "0.1.0").replace(/^v/, ""),
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
});
