import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  workers: 1,
  use: { baseURL: "http://localhost:5173", trace: "off" },
  webServer: {
    command: "npm run dev -- --port 5173",
    url: "http://localhost:5173/console/",
    reuseExistingServer: true,
  },
});
