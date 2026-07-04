import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { LogsPage } from "./LogsPage";

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <LogsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/logs/audit")) {
        return Response.json({
          events: [
            {
              id: 1,
              actor_user_id: 1,
              actor_name: "ted",
              event_type: "scan_directory",
              message: "扫描目录",
              created_at: "2026-06-17T00:00:00Z",
            },
          ],
          page: 1,
          page_size: 20,
          total: 1,
          total_pages: 1,
        });
      }
      if (url.includes("/api/logs/system")) {
        return Response.json({
          events: [
            {
              id: 2,
              severity: "warning",
              event_type: "video_seekability_warning",
              message: "MP4 修复告警",
              created_at: "2026-06-17T00:00:00Z",
            },
          ],
          page: 1,
          page_size: 20,
          total: 1,
          total_pages: 1,
        });
      }
      return Response.json({ ok: true });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it("uses the narrow page layout and renders audit/system logs", async () => {
  const user = userEvent.setup();
  const { container } = renderPage();
  expect(await screen.findByRole("heading", { name: "日志记录" })).toBeInTheDocument();
  expect(container.querySelector(".logs-page.narrow-page")).toBeInTheDocument();
  expect(await screen.findByText("scan_directory")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "系统日志" }));
  expect(await screen.findByText("video_seekability_warning")).toBeInTheDocument();
});
