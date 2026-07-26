#!/usr/bin/env node

import { createRequire } from "node:module";
import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const require = createRequire(import.meta.url);
const { chromium } = require("../web/node_modules/playwright");

const rootDir = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
const env = process.env;

const config = {
  baseURL: env.LOCALBILIBILI_REAL_E2E_BASE_URL || "http://127.0.0.1:18188",
  addr: env.LOCALBILIBILI_REAL_E2E_ADDR || "127.0.0.1:18188",
  username: env.LOCALBILIBILI_REAL_E2E_USERNAME || "ted",
  password: env.LOCALBILIBILI_REAL_E2E_PASSWORD || "ted",
  bilibiliCookie: env.BILIBILI_COOKIE_FROM_USER || env.LOCALBILIBILI_BILIBILI_COOKIE || "",
  realCreatorURL: env.LOCALBILIBILI_REAL_E2E_CREATOR_URL || "https://space.bilibili.com/54887875?spm_id_from=333.337.0.0",
  dbDir: env.LOCALBILIBILI_REAL_E2E_DB_DIR || "/tmp/localbilibili-real-user-test",
  mediaDir: env.LOCALBILIBILI_REAL_E2E_MEDIA_DIR || "/tmp/localbilibili-real-user-media",
  sampleDir: env.LOCALBILIBILI_REAL_E2E_SAMPLE_DIR || path.join(rootDir, "samples/bilibili"),
  ytdlp: env.LOCALBILIBILI_YTDLP || "/opt/homebrew/bin/yt-dlp",
  maxWorks: env.LOCALBILIBILI_YTDLP_MAX_WORKS || "3",
  downloadSections: env.LOCALBILIBILI_YTDLP_DOWNLOAD_SECTIONS || "",
  headless: env.LOCALBILIBILI_REAL_E2E_HEADLESS !== "0",
  useExistingServer: env.LOCALBILIBILI_REAL_E2E_USE_EXISTING_SERVER === "1",
  keepData: env.LOCALBILIBILI_REAL_E2E_KEEP_DATA === "1",
  requireCookie: env.LOCALBILIBILI_REAL_E2E_REQUIRE_COOKIE === "1",
  timeoutMs: Number(env.LOCALBILIBILI_REAL_E2E_TIMEOUT_MS || "180000"),
};

const report = {
  baseURL: config.baseURL,
  realCreatorURL: config.realCreatorURL,
  cookieConfigured: Boolean(config.bilibiliCookie),
  scenarios: [],
  artifacts: [],
};

let serverProcess;

function step(name, detail = {}) {
  report.scenarios.push({ name, status: "passed", ...detail });
  console.log(`PASS ${name}`);
}

function skip(name, reason) {
  report.scenarios.push({ name, status: "skipped", reason });
  console.log(`SKIP ${name}: ${reason}`);
}

function fail(message) {
  throw new Error(message);
}

function assert(condition, message) {
  if (!condition) fail(message);
}

async function main() {
  if (!config.bilibiliCookie && config.requireCookie) {
    fail("BILIBILI_COOKIE_FROM_USER is required when LOCALBILIBILI_REAL_E2E_REQUIRE_COOKIE=1");
  }

  await ensureReadyToRun();
  if (!config.useExistingServer) {
    await resetRuntimeDirs();
    serverProcess = await startServer();
  }

  const browser = await chromium.launch({ headless: config.headless });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const consoleErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  page.on("pageerror", (error) => consoleErrors.push(error.message));

  try {
    await login(page);
    const client = apiClient(page);

    if (config.bilibiliCookie) {
      await runRealCreatorFlow(client);
    } else {
      skip("真实 UP主新增和在线更新", "未提供 BILIBILI_COOKIE_FROM_USER，仅执行本地样例兜底验收");
    }

    await ensureSampleCoverage(client);
    const recent = await ensureRecentFeedCoverage(client, page);
    const feed = await assertFeedCoverage(client);
    await runFavoriteFlow(client, feed);
    await runQueueFlow(client, feed.creator);
    await runMultiUserFlow(page, client, feed);
    await runBrowserFlow(page, feed, recent);
    await runDiagnosticsAndSecurity(client);
    await assertNoSeriousConsoleErrors(consoleErrors);

    await writeReport();
    await browser.close();
  } finally {
    if (serverProcess) {
      await stopServer(serverProcess);
    }
  }
}

