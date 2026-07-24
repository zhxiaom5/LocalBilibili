import type { ScanStatus } from '../lib/types';

export function ProgressPanel({ status }: { status?: ScanStatus }) {
  const value = status?.progress ?? 0;
  return (
    <section className="progress-panel" aria-label="扫描进度">
      <div className="progress-top">
        <strong>{status?.running ? '正在扫描' : '扫描空闲'}</strong>
        <span>{value}%</span>
      </div>
      <div className="progress-bar">
        <div style={{ width: `${value}%` }} />
      </div>
      <dl>
        <div>
          <dt>当前目录</dt>
          <dd>{status?.current_directory || '-'}</dd>
        </div>
        <div>
          <dt>当前UP主</dt>
          <dd>{status?.current_creator || '-'}</dd>
        </div>
        <div>
          <dt>已扫UP主</dt>
          <dd>
            {status?.scanned_creators ?? 0}/{status?.total_creators ?? 0}
          </dd>
        </div>
        <div>
          <dt>已扫作品</dt>
          <dd>{status?.scanned_works ?? 0}</dd>
        </div>
      </dl>
      {!!status?.errors?.length && <p className="error-line">{status.errors[status.errors.length - 1]}</p>}
    </section>
  );
}
