import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { api } from "../lib/api";

export function AboutPage() {
  const about = useQuery({ queryKey: ["about"], queryFn: api.about });
  const info = about.data?.about;

  return (
    <div className="page narrow-page about-page">
      <header className="page-header">
        <h1>关于</h1>
        <p>查看当前 LocalBilibili 的版本、构建信息和运行环境。</p>
      </header>

      <section className="settings-card about-card">
        <div className="account-card-head">
          <Info size={18} />
          <div>
            <h2>{info?.name ?? "LocalBilibili"}</h2>
            <p>{info?.description ?? "局域网版本地B站作品浏览 Web 应用"}</p>
          </div>
        </div>

        {about.isLoading && <div className="empty-state">加载版本信息...</div>}
        {about.error && <p className="error-line">{about.error.message}</p>}
        {info && (
          <dl className="info-grid">
            <div>
              <dt>版本</dt>
              <dd>{info.version}</dd>
            </div>
            <div>
              <dt>编译日期</dt>
              <dd>{info.build_date || "未记录"}</dd>
            </div>
            <div>
              <dt>Commit</dt>
              <dd>{info.commit || "未记录"}</dd>
            </div>
            <div>
              <dt>Go 版本</dt>
              <dd>{info.go_version || "未记录"}</dd>
            </div>
            <div>
              <dt>平台</dt>
              <dd>{info.platform || "未记录"}</dd>
            </div>
            <div>
              <dt>运行模式</dt>
              <dd>{info.build_mode || "单体 Go 服务"}</dd>
            </div>
            <div className="info-grid-wide">
              <dt>数据库文件</dt>
              <dd>{info.database_path || "未记录"}</dd>
            </div>
          </dl>
        )}
      </section>
    </div>
  );
}
