import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { App } from "./App";

let authState: "guest" | "authed" = "authed";
let currentUser = {
  id: 1,
  username: "ted",
  is_admin: true,
  is_super_admin: true,
  role: "super_admin",
  created_at: "",
  updated_at: "",
};
let eventSourceURLs: string[] = [];

beforeEach(() => {
  authState = "authed";
  eventSourceURLs = [];
  currentUser = {
    id: 1,
    username: "ted",
    is_admin: true,
    is_super_admin: true,
    role: "super_admin",
    created_at: "",
    updated_at: "",
  };
  window.history.pushState({}, "", "/settings");
  vi.stubGlobal(
    "EventSource",
    class {
      constructor(url: string | URL) {
        eventSourceURLs.push(String(url));
      }
      close() {}
      addEventListener() {}
      removeEventListener() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/api/auth/me")) {
        return authState === "authed"
          ? response({ user: currentUser })
          : response({ error: "unauthorized" }, 401);
      }
      if (url.includes("/api/auth/login")) {
        authState = "authed";
        currentUser = {
          id: 1,
          username: "ted",
          is_admin: true,
          is_super_admin: true,
          role: "super_admin",
          created_at: "",
          updated_at: "",
        };
        return response({ user: currentUser });
      }
      if (url.includes("/api/auth/logout")) {
        authState = "guest";
        return response({ ok: true });
      }
      if (url.includes("/api/users/view-context")) {
        return response({ actor_user: currentUser, view_user: currentUser });
      }
      if (url.includes("/api/about")) {
        return response({
          about: {
            name: "LocalBilibili",
            version: "dev",
            description: "局域网版本地B站作品浏览 Web 应用",
          },
        });
      }
      if (url.includes("/api/logs/audit"))
        return response({
          events: [],
          page: 1,
          page_size: 20,
          total: 0,
          total_pages: 0,
        });
      if (url.includes("/api/logs/system"))
        return response({
          events: [],
          page: 1,
          page_size: 20,
          total: 0,
          total_pages: 0,
        });
      if (url.includes("/api/diagnostics"))
        return response({
          diagnostics: {
            about: {
              name: "LocalBilibili",
              version: "dev",
              description: "局域网版本地B站作品浏览 Web 应用",
            },
            current_user: currentUser,
            current_view_user: currentUser,
            database: {
              directory: "/tmp",
              path: "/tmp/localbilibili.db",
              exists: true,
              writable: true,
            },
            directories: [],
            cookie: { configured: false, validation_status: "unknown" },
            scan_status: { running: false, progress: 0, errors: [] },
            update_status: {
              running: false,
              pending_creators: 0,
              total_creators: 0,
              succeeded_creators: 0,
              failed_creators: 0,
              added_works: 0,
              progress: 0,
              state: "idle",
              errors: [],
              jobs: [],
            },
            playback: {
              total_events: 0,
              total_views: 0,
              total_errors: 0,
              top_viewed_creator: "",
              last_viewed_at: "",
              top_creators: [],
              top_problem_works: [],
              recent_repair_warnings: [],
              recent_events: [],
            },
            accessible_roots: [],
          },
        });
      if (url.includes("/api/config/database"))
        return response({
          database: {
            directory: "/tmp",
            path: "/tmp/localbilibili.db",
            exists: true,
            writable: true,
          },
        });
      if (url.includes("/api/config/runtime"))
        return response({ runtime: { downloader_concurrency: 1 } });
      if (url === "/api/directories" && init?.method === "POST")
        return response(
          {
            directory: {
              id: 2,
              path: "/tmp/new-bilibili",
              name: "new-bilibili",
              status: "idle",
              creator_count: 0,
              work_count: 0,
              created_at: "",
            },
          },
          201,
        );
      if (url.includes("/api/directories"))
        return response({
          directories: [
            {
              id: 1,
              path: "/tmp/bilibili",
              name: "bilibili",
              status: "idle",
              creator_count: 2,
              work_count: 8,
              created_at: "",
            },
          ],
        });
      if (url.includes("/api/scan/status"))
        return response({
          status: { running: false, progress: 0, errors: [] },
        });
      if (url.includes("/api/fs/roots"))
        return response({ roots: [{ path: "/tmp", name: "tmp" }] });
      if (url.includes("/api/fs/list"))
        return response({
          entries: [{ path: "/tmp/new-bilibili", name: "new-bilibili" }],
        });
      if (url.includes("/api/creators/bulk-import/preview"))
        return response({
          items: [
            {
              input: "https://www.bilibili.com/user/MS4wLjABAAAA_TEST?x=1",
              bilibili_space_url: "https://www.bilibili.com/user/MS4wLjABAAAA_TEST",
              bilibili_mid: "MS4wLjABAAAA_TEST",
              status: "ready",
              existing: false,
            },
          ],
        });
      if (url.includes("/api/creators/bulk-import"))
        return response(
          {
            creators: [
              {
                id: 9,
                directory_id: 1,
                name: "AAAA_TEST",
                work_count: 0,
                bilibili_mid: "MS4wLjABAAAA_TEST",
                bilibili_space_url:
                  "https://www.bilibili.com/user/MS4wLjABAAAA_TEST",
                online_identity_status: "ready",
              },
            ],
          },
          201,
        );
      if (url.match(/^\/api\/creators\/\d+$/) && init?.method === "DELETE")
        return response({ ok: true });
      if (url.includes("/api/creators"))
        return response({
          creators: [
            {
              id: 1,
              directory_id: 1,
              name: "阿言",
              work_count: 3,
              online_identity_status: "ready",
            },
            {
              id: 2,
              directory_id: 1,
              name: "徐大川",
              work_count: 5,
              online_identity_status: "missing",
            },
          ],
        });
      if (url.includes("/api/update/status"))
        return response({
          status: {
            running: true,
            current_creator: "全哥",
            pending_creators: 0,
            total_creators: 1,
            succeeded_creators: 0,
            failed_creators: 0,
            added_works: 0,
            progress: 20,
            state: "running",
            errors: [],
            running_jobs: [
              {
                id: 11,
                creator_id: 1,
                creator_name: "全哥",
                status: "running",
                added_works: 0,
                total_works: 10,
                processed_works: 2,
                downloaded_media: 3,
                skipped_works: 1,
                download_speed_bytes_per_second: 2048,
                progress_message: "正在下载视频",
                download_directory: "/tmp/bilibili/全哥",
                created_at: "",
              },
            ],
            jobs: [],
          },
        });
      if (url.includes("/api/update/jobs/") && init?.method === "DELETE")
        return response({ ok: true });
      if (url.includes("/api/update/jobs/") && init?.method === "POST")
        return response({
          status: {
            running: false,
            pending_creators: 1,
            total_creators: 1,
            succeeded_creators: 0,
            failed_creators: 0,
            added_works: 0,
            progress: 0,
            state: "queued",
            errors: [],
            running_jobs: [],
            jobs: [],
          },
        });
      if (url.includes("/api/update/jobs"))
        return response({
          jobs: [
            {
              id: 21,
              creator_id: 1,
              creator_name: "失败UP主",
              status: "failed",
              added_works: 0,
              error: "下载路径非常非常长导致失败",
              attempt: 1,
              trigger_source: "manual",
              started_at: "2026-07-10T01:00:00Z",
              finished_at: "2026-07-10T01:02:00Z",
              created_at: "",
            },
            {
              id: 22,
              creator_id: 2,
              creator_name: "成功UP主",
              status: "succeeded",
              added_works: 3,
              attempt: 1,
              trigger_source: "scheduled",
              started_at: "2026-07-10T03:00:00Z",
              finished_at: "2026-07-10T03:05:00Z",
              created_at: "",
            },
          ],
          page: 1,
          page_size: 10,
          total: 12,
          total_pages: 2,
          summary: {
            pending: 1,
            running: 0,
            succeeded: 5,
            failed: 6,
            added_works: 11,
          },
        });
      if (url.includes("/api/update/immediate/reset"))
        return response({
          status: {
            running: false,
            pending_creators: 0,
            total_creators: 0,
            succeeded_creators: 0,
            failed_creators: 0,
            added_works: 0,
            progress: 0,
            state: "idle",
            errors: [],
            running_jobs: [],
            jobs: [],
          },
        });
      if (
        url.includes("/api/update/queues/1/creators") &&
        init?.method === "DELETE"
      )
        return response({ ok: true });
      if (
        url.includes("/api/update/queues/1/creators") &&
        init?.method === "POST"
      )
        return response({ ok: true });
      if (url.includes("/api/update/queues/1/creators"))
        return response({
          creators: [
            {
              id: 1,
              directory_id: 1,
              name: "阿言",
              work_count: 3,
              online_identity_status: "ready",
            },
          ],
        });
      if (url.includes("/api/update/queues"))
        return response({
          queues: [
            {
              id: 1,
              name: "默认更新队列",
              cron: "0 3 * * *",
              is_default: true,
              enabled: true,
              next_run_at: new Date(Date.now() + 3600000).toISOString(),
              last_run_summary: "",
              subscribed_count: 0,
              created_at: "",
              updated_at: "",
            },
          ],
        });
      if (url.includes("/api/update/cookie/validate"))
        return response({
          ok: false,
          cookie: {
            configured: false,
            validation_status: "invalid",
            validation_message: "cookie is required",
          },
        });
      if (url.includes("/api/update/cookie"))
        return response({
          cookie: {
            configured: true,
            validation_status: "valid",
            value: "msToken=one; ttwid=two",
          },
        });
      if (url.includes("/api/update/immediate"))
        return response(
          {
            status: {
              running: false,
              pending_creators: 1,
              total_creators: 1,
              succeeded_creators: 0,
              failed_creators: 0,
              added_works: 0,
              progress: 0,
              state: "queued",
              errors: [],
              running_jobs: [],
              jobs: [],
            },
          },
          202,
        );
      if (url.includes("/api/feed"))
        return response({
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: "阿言",
              bvid: "1001",
              title: "第一条",
              description: "第一条",
              music_title: "",
              tags: [],
              published_at: "",
              media: [
                {
                  id: 1,
                  work_id: 1,
                  type: "image",
                  file_name: "a.webp",
                  url: "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==",
                  ordinal: 0,
                },
              ],
            },
          ],
        });
      if (url.includes("/api/favorite-folders"))
        return response({ folders: [] });
      if (url.includes("/api/favorites/status"))
        return response({
          status: { work_folder_ids: [], creator_folder_ids: [] },
        });
      if (url.includes("/api/users"))
        return response({
          users: [
            currentUser,
            {
              id: 2,
              username: "alice",
              is_admin: false,
              role: "user",
              created_at: "",
              updated_at: "",
            },
          ],
        });
      if (url.includes("/api/auth/password"))
        return response({ user: currentUser });
      return response({ ok: true });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it("shows the login page before auth and enters the app after login", async () => {
  authState = "guest";
  const user = userEvent.setup();
  render(<App />);
  expect(
    await screen.findByRole("heading", { name: "登录 LocalBilibili" }),
  ).toBeInTheDocument();
  await user.type(screen.getByLabelText("用户名"), "ted");
  await user.type(screen.getByLabelText("密码"), "ted");
  await user.click(screen.getByRole("button", { name: "登录" }));
  await waitFor(() => expect(screen.getByText("目录配置")).toBeInTheDocument());
  expect(screen.getByTitle("首页")).toBeInTheDocument();
});

it("routes user management to its own menu page", async () => {
  const user = userEvent.setup();
  render(<App />);
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(screen.queryByText("我的账户")).not.toBeInTheDocument();
  expect(screen.queryByText("账户管理")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("用户管理"));
  expect(
    await screen.findByRole("heading", { name: "用户管理" }),
  ).toBeInTheDocument();
  expect(screen.getByText("我的账户")).toBeInTheDocument();
  expect(screen.getByText("账户列表")).toBeInTheDocument();
});

it("keeps one realtime EventSource while navigating primary pages", async () => {
  const user = userEvent.setup();
  render(<App />);
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(eventSourceURLs).toEqual(["/api/events"]);

  await user.click(screen.getByTitle("首页"));
  expect(await screen.findByText(/第一条/)).toBeInTheDocument();
  expect(eventSourceURLs).toEqual(["/api/events"]);

  await user.click(screen.getByTitle("关注"));
  expect(await screen.findByText("阿言")).toBeInTheDocument();
  expect(eventSourceURLs).toEqual(["/api/events"]);

  await user.click(screen.getByTitle("更新管理"));
  expect(await screen.findByRole("heading", { name: "更新管理" })).toBeInTheDocument();
  expect(eventSourceURLs).toEqual(["/api/events"]);

  await user.click(screen.getByTitle("设置"));
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(eventSourceURLs).toEqual(["/api/events"]);
});

it("shows only self password management to normal users", async () => {
  currentUser = {
    id: 2,
    username: "alice",
    is_admin: false,
    is_super_admin: false,
    role: "user",
    created_at: "",
    updated_at: "",
  };
  window.history.pushState({}, "", "/users");
  render(<App />);
  expect(
    await screen.findByRole("heading", { name: "用户管理" }),
  ).toBeInTheDocument();
  expect(screen.getByText("我的账户")).toBeInTheDocument();
  expect(screen.queryByText("账户列表")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("新用户名")).not.toBeInTheDocument();
});

it("shows update management only to admins and exposes queue actions", async () => {
  const user = userEvent.setup();
  render(<App />);
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  await user.click(screen.getByTitle("更新管理"));
  expect(
    await screen.findByRole("heading", { name: "更新管理" }),
  ).toBeInTheDocument();
  expect(screen.getByText("运行设置")).toBeInTheDocument();
  expect(screen.getByText("立即执行队列")).toBeInTheDocument();
  expect(screen.getByText("正在下载视频")).toBeInTheDocument();
  expect(screen.getByText("2.0 KB/s")).toBeInTheDocument();
  expect(screen.getAllByText("失败UP主").length).toBeGreaterThan(0);
  expect(screen.getAllByText("成功UP主").length).toBeGreaterThan(0);
  expect(screen.getByText(/人工触发/)).toBeInTheDocument();
  expect(screen.getAllByText(/定时更新/).length).toBeGreaterThan(0);
  expect(screen.getAllByText(/触发 .*结束/).length).toBeGreaterThan(0);
  expect(screen.getByText("第 1 / 2 页 · 共 12 条")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "按失败UP主过滤更新记录" }));
  await waitFor(() =>
    expect(
      vi.mocked(fetch).mock.calls.some(([url]) =>
        String(url).includes("status=failed"),
      ),
    ).toBe(true),
  );
  expect(screen.getByText("Cookie 管理")).toBeInTheDocument();
  expect(
    screen.getByDisplayValue("msToken=one; ttwid=two"),
  ).toBeInTheDocument();
  await user.click(screen.getAllByRole("button", { name: "删除" })[0]);
  expect(await screen.findByText("更新记录已删除")).toBeInTheDocument();
  vi.spyOn(window, "confirm").mockReturnValueOnce(true);
  await user.click(screen.getByRole("button", { name: "重置" }));
  expect(await screen.findByText("立即执行队列已重置")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "编辑" }));
  const dialog = await screen.findByRole("dialog", { name: "编辑定时队列" });
  expect(within(dialog).getByRole("button", { name: "关闭弹窗" })).toBeInTheDocument();
  expect(within(dialog).getByLabelText("队列名称")).toHaveValue("默认更新队列");
  expect(within(dialog).getByText("阿言")).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: /添加/ }));
  await user.click(within(dialog).getByRole("button", { name: /移除/ }));
  await user.click(within(dialog).getByRole("button", { name: "关闭弹窗" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: "编辑定时队列" })).not.toBeInTheDocument(),
  );
  await user.click(screen.getByRole("button", { name: "浏览器获取 Cookie" }));
  expect(
    screen.getByText(/fnOS Web 环境不支持稳定自动读取/),
  ).toBeInTheDocument();
  expect(screen.getByText(/msToken=.*ttwid=.*odin_tt=/)).toBeInTheDocument();
});

