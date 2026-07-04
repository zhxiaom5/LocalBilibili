import { expect, test, type APIResponse, type Page } from "@playwright/test";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const runRealE2E = process.env.LOCALBILIBILI_REAL_E2E === "1";
const subtitleCLI = process.env.LOCALBILIBILI_REAL_SUBTITLE_CLI ?? "";

test.skip(!runRealE2E, "Set LOCALBILIBILI_REAL_E2E=1 to run the real local subtitle flow.");
test.skip(!subtitleCLI, "LOCALBILIBILI_REAL_SUBTITLE_CLI must point to a built subtitle-cli binary.");

let workspace = "";
let sampleRoot = "";
let fakeFFmpeg = "";
let fakeWhisper = "";
let fakeModel = "";
let daemon: ChildProcessWithoutNullStreams | undefined;
const daemonURL = process.env.LOCALBILIBILI_REAL_SUBTITLE_API ?? "http://127.0.0.1:19187";
const creatorName = "字幕测试UP主";

test.beforeAll(async () => {
  workspace = await fs.mkdtemp(path.join(os.tmpdir(), "localbilibili-real-subtitles-"));
  sampleRoot = path.join(workspace, "bilibili");
  const creatorDir = path.join(sampleRoot, creatorName);
  await fs.mkdir(creatorDir, { recursive: true });
  await writeExecutableRuntime(workspace);
  await writeSampleWorks(creatorDir);
  daemon = spawn(
    subtitleCLI,
    [
      "daemon",
      "--addr",
      new URL(daemonURL).host,
      "--root",
      sampleRoot,
      "--whisper-cli",
      fakeWhisper,
      "--ffmpeg",
      fakeFFmpeg,
      "--model",
      fakeModel,
      "--no-gpu",
    ],
    { stdio: "pipe" },
  );
  await waitForHTTP(`${daemonURL}/api/check`);
});

test.afterAll(async () => {
  daemon?.kill();
  if (workspace) {
    await fs.rm(workspace, { recursive: true, force: true });
  }
});