async function ensureReadyToRun() {
  const distIndex = path.join(rootDir, "web/dist/index.html");
  try {
    await fs.access(distIndex);
  } catch {
    fail("web/dist/index.html 不存在，请先运行 rtk npm run build --prefix web");
  }
  try {
    await fs.access(path.join(rootDir, "web/node_modules/playwright"));
  } catch {
    fail("web/node_modules/playwright 不存在，请先安装前端依赖");
  }
}

async function resetRuntimeDirs() {
  if (!config.keepData) {
    await fs.rm(config.dbDir, { recursive: true, force: true });
    await fs.rm(config.mediaDir, { recursive: true, force: true });
  }
  await fs.mkdir(config.dbDir, { recursive: true });
  await fs.mkdir(config.mediaDir, { recursive: true });
}

async function startServer() {
  const child = spawn("go", ["run", "./cmd/server"], {
    cwd: rootDir,
    stdio: ["ignore", "pipe", "pipe"],
    detached: true,
    env: {
      ...env,
      LOCALBILIBILI_ADDR: config.addr,
      LOCALBILIBILI_DB: path.join(config.dbDir, "localbilibili.db"),
      LOCALBILIBILI_CONFIG: path.join(config.dbDir, "config.json"),
      LOCALBILIBILI_WEB_DIR: path.join(rootDir, "web/dist"),
      LOCALBILIBILI_YTDLP: config.ytdlp,
      LOCALBILIBILI_YTDLP_MAX_WORKS: config.maxWorks,
      ...(config.downloadSections ? { LOCALBILIBILI_YTDLP_DOWNLOAD_SECTIONS: config.downloadSections } : {}),
    },
  });
  child.stdout.on("data", (chunk) => process.stdout.write(chunk));
  child.stderr.on("data", (chunk) => process.stderr.write(chunk));
  await waitForHTTP(`${config.baseURL}/login`, 30000);
  return child;
}

async function stopServer(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  try {
    process.kill(-child.pid, "SIGINT");
  } catch {
    child.kill("SIGINT");
  }
  const exited = await Promise.race([
    new Promise((resolve) => child.once("exit", () => resolve(true))),
    delay(5000).then(() => false),
  ]);
  if (!exited) {
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch {
      child.kill("SIGKILL");
    }
    await new Promise((resolve) => child.once("exit", resolve));
  }
}

async function waitForHTTP(url, timeoutMs) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    try {
      const response = await fetch(url);
      if (response.status < 500) return;
    } catch {
      // Server not ready yet.
    }
    await delay(250);
  }
  fail(`服务未在 ${timeoutMs}ms 内就绪: ${url}`);
}

