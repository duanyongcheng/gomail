import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

const shim = path.resolve(__dirname, "src/poc/platform.ts");
export default defineConfig({
  root: __dirname,
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: [
      { find: /^@tauri-apps\/api\/.*$/, replacement: shim },
      { find: "@", replacement: path.resolve(__dirname, "src") },
    ],
  },
  build: { outDir: "dist", emptyOutDir: true },
});