it("routes diagnostics to its own primary menu page", async () => {
  const user = userEvent.setup();
  render(<App />);
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(screen.queryByText("统一设置 / 诊断中心")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("诊断中心"));
  expect(
    await screen.findByRole("heading", { name: "诊断中心" }),
  ).toBeInTheDocument();
  expect(screen.getByText("统一设置 / 诊断中心")).toBeInTheDocument();
});

it("adds creator update actions for admins and hides updates for normal users", async () => {
  const user = userEvent.setup();
  window.history.pushState({}, "", "/creators");
  const view = render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();
  expect(document.querySelector(".creator-list")).toBeInTheDocument();
  expect(document.querySelector(".creator-grid")).not.toBeInTheDocument();
  await user.click(screen.getAllByRole("button", { name: "立即更新" })[1]);
  expect(
    await screen.findByText("已加入立即更新队列：1 个UP主"),
  ).toBeInTheDocument();
  await user.click(screen.getAllByRole("button", { name: "订阅更新" })[0]);
  const subscribeDialog = await screen.findByRole("dialog", { name: "订阅更新" });
  expect(within(subscribeDialog).getByRole("button", { name: "关闭弹窗" })).toBeInTheDocument();
  expect(screen.getByText("默认更新队列")).toBeInTheDocument();
  await user.keyboard("{Escape}");
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: "订阅更新" })).not.toBeInTheDocument(),
  );
  view.unmount();

  vi.unstubAllGlobals();
  currentUser = {
    id: 2,
    username: "alice",
    is_admin: false,
    is_super_admin: false,
    role: "user",
    created_at: "",
    updated_at: "",
  };
  vi.stubGlobal(
    "EventSource",
    class {
      close() {}
      addEventListener() {}
      removeEventListener() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/auth/me")) return response({ user: currentUser });
      if (url.includes("/api/creators"))
        return response({
          creators: [{ id: 1, directory_id: 1, name: "阿言", work_count: 3 }],
        });
      return response({ ok: true });
    }),
  );
  window.history.pushState({}, "", "/creators");
  render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();
  expect(screen.queryByTitle("更新管理")).not.toBeInTheDocument();
});