async function login(page, username = config.username, password = config.password) {
  await page.goto(`${config.baseURL}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill(password);
  await page.getByRole("button", { name: "登录" }).click();
  await page.waitForURL(`${config.baseURL}/`, { timeout: 10000 });
  step(`登录 ${username}`);
}

function apiClient(page) {
  return async function api(pathname, options = {}) {
    const result = await page.evaluate(async ({ pathname, options }) => {
      const response = await fetch(pathname, {
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          ...(options.headers || {}),
        },
        ...options,
      });
      const text = await response.text();
      let body = null;
      try {
        body = text ? JSON.parse(text) : null;
      } catch {
        body = text;
      }
      return {
        ok: response.ok,
        status: response.status,
        headers: Object.fromEntries(response.headers.entries()),
        body,
      };
    }, { pathname, options });
    if (!result.ok && !options.allowFailure) {
      fail(`API ${pathname} failed with ${result.status}: ${safeJSON(result.body)}`);
    }
    return result;
  };
}

async function runRealCreatorFlow(api) {
  let response = await api("/api/update/cookie", {
    method: "PATCH",
    body: JSON.stringify({ cookie: config.bilibiliCookie }),
  });
  assert(response.body.cookie?.configured, "Cookie 保存后未显示 configured=true");
  response = await api("/api/update/cookie/validate", { method: "POST" });
  assert(response.body.cookie?.configured, "Cookie 校验后配置状态丢失");
  step("保存并校验 Bilibili Cookie", { validationOK: Boolean(response.body.ok) });

  response = await api("/api/directories", {
    method: "POST",
    body: JSON.stringify({ path: config.mediaDir }),
  });
  assert(response.status === 201, "保存下载目录失败");
  const directoryID = response.body.directory.id;
  step("保存真实下载目录", { directory: config.mediaDir });

  const noisyText = `1. 重点测试 UP 主：${config.realCreatorURL}\ninvalid://not-bilibili`;
  response = await api("/api/creators/bulk-import/preview", {
    method: "POST",
    body: JSON.stringify({ directory_id: directoryID, text: noisyText }),
  });
  const previewItems = response.body.items || [];
  assert(previewItems.some((item) => item.bilibili_mid || item.bilibili_space_url), "批量预览未解析出 B站 UP主");
  assert(previewItems.some((item) => item.status === "error" || item.error), "批量预览未保留非法链接失败行");
  step("批量新增关注预览：有效链接和非法链接混合");

  response = await api("/api/creators/bulk-import", {
    method: "POST",
    body: JSON.stringify({ directory_id: directoryID, bilibili_space_urls: [config.realCreatorURL] }),
  });
  const imported = response.body.creators || [];
  assert(imported.length >= 1, "批量导入未返回 creator");
  const creator = imported[0];
  assert(creator.id > 0, "导入 creator 缺少 id");
  assert(!/^\d{3,}$/.test(creator.name || ""), "导入后仍是纯数字占位名");
  step("从 0 新增真实 UP主", { creatorID: creator.id, creatorName: creator.name });

  response = await api("/api/creators/bulk-import", {
    method: "POST",
    body: JSON.stringify({ directory_id: directoryID, bilibili_space_urls: [config.realCreatorURL] }),
  });
  assert((response.body.creators || []).length >= 1, "重复导入未复用 creator");
  const creatorsAfterDuplicate = await api(`/api/creators?search=${encodeURIComponent(creator.name || "")}&min_work_count=0`);
  const duplicateMatches = (creatorsAfterDuplicate.body.creators || []).filter((item) => item.id === creator.id);
  assert(duplicateMatches.length === 1, "重复导入产生重复 creator");
  step("重复新增 UP主去重");

  const firstUpdate = await runImmediateUpdate(api, creator.id, "真实 UP主立即更新");
  if (firstUpdate.externalFailure) {
    report.realUpdateExternalFailure = true;
    skip("真实 UP主媒体下载成功验收", firstUpdate.reason);
  } else {
    assert(firstUpdate.feedWorksAfter >= 1, "真实更新完成后 Feed 未出现作品");
    await assertDownloadedLayoutDoesNotLeakCookie(config.mediaDir);
    step("真实 UP主下载落盘和自动入库", { feedWorksAfter: firstUpdate.feedWorksAfter });
  }

  const beforeSecond = await countFeedWorks(api);
  const secondUpdate = await runImmediateUpdate(api, creator.id, "真实 UP主再次更新");
  const afterSecond = await countFeedWorks(api);
  if (!secondUpdate.externalFailure) {
    assert(afterSecond <= beforeSecond + 3, "再次更新后作品数异常膨胀，疑似重复入库");
  }
  step("再次更新幂等检查", { before: beforeSecond, after: afterSecond });

  await api("/api/update/immediate", {
    method: "POST",
    body: JSON.stringify({ creator_ids: [creator.id] }),
    allowFailure: true,
  });
  response = await api("/api/update/immediate", {
    method: "POST",
    body: JSON.stringify({ creator_ids: [creator.id] }),
    allowFailure: true,
  });
  assert([202, 409, 503].includes(response.status), "连续点击立即更新返回异常状态");
  step("连续立即更新去重或明确拒绝");

  return creator;
}

async function runImmediateUpdate(api, creatorID, label) {
  const response = await api("/api/update/immediate", {
    method: "POST",
    body: JSON.stringify({ creator_ids: [creatorID] }),
    allowFailure: true,
  });
  if (![202, 409, 503].includes(response.status)) {
    fail(`${label} 入队返回异常状态 ${response.status}: ${safeJSON(response.body)}`);
  }
  const status = await waitForUpdateIdle(api);
  const jobsPage = await api("/api/update/jobs?page=1&page_size=20");
  const jobs = jobsPage.body.jobs || [];
  const latest = jobs.find((job) => job.creator_id === creatorID) || jobs[0];
  const error = latest?.error || latest?.error_detail || "";
  const externalFailure = latest?.status === "failed" && isExternalBilibiliFailure(error);
  if (latest?.status === "failed" && !externalFailure) {
    fail(`${label} 失败: ${error || "unknown error"}`);
  }
  step(label, { state: status.state, jobStatus: latest?.status || "unknown", addedWorks: latest?.added_works ?? 0 });
  return {
    status,
    latest,
    externalFailure,
    reason: externalFailure ? sanitizeErrorForReport(error) : "",
    feedWorksAfter: await countFeedWorks(api),
  };
}

async function waitForUpdateIdle(api) {
  const started = Date.now();
  let last;
  while (Date.now() - started < config.timeoutMs) {
    const response = await api("/api/update/status");
    last = response.body.status;
    if (!last.running && last.pending_creators === 0) return last;
    await delay(1000);
  }
  fail(`更新任务未在 ${config.timeoutMs}ms 内结束: ${safeJSON(last)}`);
}

async function ensureSampleCoverage(api) {
  await fs.access(config.sampleDir).catch(() => fail(`样例目录不存在: ${config.sampleDir}`));
  let response = await api("/api/directories", {
    method: "POST",
    body: JSON.stringify({ path: config.sampleDir }),
    allowFailure: true,
  });
  let directoryID;
  if (response.status === 201) {
    directoryID = response.body.directory.id;
  } else {
    response = await api("/api/directories");
    const existing = (response.body.directories || []).find((item) => item.path === config.sampleDir);
    assert(existing, "样例目录保存失败且未找到已有目录");
    directoryID = existing.id;
  }
  await api(`/api/directories/${directoryID}/scan`, { method: "POST" });
  await waitForScanCompleted(api);
  step("扫描本地样例兜底数据", { sampleDir: config.sampleDir });
}

async function ensureRecentFeedCoverage(api, page) {
  const root = path.join(config.mediaDir, "recent-feed-e2e");
  const now = Date.now();
  const fixtures = [
    { id: "BVrecentvideo", title: "近期搜索视频", workType: "video_long", publishedAt: new Date(now - 2 * 86400000).toISOString(), files: [{ type: "video", file: "recent.mp4", content: "fake recent video\n" }] },
    { id: "recent-image-text", title: "近期搜索图文", workType: "image_text", publishedAt: new Date(now - 6 * 86400000).toISOString(), files: [{ type: "image", file: "recent.jpg", content: "fake recent image\n" }] },
  ];
  for (const fixture of fixtures) {
    const creatorDir = path.join(root, "近期验收UP主__mid_900001");
    const dir = path.join(creatorDir, fixture.id);
    await fs.mkdir(dir, { recursive: true });
    await fs.writeFile(path.join(dir, "metadata.localbilibili.json"), JSON.stringify({
      work_type: fixture.workType, bvid: fixture.workType === "video_long" ? fixture.id : "", dynamic_id: fixture.workType === "image_text" ? fixture.id : "",
      title: fixture.title, description: fixture.title, published_at: fixture.publishedAt,
      source_url: "https://example.invalid/" + fixture.id,
      creator: { mid: "900001", name: "近期验收UP主", space_url: "https://space.bilibili.com/900001" },
      media: fixture.files.map(({ type, file }) => ({ type, file })),
    }));
    for (const file of fixture.files) {
      const destination = path.join(dir, file.file);
      if (file.type === "video") {
        await fs.copyFile(path.join(config.sampleDir, "video_single", "2009-09-09_字幕君交流场所_BV1xx411c7mD", "media_001.mp4"), destination);
      } else {
        await fs.writeFile(destination, file.content);
      }
    }
  }
  let response = await api("/api/directories", { method: "POST", body: JSON.stringify({ path: root }), allowFailure: true });
  const directoryID = response.status === 201 ? response.body.directory.id : (await api("/api/directories")).body.directories.find((item) => item.path === root)?.id;
  assert(directoryID, "近期验收目录保存失败");
  await api(`/api/directories/${directoryID}/scan`, { method: "POST" });
  await waitForScanCompleted(api);
  const threeDays = await api("/api/feed?recent_mode=three_days&order=desc&limit=20");
  const week = await api("/api/feed?recent_mode=week&order=desc&limit=20");
  const works = await api("/api/feed?recent_mode=works&order=desc&limit=20");
  const cookies = await page.context().cookies(config.baseURL);
  const invalid = await fetch(`${config.baseURL}/api/feed?recent_mode=invalid`, { headers: { Cookie: cookies.map((cookie) => `${cookie.name}=${cookie.value}`).join("; ") } });
  assert(invalid.status === 400, "非法 recent_mode 未返回 400");
  assert((threeDays.body.works || []).some((work) => work.title === "近期搜索视频"), "最近三天缺少近期视频");
  assert(!(threeDays.body.works || []).some((work) => work.title === "近期搜索图文"), "最近三天混入一周作品");
  assert((week.body.works || []).some((work) => work.title === "近期搜索图文"), "最近一周缺少近期图文");
  assert((works.body.works || []).some((work) => work.title === "近期搜索视频"), "最近作品缺少近期视频");
  step("真实服务近期内容 API：三天、一周、最近作品与非法参数", { directory: root });
  return { videoTitle: "近期搜索视频", imageTitle: "近期搜索图文" };
}

async function waitForScanCompleted(api) {
  const started = Date.now();
  let last;
  while (Date.now() - started < 30000) {
    const response = await api("/api/scan/status");
    last = response.body.status;
    if (!last.running && last.state === "completed") return last;
    if (!last.running && last.state === "failed") fail(`扫描失败: ${safeJSON(last)}`);
    await delay(300);
  }
  fail(`扫描未在 30000ms 内完成: ${safeJSON(last)}`);
}

async function assertFeedCoverage(api) {
  const response = await api("/api/feed?limit=50&order=desc");
  const works = response.body.works || [];
  const types = new Set(works.map((work) => work.work_type));
  for (const required of ["video_long", "video_short", "image_text"]) {
    assert(types.has(required), `Feed 缺少 ${required}`);
  }
  const video = works.find((work) => ["video_long", "video_short"].includes(work.work_type) && work.media?.some((media) => media.type === "video"));
  const imageText = works.find((work) => work.work_type === "image_text" && (work.media || []).filter((media) => media.type === "image").length >= 1);
  assert(video, "Feed 缺少可播放视频");
  assert(imageText, "Feed 缺少图文作品");
  const mediaID = video.media.find((media) => media.type === "video").id;
  const mediaResponse = await api(`/api/media/${mediaID}`, { headers: { Range: "bytes=0-10" } });
  assert(mediaResponse.status === 206, `视频 Range 未返回 206: ${mediaResponse.status}`);

  const byCreator = await api(`/api/feed?search=${encodeURIComponent(video.creator_name)}&limit=20`);
  assert((byCreator.body.works || []).some((work) => work.creator_name === video.creator_name), "按 UP主搜索没有命中作品");
  const byTitle = await api(`/api/feed?search=${encodeURIComponent(imageText.title)}&limit=20`);
  assert((byTitle.body.works || []).some((work) => work.id === imageText.id), "按作品标题搜索没有命中作品");
  await api("/api/feed?order=asc&limit=10");
  await api("/api/feed?order=desc&limit=10");
  await api("/api/feed?order=random&limit=10");

  step("Feed 覆盖长视频、短视频、图文、搜索、排序和 Range");
  return { works, video, imageText, creator: { id: video.creator_id, name: video.creator_name } };
}

async function runFavoriteFlow(api, feed) {
  let response = await api("/api/favorite-folders?type=work");
  const defaultWorkFolder = (response.body.folders || []).find((folder) => folder.is_default);
  assert(defaultWorkFolder, "缺少默认作品收藏夹");
  response = await api("/api/favorites/works", {
    method: "POST",
    body: JSON.stringify({ work_id: feed.video.id }),
  });
  assert(response.body.status?.work_favorited, "收藏作品到默认收藏夹后状态未更新");
  await api("/api/favorites/works", {
    method: "POST",
    body: JSON.stringify({ work_id: feed.video.id }),
  });
  response = await api(`/api/favorites/works?folder_id=${defaultWorkFolder.id}`);
  assert((response.body.works || []).filter((work) => work.id === feed.video.id).length === 1, "默认作品收藏夹出现重复收藏");
  await api(`/api/favorites/works/${feed.video.id}?folder_id=${defaultWorkFolder.id}`, { method: "DELETE" });
  response = await api(`/api/favorites/works?folder_id=${defaultWorkFolder.id}`);
  assert(!(response.body.works || []).some((work) => work.id === feed.video.id), "取消默认作品收藏后仍存在");

  response = await api("/api/favorite-folders", {
    method: "POST",
    body: JSON.stringify({ type: "work", name: uniqueName("稍后看") }),
  });
  const customWorkFolder = response.body.folder;
  await api("/api/favorites/works", {
    method: "POST",
    body: JSON.stringify({ work_id: feed.imageText.id, folder_id: customWorkFolder.id }),
  });
  response = await api(`/api/favorites/works?folder_id=${customWorkFolder.id}`);
  assert((response.body.works || []).length === 1 && response.body.works[0].id === feed.imageText.id, "自定义作品收藏夹内容不正确");

  response = await api("/api/favorite-folders?type=creator");
  const defaultCreatorFolder = (response.body.folders || []).find((folder) => folder.is_default);
  assert(defaultCreatorFolder, "缺少默认作者收藏夹");
  await api("/api/favorites/creators", {
    method: "POST",
    body: JSON.stringify({ creator_id: feed.creator.id }),
  });
  await api("/api/favorites/creators", {
    method: "POST",
    body: JSON.stringify({ creator_id: feed.creator.id }),
  });
  response = await api(`/api/favorites/creators?folder_id=${defaultCreatorFolder.id}`);
  assert((response.body.creators || []).filter((creator) => creator.id === feed.creator.id).length === 1, "默认作者收藏夹出现重复收藏");
  await api(`/api/favorites/creators/${feed.creator.id}?folder_id=${defaultCreatorFolder.id}`, { method: "DELETE" });
  response = await api(`/api/favorites/creators?folder_id=${defaultCreatorFolder.id}`);
  assert(!(response.body.creators || []).some((creator) => creator.id === feed.creator.id), "取消默认作者收藏后仍存在");

  response = await api("/api/favorite-folders", {
    method: "POST",
    body: JSON.stringify({ type: "creator", name: uniqueName("重点UP主") }),
  });
  const customCreatorFolder = response.body.folder;
  await api("/api/favorites/creators", {
    method: "POST",
    body: JSON.stringify({ creator_id: feed.creator.id, folder_id: customCreatorFolder.id }),
  });
  response = await api(`/api/favorites/creators?folder_id=${customCreatorFolder.id}`);
  assert((response.body.creators || []).some((creator) => creator.id === feed.creator.id), "自定义作者收藏夹未包含目标 UP主");
  step("作品收藏和作者收藏：默认、自定义、取消、去重");
}

async function runQueueFlow(api, creator) {
  const name = uniqueName("真实用户测试队列");
  let response = await api("/api/update/queues", {
    method: "POST",
    body: JSON.stringify({ name, cron: "0 3 * * *" }),
  });
  const queue = response.body.queue;
  assert(queue.id > 0, "创建定时更新队列失败");
  await api(`/api/update/queues/${queue.id}/creators`, {
    method: "POST",
    body: JSON.stringify({ creator_ids: [creator.id] }),
  });
  response = await api(`/api/update/queues/${queue.id}/creators`);
  assert((response.body.creators || []).some((item) => item.id === creator.id), "定时队列未添加目标 UP主");
  response = await api(`/api/update/queues/${queue.id}`, {
    method: "PATCH",
    body: JSON.stringify({ name: `${name}-已编辑`, cron: "30 3 * * *", enabled: false }),
  });
  assert(response.body.queue.name.endsWith("已编辑") && response.body.queue.enabled === false, "编辑定时队列未持久化");
  step("定时更新队列创建、添加 UP主、编辑和禁用");
}

async function runMultiUserFlow(page, api, feed) {
  let response = await api("/api/users", {
    method: "POST",
    body: JSON.stringify({ username: "alice", password: "alice123456" }),
    allowFailure: true,
  });
  let alice = response.body?.user;
  if (!alice) {
    response = await api("/api/users");
    alice = (response.body.users || []).find((user) => user.username === "alice");
    assert(alice, "无法创建或找到 alice 用户");
    await api(`/api/users/${alice.id}/reset-password`, {
      method: "POST",
      body: JSON.stringify({ password: "alice123456" }),
    });
  }

  await api("/api/users/view-context", {
    method: "PUT",
    body: JSON.stringify({ user_id: alice.id }),
  });
  await api("/api/favorites/works", {
    method: "POST",
    body: JSON.stringify({ work_id: feed.video.id }),
  });
  response = await api(`/api/favorites/status?work_id=${feed.video.id}`);
  assert(response.body.status?.work_favorited, "alice 视角收藏作品失败");
  await api("/api/users/view-context", { method: "DELETE" });
  response = await api(`/api/favorites/status?work_id=${feed.video.id}`);
  assert(!response.body.status?.work_favorited, "ted 与 alice 的作品收藏未隔离");

  const browser = page.context().browser();
  const aliceContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const alicePage = await aliceContext.newPage();
  try {
    await login(alicePage, "alice", "alice123456");
    const aliceAPI = apiClient(alicePage);
    response = await aliceAPI("/api/update/status", { allowFailure: true });
    assert(response.status === 403, "普通用户不应访问更新管理 API");
    response = await aliceAPI("/api/creators/bulk-import/preview", {
      method: "POST",
      body: JSON.stringify({ directory_id: 1, text: config.realCreatorURL }),
      allowFailure: true,
    });
    assert(response.status === 403, "普通用户不应访问批量新增关注 API");
  } finally {
    await aliceContext.close();
  }
  step("多用户收藏隔离和普通用户权限限制");
}

async function runBrowserFlow(page, feed, recent) {
  await page.goto(`${config.baseURL}/`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("text=LocalBilibili", { timeout: 10000 });
  await page.getByLabel("作品播放器").hover();
  await page.getByLabel("打开搜索").click();
  await page.getByLabel("搜索本地作品").fill(recent.videoTitle);
  await page.getByRole("button", { name: "搜索" }).click();
  assert(new URL(page.url()).pathname === "/search", "Feed 搜索未进入独立结果页");
  assert(new URL(page.url()).searchParams.get("q") === recent.videoTitle, "Feed 搜索未保留关键词");
  await page.waitForSelector(`text=${recent.videoTitle}`, { timeout: 10000 });
  await page.getByRole("link", { name: new RegExp(recent.videoTitle) }).click();
  assert(new URL(page.url()).pathname === "/search/play", "独立搜索视频未进入搜索播放流");
  await page.goto(`${config.baseURL}/search?q=${encodeURIComponent("近期搜索")}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("link", { name: new RegExp(recent.imageTitle) }).click();
  assert(new URL(page.url()).pathname === "/search/play", "独立搜索图文未进入搜索播放流");
  for (const [route, expectedTitle] of [["/recent-3-days", recent.videoTitle], ["/recent-week", recent.imageTitle], ["/recent-works", recent.videoTitle]]) {
    const responsePromise = page.waitForResponse((response) => response.url().includes("/api/feed?") && response.request().method() === "GET");
    await page.goto(`${config.baseURL}${route}`, { waitUntil: "domcontentloaded" });
    const response = await responsePromise;
    const payload = await response.json();
    assert((payload.works || []).some((work) => work.title === expectedTitle), `${route} 浏览器 Feed 未返回 ${expectedTitle}`);
  }
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowUp");
  await page.screenshot({ path: path.join(rootDir, "output/playwright/real-user-feed-desktop.png"), fullPage: true });
  report.artifacts.push("output/playwright/real-user-feed-desktop.png");

  await page.goto(`${config.baseURL}/creator/${feed.creator.id}`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector(`text=${feed.creator.name}`, { timeout: 10000 });
  await page.goto(`${config.baseURL}/favorite-works`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("text=作品收藏夹", { timeout: 10000 });
  await page.goto(`${config.baseURL}/favorite-creators`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("text=作者收藏夹", { timeout: 10000 });
  await page.goto(`${config.baseURL}/updates`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("text=更新管理", { timeout: 10000 });
  await page.goto(`${config.baseURL}/settings`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("text=设置", { timeout: 10000 });

  for (const route of ["/", "/search?q=%E8%BF%91%E6%9C%9F%E6%90%9C%E7%B4%A2", "/creators", "/updates", "/favorite-works", "/favorite-creators", "/settings"]) {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${config.baseURL}${route}`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector("body", { timeout: 10000 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
    assert(!overflow, `移动端 ${route} 出现横向溢出`);
  }
  await page.screenshot({ path: path.join(rootDir, "output/playwright/real-user-mobile.png"), fullPage: true });
  report.artifacts.push("output/playwright/real-user-mobile.png");
  step("真实浏览器搜索、近期入口、页面、UP主详情、收藏页、更新页、设置页和移动端布局");
}

async function runDiagnosticsAndSecurity(api) {
  let response = await api("/api/diagnostics");
  assert(response.body.diagnostics, "诊断接口缺少 diagnostics");
  response = await api("/api/logs/audit?page=1&page_size=50");
  assert(Array.isArray(response.body.events), "审计日志接口未返回 events");
  if (config.bilibiliCookie) {
    assert(!containsSensitive(response.body, config.bilibiliCookie), "审计日志泄露 Cookie 片段");
  }
  response = await api("/api/logs/system?page=1&page_size=50");
  assert(Array.isArray(response.body.events), "系统日志接口未返回 events");
  if (config.bilibiliCookie) {
    assert(!containsSensitive(response.body, config.bilibiliCookie), "系统日志泄露 Cookie 片段");
    await assertDownloadedLayoutDoesNotLeakCookie(config.mediaDir);
  }
  step("诊断、日志和 Cookie 脱敏检查");
}

async function assertDownloadedLayoutDoesNotLeakCookie(dir) {
  const sourceFiles = await collectFiles(dir, (file) => /source\..*\.json$|metadata\.localbilibili\.json$/.test(path.basename(file)));
  if (sourceFiles.length === 0) return;
  for (const file of sourceFiles) {
    const content = await fs.readFile(file, "utf8");
    assert(!containsSensitive(content, config.bilibiliCookie), `下载元数据泄露 Cookie 片段: ${path.relative(rootDir, file)}`);
  }
}

async function collectFiles(dir, predicate) {
  const result = [];
  async function walk(current) {
    let entries = [];
    try {
      entries = await fs.readdir(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        await walk(full);
      } else if (predicate(full)) {
        result.push(full);
      }
    }
  }
  await walk(dir);
  return result;
}

async function assertNoSeriousConsoleErrors(consoleErrors) {
  const serious = consoleErrors.filter((line) => !/status of 401|status of 404|favicon|React Router Future Flag|ResizeObserver/.test(line));
  assert(serious.length === 0, `浏览器控制台存在严重错误: ${serious.join("\n")}`);
  step("浏览器控制台无严重错误");
}

async function countFeedWorks(api) {
  const response = await api("/api/feed?limit=100");
  return (response.body.works || []).length;
}

function containsSensitive(value, cookie) {
  if (!cookie) return false;
  const text = typeof value === "string" ? value : JSON.stringify(value);
  return sensitiveNeedles(cookie).some((needle) => text.includes(needle));
}

function sensitiveNeedles(cookie) {
  return cookie
    .split(";")
    .map((part) => part.trim().split("=").slice(1).join("="))
    .filter((value) => value.length >= 12);
}

function isExternalBilibiliFailure(error) {
  const value = String(error || "").toLowerCase();
  return /412|403|429|captcha|风控|反爬|forbidden|too many|timeout|timed out|network|bilibili/.test(value);
}

function sanitizeErrorForReport(error) {
  return String(error || "")
    .replace(/SESSDATA=[^;\s"]+/g, "SESSDATA=<redacted>")
    .replace(/bili_jct=[^;\s"]+/g, "bili_jct=<redacted>")
    .slice(0, 240);
}

function safeJSON(value) {
  const text = JSON.stringify(value);
  return config.bilibiliCookie ? text.replaceAll(config.bilibiliCookie, "<redacted-cookie>") : text;
}

function uniqueName(prefix) {
  return `${prefix}-${Date.now().toString(36)}`;
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function writeReport() {
  const reportDir = path.join(rootDir, "output/real-user-e2e");
  await fs.mkdir(reportDir, { recursive: true });
  const reportPath = path.join(reportDir, "latest.json");
  await fs.writeFile(reportPath, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(`Report: ${path.relative(rootDir, reportPath)}`);
}

main().catch(async (error) => {
  report.failed = true;
  report.error = sanitizeErrorForReport(error.stack || error.message || String(error));
  try {
    await writeReport();
  } catch {
    // Ignore report write failure during fatal handling.
  }
  if (serverProcess) await stopServer(serverProcess);
  console.error(report.error);
  process.exit(1);
});
