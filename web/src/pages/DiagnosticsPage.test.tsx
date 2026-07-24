import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { DiagnosticsPage } from "./DiagnosticsPage";

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <DiagnosticsPage />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

it("renders diagnostics as a standalone page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === "/api/diagnostics") {
        return Response.json({
          diagnostics: diagnosticsPayload({
            playback: {
              total_events: 1,
              total_views: 1,
              total_errors: 0,
              top_viewed_creator: "阿言",
              last_viewed_at: "",
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
          }),
        });
      }
      return Response.json({ ok: true });
    }),
  );

  renderPage();

  expect(await screen.findByText("诊断中心")).toBeInTheDocument();
  expect(screen.getByText("统一设置 / 诊断中心")).toBeInTheDocument();
  expect(await screen.findByText("最高播放UP主：阿言")).toBeInTheDocument();
  expect(screen.getByText("高频浏览创作者")).toBeInTheDocument();
  expect(screen.getByText("问题较多作品")).toBeInTheDocument();
  expect(screen.getByText("最近 MP4 修复告警")).toBeInTheDocument();
});

it("renders even when playback diagnostic arrays are omitted", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === "/api/diagnostics") {
        return Response.json({
          diagnostics: diagnosticsPayload({
            playback: {
              total_events: 0,
              total_views: 0,
              total_errors: 0,
              top_viewed_creator: "",
              recent_events: [],
            },
          }),
        });
      }
      return Response.json({ ok: true });
    }),
  );

  renderPage();

  expect(await screen.findByText("统一设置 / 诊断中心")).toBeInTheDocument();
  expect(await screen.findByText("还没有创作者聚合数据。")).toBeInTheDocument();
  expect(await screen.findByText("还没有作品级诊断数据。")).toBeInTheDocument();
  expect(await screen.findByText("最近没有自动修复告警。")).toBeInTheDocument();
});

function diagnosticsPayload(overrides: Record<string, unknown>) {
  return {
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
    ...overrides,
  };
}