test("real local service covers subtitle config, scan mode, public links, creator UI and player UI", async ({ page, baseURL }) => {
  await login(page);

  await page.goto("/subtitles");
  await expect(page.getByRole("heading", { name: "字幕管理" })).toBeVisible();

  await saveSubtitleConfig(page, {
    mode: "local",
    cliPath: subtitleCLI,
    whisperPath: fakeWhisper,
    ffmpegPath: fakeFFmpeg,
    modelPath: fakeModel,
  });
  await page.getByRole("button", { name: "检测配置" }).click();
  await expect(page.getByText(/检测通过/)).toBeVisible();

  await saveSubtitleConfig(page, { mode: "api", apiURL: daemonURL });
  await page.getByRole("button", { name: "检测配置" }).click();
  await expect(page.getByText(/检测通过/)).toBeVisible();

  await saveSubtitleConfig(page, { mode: "scan" });
  await page.getByRole("button", { name: "检测配置" }).click();
  await expect(page.getByText(/CLI 扫码模式可用/)).toBeVisible();

  const directory = await apiJSON<{ directory: { id: number } }>(page, "/api/directories", {
    method: "POST",
    data: { path: sampleRoot },
  });
  await apiJSON(page, `/api/directories/${directory.directory.id}/scan`, { method: "POST" });
  await expect.poll(async () => (await apiJSON<{ status: { running: boolean; state: string } }>(page, "/api/scan/status")).status.running).toBe(false);

  const creators = await apiJSON<{ creators: Array<{ id: number; name: string; work_count: number; subtitle_count: number }> }>(
    page,
    `/api/creators?search=${encodeURIComponent(creatorName)}&min_work_count=0`,
  );
  expect(creators.creators).toHaveLength(1);
  const creator = creators.creators[0];
  expect(creator.work_count).toBeGreaterThanOrEqual(2);

  await page.goto("/creators");
  await page.getByLabel("搜索UP主").fill(creatorName);
  const creatorCard = page.locator(".creator-card").filter({ hasText: creatorName }).first();
  await expect(creatorCard).toContainText("0 个字幕");
  await creatorCard.getByRole("button", { name: /^立即转字幕$/ }).click();
  await expect(page.getByText(/已加入立即转字幕队列/)).toBeVisible();
  await waitForSubtitleIdle(page);

  await page.goto("/creators");
  await page.getByLabel("搜索UP主").fill(creatorName);
  await expect(page.locator(".creator-card").filter({ hasText: creatorName }).first()).toContainText("1 个字幕");

  const feed = await apiJSON<{ works: Array<{ id: number; title: string }> }>(page, `/api/feed?creator_id=${creator.id}&limit=10`);
  const readyWork = feed.works.find((work) => work.title.includes("已有字幕作品"));
  const missingWork = feed.works.find((work) => work.title.includes("缺字幕作品"));
  expect(readyWork).toBeTruthy();
  expect(missingWork).toBeTruthy();

  const creatorLink = await apiJSON<{ url: string }>(page, `/api/subtitles/links/creators/${creator.id}`);
  const creatorText = await publicText(page, baseURL!, creatorLink.url, 200);
  expect(creatorText).toContain("## 已有字幕作品");
  expect(creatorText).toContain("已有字幕文本");

  const readyLink = await apiJSON<{ url: string }>(page, `/api/subtitles/links/works/${readyWork!.id}`);
  const readyText = await publicText(page, baseURL!, readyLink.url, 200);
  expect(readyText).toContain("已有字幕文本");

  const missingLink = await apiJSON<{ url: string }>(page, `/api/subtitles/links/works/${missingWork!.id}`);
  const missingText = await publicText(page, baseURL!, missingLink.url, 202);
  expect(missingText).toContain("字幕生成中");
  await expect.poll(async () => {
    const pageData = await apiJSON<{ jobs: Array<{ source: string }> }>(page, "/api/subtitles/jobs?page=1&page_size=20");
    return pageData.jobs.some((job) => job.source === "api");
  }).toBe(true);

  await page.goto(`/creator/${creator.id}`);
  const subtitleButton = page.getByTitle("复制字幕链接").first();
  await expect(subtitleButton).toBeVisible();
  await subtitleButton.click();
  await expect(page.locator(".subtitle-copy-notice")).toBeVisible();

  await page.goto("/subtitles");
  await expect(page.getByRole("heading", { name: "字幕任务历史" })).toBeVisible();
  await expect(page.locator(".job-list")).toContainText(creatorName);
});

async function login(page: Page) {
  const response = await page.request.post("/api/auth/login", {
    data: { username: "ted", password: "ted" },
    headers: { "Content-Type": "application/json" },
  });
  await expectResponseOK(response, "/api/auth/login");
  await page.goto("/");
  await expect(page.getByRole("link", { name: /字幕管理/ })).toBeVisible();
}

async function saveSubtitleConfig(
  page: Page,
  patch: {
    mode: "local" | "api" | "scan";
    cliPath?: string;
    apiURL?: string;
    whisperPath?: string;
    ffmpegPath?: string;
    modelPath?: string;
  },
) {
  await page.getByLabel("模式").selectOption(patch.mode);
  if (patch.cliPath !== undefined) await page.getByLabel("subtitle-cli 路径").fill(patch.cliPath);
  if (patch.apiURL !== undefined) await page.getByRole("textbox", { name: "外部 API" }).fill(patch.apiURL);
  if (patch.whisperPath !== undefined) await page.getByLabel("whisper-cli").fill(patch.whisperPath);
  if (patch.ffmpegPath !== undefined) await page.getByLabel("ffmpeg").fill(patch.ffmpegPath);
  if (patch.modelPath !== undefined) await page.getByLabel("模型路径").fill(patch.modelPath);
  await page.getByRole("button", { name: "保存字幕配置" }).click();
  await expect(page.getByText("字幕配置已保存")).toBeVisible();
}

