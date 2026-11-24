import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "GoMail",
  identifier: "dev.bary.gomail",
  version: (process.env.GOMAIL_VERSION || "0.4.0").replace(/^v/, ""),
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  updates: {
    publicKey: "y1Ry43TnUetqMkPQ2lnrY0v9rcKVSPL2zXOXEX8kbvQ=",
    github: "duanyongcheng/gomail",
    deltas: 3,
  },
});
