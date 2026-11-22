import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Pebble Go",
  identifier: "dev.bary.pebble.go",
  version: (process.env.PEBBLE_VERSION || "0.2.2").replace(/^v/, ""),
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  updates: {
    publicKey: "y1Ry43TnUetqMkPQ2lnrY0v9rcKVSPL2zXOXEX8kbvQ=",
    github: "duanyongcheng/pebble-go",
    deltas: 3,
  },
});
