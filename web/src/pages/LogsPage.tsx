import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useState } from "react";
import { api } from "../lib/api";
import type { LogEvent } from "../lib/types";

type LogTab = "audit" | "system";

export function LogsPage() {
  const [tab, setTab] = useState<LogTab>("audit");
  const [page, setPage] = useState(1);
  const logs = useQuery({
    queryKey: ["logs", tab, page],
    queryFn: () =>
      tab === "audit"
        ? api.auditLogs({ page, pageSize: 20 })
        : api.systemLogs({ page, pageSize: 20 }),
  });
  const data = logs.data;

  return (
    <div className="page narrow-page logs-page">
      <header className="page-header">
        <h1>日志记录</h1>
        <p>超级管理员可查看关键操作和系统事件，便于排障与回溯。</p>
      </header>

      <section className="settings-card">
        <div className="toolbar log-toolbar">
          <div className="segmented">
            <button
              className={tab === "audit" ? "active" : ""}
              type="button"
              onClick={() => {
                setTab("audit");
                setPage(1);
              }}
            >
              操作日志
            </button>
            <button
              className={tab === "system" ? "active" : ""}
              type="button"
              onClick={() => {
                setTab("system");
                setPage(1);
              }}
            >
              系统日志
            </button>
          </div>
        </div>

        {logs.isLoading && <div className="empty-state">加载日志中...</div>}
        {logs.error && <p className="error-line">{logs.error.message}</p>}
        {!logs.isLoading && data?.events.length === 0 && (
          <div className="empty-state">暂无日志。</div>
        )}
        <div className="log-list">
          {data?.events.map((event) => (
            <LogRow
              key={`${tab}-${event.id}`}
              event={event}
              system={tab === "system"}
            />
          ))}
        </div>
        <div className="pagination-bar">
          <button
            type="button"
            disabled={page <= 1}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            上一页
          </button>
          <span>
            第 {data?.page ?? page} / {data?.total_pages || 1} 页 · 共{" "}
            {data?.total ?? 0} 条
          </span>
          <button
            type="button"
            disabled={!data || page >= data.total_pages}
            onClick={() => setPage((current) => current + 1)}
          >
            下一页
          </button>
        </div>
      </section>
    </div>
  );
}

function LogRow({ event, system }: { event: LogEvent; system: boolean }) {
  return (
    <details className="log-row">
      <summary>
        <FileText size={16} />
        <strong>{event.event_type}</strong>
        {!system && (
          <span>
            {event.actor_name || `用户 ${event.actor_user_id || "-"}`}
          </span>
        )}
        {system && <span>{event.severity || "info"}</span>}
        <time>{formatDate(event.created_at)}</time>
      </summary>
      <div className="log-detail">
        {event.message && <p>{event.message}</p>}
        {event.target_type && (
          <p>
            目标：{event.target_type} #{event.target_id || "-"}
          </p>
        )}
        {event.detail_json && <pre>{event.detail_json}</pre>}
      </div>
    </details>
  );
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
