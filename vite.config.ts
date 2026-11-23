import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

const platform = path.resolve(__dirname, "src/lib/platform");
export default defineConfig({
  root: __dirname,
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: [
      { find: /^@tauri-apps\/api\/(core|event|window|app|dpi|path)$/, replacement: `${platform}/$1.ts` },
      { find: "@", replacement: path.resolve(__dirname, "src") },
    ],
  },
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { url: "http://localhost/" } },
    setupFiles: ["./tests/setup.ts"],
    globals: true,
  },
  build: { outDir: "dist", emptyOutDir: true },
});
