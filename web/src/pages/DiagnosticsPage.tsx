import { useQuery } from "@tanstack/react-query";
import { FolderSearch } from "lucide-react";
import { api } from "../lib/api";

export function DiagnosticsPage() {
  const diagnostics = useQuery({
    queryKey: ["diagnostics"],
    queryFn: api.diagnostics,
    refetchInterval: 5000,
  });
  const playback =
    diagnostics.data?.diagnostics?.playback ?? {
      total_events: 0,
      total_views: 0,
      total_errors: 0,
      top_viewed_creator: "",
      last_viewed_at: "",
      top_creators: [],
      top_problem_works: [],
      recent_repair_warnings: [],
      recent_events: [],
    };
  const topCreators = playback.top_creators ?? [];
  const topProblemWorks = playback.top_problem_works ?? [];
  const recentRepairWarnings = playback.recent_repair_warnings ?? [];
  const recentPlaybackEvents = playback.recent_events ?? [];

  return (
    <div className="page diagnostics-page">
      <header className="page-header">
        <h1>诊断中心</h1>
        <p>集中查看数据库、扫描、更新、最近播放和错误摘要。</p>
      </header>

      <section className="settings-card diagnostics-center">
        <div className="account-card-head">
          <FolderSearch size={18} />
          <div>
            <h2>统一设置 / 诊断中心</h2>
            <p>把数据库、扫描、更新、最近播放和错误摘要集中到一页里。</p>
          </div>
        </div>
        {diagnostics.error && (
          <p className="error-line">{diagnostics.error.message}</p>
        )}
        {diagnostics.data?.diagnostics && (
          <>
            <div className="metric-grid diagnostics-grid">
              <MetricCard
                label="数据库"
                value={
                  diagnostics.data.diagnostics.database.writable
                    ? "可写"
                    : "只读"
                }
                detail={diagnostics.data.diagnostics.database.path}
              />
              <MetricCard
                label="扫描状态"
                value={diagnostics.data.diagnostics.scan_status.state}
                detail={`${diagnostics.data.diagnostics.scan_status.scanned_creators}/${diagnostics.data.diagnostics.scan_status.total_creators} UP主`}
              />
              <MetricCard
                label="更新状态"
                value={diagnostics.data.diagnostics.update_status.state}
                detail={`成功 ${diagnostics.data.diagnostics.update_status.succeeded_creators} · 失败 ${diagnostics.data.diagnostics.update_status.failed_creators}`}
              />
              <MetricCard
                label="播放事件"
                value={String(playback.total_events)}
                detail={`播放 ${playback.total_views} · 错误 ${playback.total_errors}`}
              />
            </div>
            <div className="diagnostics-panels">
              <div className="diagnostics-panel">
                <h3>运行环境</h3>
                <p>
                  当前用户：
                  {diagnostics.data.diagnostics.current_user.username}
                </p>
                <p>
                  查看视角：
                  {diagnostics.data.diagnostics.current_view_user.username}
                </p>
                <p>
                  可访问根目录：
                  {diagnostics.data.diagnostics.accessible_roots
                    .map((item) => item.name)
                    .join("、") || "-"}
                </p>
              </div>
              <div className="diagnostics-panel">
                <h3>最近错误</h3>
                <p>
                  {diagnostics.data.diagnostics.latest_scan_error ||
                    "扫描暂无错误"}
                </p>
                <p>
                  {diagnostics.data.diagnostics.latest_update_error ||
                    "更新暂无错误"}
                </p>
              </div>
              <div className="diagnostics-panel">
                <h3>最近播放诊断</h3>
                <p>最高播放UP主：{playback.top_viewed_creator || "-"}</p>
                <p>最近浏览：{playback.last_viewed_at || "-"}</p>
                <ul className="diagnostics-events">
                  {recentPlaybackEvents.slice(0, 5).map((event) => (
                    <li key={event.id}>
                      <strong>{event.creator_name}</strong>
                      <span>{event.event_type}</span>
                      <small>{event.work_title}</small>
                    </li>
                  ))}
                  {recentPlaybackEvents.length === 0 && <li>还没有播放事件。</li>}
                </ul>
              </div>
              <div className="diagnostics-panel">
                <h3>高频浏览创作者</h3>
                <ul className="diagnostics-events">
                  {topCreators.slice(0, 5).map((creator) => (
                    <li key={creator.creator_id}>
                      <strong>{creator.creator_name}</strong>
                      <span>播放 {creator.views}</span>
                      <small>错误 {creator.errors}</small>
                    </li>
                  ))}
                  {topCreators.length === 0 && <li>还没有创作者聚合数据。</li>}
                </ul>
              </div>
              <div className="diagnostics-panel">
                <h3>问题较多作品</h3>
                <ul className="diagnostics-events">
                  {topProblemWorks.slice(0, 5).map((work) => (
                    <li key={work.work_id}>
                      <strong>{work.work_title || "未命名作品"}</strong>
                      <span>{work.creator_name}</span>
                      <small>
                        错误 {work.errors} · 播放 {work.views}
                      </small>
                    </li>
                  ))}
                  {topProblemWorks.length === 0 && <li>还没有作品级诊断数据。</li>}
                </ul>
              </div>
              <div className="diagnostics-panel">
                <h3>最近 MP4 修复告警</h3>
                <ul className="diagnostics-events">
                  {recentRepairWarnings.slice(0, 5).map((warning) => (
                    <li key={warning.id}>
                      <strong>{warning.event_type}</strong>
                      <span>{warning.created_at}</span>
                      <small>{warning.message}</small>
                    </li>
                  ))}
                  {recentRepairWarnings.length === 0 && (
                    <li>最近没有自动修复告警。</li>
                  )}
                </ul>
              </div>
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function MetricCard({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail: string;
}) {
  return (
    <div className="metric-card">
      <strong>{value}</strong>
      <span>{label}</span>
      <small>{detail}</small>
    </div>
  );
}
