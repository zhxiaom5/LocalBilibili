import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { SettingsPage } from "./SettingsPage";

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SettingsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/directories" && init?.method === "POST") {
        return Response.json(
          {
            directory: {
              id: 1,
              path: "/tmp/bilibili",
              name: "bilibili",
              status: "idle",
              creator_count: 0,
              work_count: 0,
              created_at: "",
            },
          },
          { status: 201 },
        );
      }
      if (url === "/api/directories") {
        return Response.json({
          directories: [
            {
              id: 1,
              path: "/tmp/bilibili",
              name: "bilibili",
              status: "idle",
              creator_count: 1,
              work_count: 2,
              created_at: "",
            },
          ],
        });
      }
      if (url === "/api/scan/status") {
        return Response.json({
          status: {
            running: true,
            queue_length: 0,
            current_directory: "/tmp/bilibili",
            current_creator: "阿言",
            scanned_creators: 1,
            total_creators: 4,
            scanned_works: 2,
            progress: 25,
            state: "scanning",
            errors: [],
          },
        });
      }
      if (url === "/api/config/database") {
        return Response.json({
          database: {
            directory: "/tmp",
            path: "/tmp/localbilibili.db",
            exists: true,
            writable: true,
          },
        });
      }
      if (url === "/api/diagnostics") {
        return Response.json({
          diagnostics: {
            about: { name: "LocalBilibili", version: "test", description: "test" },
            current_user: {
              id: 1,
              username: "ted",
              is_admin: true,
              is_super_admin: true,
              created_at: "",
              updated_at: "",
            },
            current_view_user: {
              id: 1,
              username: "ted",
              is_admin: true,
              is_super_admin: true,
              created_at: "",
              updated_at: "",
            },
            database: {
              directory: "/tmp",
              path: "/tmp/localbilibili.db",
              exists: true,
              writable: true,
            },
            directories: [],
            cookie: { configured: false, validation_status: "unknown" },
            scan_status: {
              running: true,
              queue_length: 0,
              current_directory: "/tmp/bilibili",
              current_creator: "阿言",
              scanned_creators: 1,
              total_creators: 4,
              scanned_works: 2,
              progress: 25,
              state: "scanning",
              errors: [],
            },
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
            accessible_roots: [{ path: "/tmp", name: "tmp" }],
            playback: {
              total_events: 1,
              total_views: 1,
              total_errors: 0,
              top_viewed_creator: "阿言",
              top_creators: [
                { creator_id: 1, creator_name: "阿言", views: 1, errors: 0 },
              ],
              top_problem_works: [
                {
                  work_id: 8,
                  creator_id: 1,
                  creator_name: "阿言",
                  work_title: "测试作品",
                  views: 1,
                  errors: 0,
                },
              ],
              recent_repair_warnings: [
                {
                  id: 11,
                  event_type: "video_seekability_warning",
                  severity: "warning",
                  message: "修复失败，已保留原文件",
                  created_at: "",
                },
              ],
              recent_events: [],
            },
          },
        });
      }
      if (url === "/api/config/database" && init?.method === "PUT") {
        return Response.json({
          database: {
            directory: "/tmp",
            path: "/tmp/localbilibili.db",
            exists: true,
            writable: true,
          },
        });
      }
      if (
        url === "/api/directories/1/scan" ||
        url === "/api/directories/scan-all"
      ) {
        return Response.json(
          { status: { running: true, progress: 0 } },
          { status: 202 },
        );
      }
      if (url === "/api/fs/roots") {
        return Response.json({ roots: [{ path: "/tmp", name: "tmp" }] });
      }
      return Response.json({ ok: true });
    },
  );
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it("renders saved directories and scan progress", async () => {
  renderPage();
  expect(await screen.findByText("bilibili")).toBeInTheDocument();
  expect(screen.getAllByText("/tmp/bilibili").length).toBeGreaterThan(0);
  expect(screen.getAllByText("阿言").length).toBeGreaterThan(0);
  expect(screen.getByText("25%")).toBeInTheDocument();
  expect(screen.queryByText("运行设置")).not.toBeInTheDocument();
  expect(screen.queryByText("统一设置 / 诊断中心")).not.toBeInTheDocument();
  expect(screen.queryByText("我的账户")).not.toBeInTheDocument();
  expect(screen.queryByText("账户管理")).not.toBeInTheDocument();
});

it("saves typed path and can open browser", async () => {
  const user = userEvent.setup();
  renderPage();
  await user.type(
    screen.getByPlaceholderText("/Users/ted/Documents/LocalBilibili/bilibili"),
    "/tmp/bilibili",
  );
  await user.click(screen.getByText("保存"));
  await waitFor(() =>
    expect(fetch).toHaveBeenCalledWith(
      "/api/directories",
      expect.objectContaining({ method: "POST" }),
    ),
  );
  await user.click(screen.getByText("浏览"));
  expect(await screen.findByRole("dialog")).toBeInTheDocument();
});

it("keeps diagnostics out of the settings page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/directories") {
        return Response.json({ directories: [] });
      }
      if (url === "/api/scan/status") {
        return Response.json({
          status: {
            running: false,
            queue_length: 0,
            scanned_creators: 0,
            total_creators: 0,
            scanned_works: 0,
            progress: 0,
            state: "idle",
            errors: [],
          },
        });
      }
      if (url === "/api/config/database") {
        return Response.json({
          database: {
            directory: "/tmp",
            path: "/tmp/localbilibili.db",
            exists: true,
            writable: true,
          },
        });
      }
      if (url === "/api/diagnostics") {
        return Response.json({
          diagnostics: {
            about: { name: "LocalBilibili", version: "test", description: "test" },
            current_user: {
              id: 1,
              username: "ted",
              is_admin: true,
              is_super_admin: true,
              created_at: "",
              updated_at: "",
            },
            current_view_user: {
              id: 1,
              username: "ted",
              is_admin: true,
              is_super_admin: true,
              created_at: "",
              updated_at: "",
            },
            database: {
              directory: "/tmp",
              path: "/tmp/localbilibili.db",
              exists: true,
              writable: true,
            },
            directories: [],
            cookie: { configured: false, validation_status: "unknown" },
            scan_status: {
              running: false,
              queue_length: 0,
              scanned_creators: 0,
              total_creators: 0,
              scanned_works: 0,
              progress: 0,
              state: "idle",
              errors: [],
            },
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
            accessible_roots: [{ path: "/tmp", name: "tmp" }],
            playback: {
              total_events: 0,
              total_views: 0,
              total_errors: 0,
              top_viewed_creator: "",
              recent_events: [],
            },
          },
        });
      }
      return Response.json({ ok: true });
    }),
  );

  renderPage();
  expect(await screen.findByText("目录配置")).toBeInTheDocument();
  expect(screen.queryByText("统一设置 / 诊断中心")).not.toBeInTheDocument();
  expect(screen.queryByText("还没有创作者聚合数据。")).not.toBeInTheDocument();
});
