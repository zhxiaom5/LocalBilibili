import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: /local-real-subtitles\.spec\.ts/,
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: {
    timeout: 15_000,
  },
  use: {
    baseURL: process.env.LOCALBILIBILI_REAL_E2E_URL ?? "http://127.0.0.1:18087",
    trace: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
});