async function waitForSubtitleIdle(page: Page) {
  await expect
    .poll(async () => (await apiJSON<{ status: { running: boolean } }>(page, "/api/subtitles/status")).status.running, {
      timeout: 20_000,
    })
    .toBe(false);
}

async function apiJSON<T>(page: Page, url: string, options: { method?: string; data?: unknown } = {}): Promise<T> {
  const response = await page.request.fetch(url, {
    method: options.method ?? "GET",
    data: options.data,
    headers: options.data ? { "Content-Type": "application/json" } : undefined,
  });
  await expectResponseOK(response, url);
  return (await response.json()) as T;
}

async function expectResponseOK(response: APIResponse, url: string) {
  if (!response.ok()) {
    throw new Error(`${url} returned ${response.status()}: ${await response.text()}`);
  }
}

async function publicText(page: Page, baseURL: string, url: string, status: number) {
  const response = await page.request.get(new URL(url, baseURL).toString());
  expect(response.status()).toBe(status);
  return response.text();
}

async function writeExecutableRuntime(root: string) {
  const runtime = path.join(root, "runtime");
  await fs.mkdir(runtime, { recursive: true });
  fakeFFmpeg = path.join(runtime, "ffmpeg");
  fakeWhisper = path.join(runtime, "whisper-cli");
  fakeModel = path.join(runtime, "ggml-small.bin");
  await fs.writeFile(
    fakeFFmpeg,
    `#!/bin/sh
last=""
for arg in "$@"; do last="$arg"; done
printf 'fake wav\\n' > "$last"
`,
    { mode: 0o755 },
  );
  await fs.writeFile(
    fakeWhisper,
    `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-of" ]; then shift; out="$1"; fi
  shift
done
printf 'daemon subtitle text\\n' > "$out.txt"
printf '1\\n00:00:00,000 --> 00:00:01,000\\ndaemon subtitle text\\n' > "$out.srt"
printf '{"text":"daemon subtitle text"}\\n' > "$out.json"
`,
    { mode: 0o755 },
  );
  await fs.writeFile(fakeModel, "fake model\n");
}

async function writeSampleWorks(creatorDir: string) {
  await writeVideoWork(creatorDir, "2026-06-23_已有字幕作品_1001", "1001");
  await fs.writeFile(path.join(creatorDir, "2026-06-23_已有字幕作品_1001.txt"), "已有字幕文本\n");
  await fs.writeFile(path.join(creatorDir, "2026-06-23_已有字幕作品_1001.srt"), "1\n00:00:00,000 --> 00:00:01,000\n已有字幕文本\n");
  await fs.writeFile(path.join(creatorDir, "2026-06-23_已有字幕作品_1001.json"), '{"text":"已有字幕文本"}\n');
  await writeVideoWork(creatorDir, "2026-06-22_缺字幕作品_1002", "1002");
}

async function writeVideoWork(creatorDir: string, stem: string, awemeID: string) {
  await fs.writeFile(path.join(creatorDir, `${stem}.mp4`), "fake video\n");
  await fs.writeFile(
    path.join(creatorDir, `${stem}_data.json`),
    JSON.stringify({
      aweme_id: awemeID,
      desc: stem.includes("已有") ? "已有字幕作品 #字幕" : "缺字幕作品 #字幕",
      create_time: awemeID === "1001" ? 1782144000 : 1782057600,
      author: {
        nickname: creatorName,
        uid: "subtitle-e2e-uid",
        sec_uid: "subtitle-e2e-sec",
        signature: "字幕测试签名",
        avatar_thumb: { url_list: ["https://example.invalid/avatar.webp"] },
      },
      music: { title: "字幕测试音乐" },
    }),
  );
}

async function waitForHTTP(url: string) {
  const started = Date.now();
  while (Date.now() - started < 10_000) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // wait
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`Timed out waiting for ${url}`);
}
