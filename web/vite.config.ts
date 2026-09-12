import { defineConfig } from "vite";
export default defineConfig({
  base: "/console/",
  build: { outDir: "../internal/webconsole/dist", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
});