it("confirms creator deletion and can request media deletion", async () => {
  const user = userEvent.setup();
  window.history.pushState({}, "", "/creators");
  render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();
  const actions = document.querySelector(".creator-update-actions");
  expect(actions).toHaveClass("creator-update-actions-two-rows");

  await user.click(screen.getAllByRole("button", { name: "删除" })[0]);
  const dialog = await screen.findByRole("dialog", { name: "删除UP主" });
  expect(within(dialog).getByText(/确定删除UP主「阿言」/)).toBeInTheDocument();
  expect(within(dialog).getByLabelText("删除媒体数据")).not.toBeChecked();
  await user.click(within(dialog).getByLabelText("删除媒体数据"));
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));

  await waitFor(() =>
    expect(
      vi.mocked(fetch).mock.calls.some(([input, init]) => {
        return (
          String(input) === "/api/creators/1" &&
          init?.method === "DELETE" &&
          init.body === JSON.stringify({ delete_media: true })
        );
      }),
    ).toBe(true),
  );
  expect(await screen.findByText("已删除UP主：阿言")).toBeInTheDocument();
});

it("bulk imports creators from pasted bilibili links and can enqueue imported creators", async () => {
  const user = userEvent.setup();
  window.history.pushState({}, "", "/creators");
  render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "批量新增关注" }));
  const dialog = await screen.findByRole("dialog", { name: "批量新增关注" });
  expect(within(dialog).getByRole("button", { name: "关闭弹窗" })).toBeInTheDocument();
  expect(within(dialog).getByLabelText("保存目录路径")).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: /浏览/ }));
  expect(await screen.findByText("选择目录")).toBeInTheDocument();
  await user.click(screen.getByText("tmp"));
  await user.click(screen.getByText("选择此目录"));
  expect(within(dialog).getByLabelText("保存目录路径")).toHaveValue("/tmp");
  await user.clear(within(dialog).getByLabelText("保存目录路径"));
  await user.type(
    within(dialog).getByLabelText("保存目录路径"),
    "/tmp/new-bilibili",
  );
  await user.click(within(dialog).getByRole("button", { name: "保存目录" }));
  await waitFor(() =>
    expect(
      within(dialog).getByText(
        (_, node) => node?.textContent?.includes("/tmp/new-bilibili") ?? false,
        {
          selector: ".bulk-selected-directory",
        },
      ),
    ).toBeInTheDocument(),
  );
  await user.type(
    within(dialog).getByLabelText("批量关注文本"),
    "4- 长按复制此条消息，打开B站搜索，查看TA的更多作品。 https://v.bilibili.com/MJ73IlBJI54/ 1@7.com :9pm",
  );
  await user.click(within(dialog).getByRole("button", { name: "解析" }));
  expect(
    await within(dialog).findByText(
      "https://www.bilibili.com/user/MS4wLjABAAAA_TEST",
    ),
  ).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "提交" }));
  expect(
    await within(dialog).findByText(
      "下载完成后会自动入库，可在关注页或 Feed 中查看。",
    ),
  ).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "立即更新" }));
  expect(
    await within(dialog).findByText("已加入立即更新队列：1 个UP主"),
  ).toBeInTheDocument();
  await user.click(
    within(dialog).getByRole("button", { name: "添加到定时更新队列" }),
  );
  const subscribeDialog = await screen.findByRole("dialog", { name: "订阅更新" });
  expect(within(subscribeDialog).getByRole("button", { name: "关闭弹窗" })).toBeInTheDocument();
  await user.click(within(subscribeDialog).getByRole("button", { name: "关闭弹窗" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: "订阅更新" })).not.toBeInTheDocument(),
  );
  await user.click(within(dialog).getByRole("button", { name: "关闭弹窗" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog", { name: "批量新增关注" })).not.toBeInTheDocument(),
  );
});

