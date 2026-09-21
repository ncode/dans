import { defineConfig } from "@playwright/test";
const port = process.env.CONSOLE_TEST_PORT || "5189";
if (!/^\d+$/.test(port) || Number(port) < 1024 || Number(port) > 65535)
  throw new Error("CONSOLE_TEST_PORT must be a port from 1024 to 65535");
const baseURL = "http://localhost:" + port;

export default defineConfig({
  testDir: "./tests",
  testMatch: "management*.spec.ts",
  workers: 1,
  outputDir: process.env.CONSOLE_TEST_OUTPUT || "test-results",
  use: { baseURL, trace: "off" },
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    ...(process.env.CONSOLE_WEBKIT
      ? [{ name: "webkit", use: { browserName: "webkit" as const } }]
      : []),
  ],
  webServer: {
    command: "npm run dev -- --port " + port + " --strictPort",
    url: baseURL + "/console/",
    reuseExistingServer: false,
  },
});
