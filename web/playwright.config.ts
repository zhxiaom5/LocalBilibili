import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  webServer: {
    command:
      "cd .. && npm run build --prefix web && TMPDIR=$(mktemp -d -t localbilibili-e2e) && LOCALBILIBILI_ADDR=127.0.0.1:18080 LOCALBILIBILI_DB=$TMPDIR/localbilibili.db LOCALBILIBILI_CONFIG=$TMPDIR/localbilibili.config.json go run ./cmd/server",
    url: "http://127.0.0.1:18080",
    reuseExistingServer: true,
    timeout: 120_000,
  },
  use: {
    baseURL: "http://127.0.0.1:18080",
    trace: "on-first-retry",
    ...devices["Desktop Chrome"],
  },
});