it("renders a centered vertical login form", async () => {
  authState = "guest";
  window.history.pushState({}, "", "/users");
  const { container } = render(<App />);
  expect(
    await screen.findByRole("heading", { name: "登录 LocalBilibili" }),
  ).toBeInTheDocument();
  const card = container.querySelector(".login-card");
  const form = container.querySelector(".login-form");
  expect(card).toBeInTheDocument();
  expect(form).toBeInTheDocument();
  expect(
    within(form as HTMLElement).getByLabelText("用户名"),
  ).toBeInTheDocument();
  expect(
    within(form as HTMLElement).getByLabelText("密码"),
  ).toBeInTheDocument();
  expect(screen.getByText("忘记密码请联系管理员重置。")).toBeInTheDocument();
});

it("hides and restores the desktop sidebar without removing navigation", async () => {
  const user = userEvent.setup();
  const { container } = render(<App />);
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(container.querySelector(".app-shell")).not.toHaveClass(
    "sidebar-collapsed",
  );
  await user.click(screen.getByLabelText("隐藏菜单"));
  expect(container.querySelector(".app-shell")).toHaveClass(
    "sidebar-collapsed",
  );
  expect(screen.getByLabelText("展开菜单")).toBeInTheDocument();
  expect(screen.getByTitle("首页")).toBeInTheDocument();
  await user.click(screen.getByLabelText("展开菜单"));
  expect(container.querySelector(".app-shell")).not.toHaveClass(
    "sidebar-collapsed",
  );
  expect(screen.getByTitle("关注")).toBeInTheDocument();
});

it("uses a single close control for the mobile sidebar", async () => {
  const user = userEvent.setup();
  window.history.pushState({}, "", "/creators");
  const { container } = render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();

  await user.click(screen.getByLabelText("打开菜单"));
  expect(screen.queryByLabelText("打开菜单")).not.toBeInTheDocument();
  expect(screen.getAllByLabelText("关闭菜单")).toHaveLength(1);

  await user.click(screen.getByLabelText("关闭菜单"));
  await waitFor(() =>
    expect(screen.getByLabelText("打开菜单")).toBeInTheDocument(),
  );
  expect(container.querySelector(".app-shell")).not.toHaveClass(
    "mobile-nav-open",
  );
});

it("keeps creator action grid responsive on the creators page", async () => {
  window.history.pushState({}, "", "/creators");
  render(<App />);
  expect(await screen.findByText("阿言")).toBeInTheDocument();

  const actions = document.querySelector(
    ".creator-update-actions.creator-update-actions-two-rows",
  );
  expect(actions).toBeInTheDocument();
  expect(actions).toHaveClass("creator-update-actions-two-rows");

  const buttons = within(actions as HTMLElement).getAllByRole("button");
  expect(buttons.map((button) => button.textContent)).toEqual(
    expect.arrayContaining([
      expect.stringContaining("立即更新"),
      expect.stringContaining("订阅更新"),
      expect.stringContaining("立即转字幕"),
      expect.stringContaining("获取字幕"),
      expect.stringContaining("删除"),
    ]),
  );
});

function response(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}
