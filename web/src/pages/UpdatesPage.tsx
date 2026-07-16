import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarClock, Cookie, Play, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../lib/api';
import type { Creator, UpdateJob, UpdateQueue } from '../lib/types';

type JobStatusFilter = 'pending' | 'running' | 'succeeded' | 'failed';

export function UpdatesPage() {
  const queryClient = useQueryClient();
  const [newName, setNewName] = useState('');
  const [newCron, setNewCron] = useState('0 3 * * *');
  const [cookieText, setCookieText] = useState('');
  const [cookieHelp, setCookieHelp] = useState(false);
  const [editingQueue, setEditingQueue] = useState<UpdateQueue | null>(null);
  const [jobPage, setJobPage] = useState(1);
  const [jobStatusFilter, setJobStatusFilter] = useState<JobStatusFilter | ''>('');
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const status = useQuery({ queryKey: ['update-status'], queryFn: api.updateStatus, refetchInterval: 3000 });
  const updateJobs = useQuery({ queryKey: ['update-jobs', jobPage, jobStatusFilter], queryFn: () => api.updateJobs({ page: jobPage, pageSize: 10, status: jobStatusFilter || undefined }), refetchInterval: 5000 });
  const queues = useQuery({ queryKey: ['update-queues'], queryFn: api.updateQueues });
  const cookie = useQuery({ queryKey: ['update-cookie'], queryFn: api.updateCookie });
  const runtime = useQuery({ queryKey: ['runtime-config'], queryFn: api.runtimeConfig });
  const diagnostics = useQuery({ queryKey: ['diagnostics'], queryFn: api.diagnostics, refetchInterval: 5000 });
  const queueList = queues.data?.queues ?? [];
  const statusValue = status.data?.status;
  const playback = diagnostics.data?.diagnostics?.playback;

  useEffect(() => {
    const value = cookie.data?.cookie.value ?? '';
    if (value) setCookieText(value);
  }, [cookie.data?.cookie.value]);

  const nextThreeDays = useMemo(() => queueList.filter((queue) => withinThreeDays(queue.next_run_at)), [queueList]);
  const createQueue = useMutation({
    mutationFn: () => api.createUpdateQueue(newName, newCron),
    onSuccess: () => {
      setNewName('');
      setNewCron('0 3 * * *');
      setNotice({ type: 'success', text: '定时更新队列已创建' });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const patchQueue = useMutation({
    mutationFn: ({ id, body }: { id: number; body: { name?: string; cron?: string; is_default?: boolean; enabled?: boolean } }) => api.updateQueue(id, body),
    onSuccess: () => {
      setNotice({ type: 'success', text: '定时更新队列已更新' });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const deleteQueue = useMutation({
    mutationFn: api.deleteUpdateQueue,
    onSuccess: () => {
      setNotice({ type: 'success', text: '定时更新队列已删除' });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const runQueue = useMutation({
    mutationFn: api.runUpdateQueueNow,
    onSuccess: (_data, queueID) => {
      const queue = queueList.find((item) => item.id === queueID);
      setNotice({ type: 'success', text: `${queue?.name ?? '定时队列'} 已加入立即执行队列` });
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const saveCookie = useMutation({
    mutationFn: () => api.saveUpdateCookie(cookieText),
    onSuccess: () => {
      setCookieText('');
      setNotice({ type: 'success', text: 'Cookie 已保存' });
      queryClient.invalidateQueries({ queryKey: ['update-cookie'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const validateCookie = useMutation({
    mutationFn: api.validateUpdateCookie,
    onSuccess: () => {
      setNotice({ type: 'success', text: 'Cookie 校验已完成' });
      queryClient.invalidateQueries({ queryKey: ['update-cookie'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const resetImmediate = useMutation({
    mutationFn: api.resetImmediateUpdate,
    onSuccess: () => {
      setNotice({ type: 'success', text: '立即执行队列已重置' });
      setJobPage(1);
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const retryJob = useMutation({
    mutationFn: api.retryUpdateJob,
    onSuccess: () => {
      setNotice({ type: 'success', text: '失败任务已重新加入立即执行队列' });
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const deleteJob = useMutation({
    mutationFn: api.deleteUpdateJob,
    onSuccess: () => {
      setNotice({ type: 'success', text: '更新记录已删除' });
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const saveRuntime = useMutation({
    mutationFn: api.saveRuntimeConfig,
    onSuccess: () => {
      setNotice({ type: 'success', text: '运行设置已保存' });
      queryClient.invalidateQueries({ queryKey: ['runtime-config'] });
      queryClient.invalidateQueries({ queryKey: ['diagnostics'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const resolvePlaceholders = useMutation({
    mutationFn: api.resolveCreatorPlaceholders,
    onSuccess: (data) => {
      setNotice({ type: 'success', text: `占位UP主名修复完成：${data.creators.length} 个UP主已回填真实资料` });
      queryClient.invalidateQueries({ queryKey: ['update-status'] });
      queryClient.invalidateQueries({ queryKey: ['update-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['creators'] });
    },
    onError: (cause) => setNotice({ type: 'error', text: errorMessage(cause) }),
  });
  const runningJobs = statusValue?.running_jobs ?? [];
  const historyJobs = (updateJobs.data?.jobs && updateJobs.data.jobs.length > 0 ? updateJobs.data.jobs : statusValue?.jobs) ?? [];
  const jobSummary = updateJobs.data?.summary;
  const activeFilterLabel = jobStatusFilter ? formatJobStatusFilter(jobStatusFilter) : '全部状态';
  const selectJobFilter = (filter: JobStatusFilter | '') => {
    setJobStatusFilter(filter);
    setJobPage(1);
  };

  return (
    <div className="page updates-page">
      <header className="page-header">
        <h1>更新管理</h1>
        <p>管理立即更新队列、定时更新队列、订阅UP主和B站 Cookie。</p>
      </header>
      <div className="row-actions">
        <button type="button" disabled={resolvePlaceholders.isPending} onClick={() => resolvePlaceholders.mutate()}>
          <RefreshCw size={16} /> 修复占位UP主名
        </button>
      </div>
      {notice && (
        <p className={notice.type === 'success' ? 'success-line' : 'error-line'} role="status">
          {notice.text}
        </p>
      )}

      <section className="account-card update-runtime-card">
        <div className="account-card-head">
          <RefreshCw size={18} />
          <div>
            <h2>运行设置</h2>
            <p>控制B站下载器同时更新的UP主数量。</p>
          </div>
        </div>
        <form
          className="inline-form runtime-form"
          onSubmit={(event) => {
            event.preventDefault();
            const form = event.currentTarget;
            const input = form.elements.namedItem('downloaderConcurrency') as HTMLInputElement | null;
            saveRuntime.mutate(Number(input?.value ?? 1));
          }}
        >
          <label>
            下载器并发数量
            <input
              name="downloaderConcurrency"
              type="number"
              min={1}
              max={6}
              defaultValue={runtime.data?.runtime?.downloader_concurrency ?? 1}
              aria-label="下载器并发数量"
            />
          </label>
          <button type="submit" disabled={saveRuntime.isPending}>保存运行设置</button>
        </form>
        <p className="hint-line">当前并发：{runtime.data?.runtime?.downloader_concurrency ?? 1}，默认 1，最大 6。</p>
        {runtime.error && <p className="error-line">{runtime.error.message}</p>}
        {saveRuntime.error && <p className="error-line">{saveRuntime.error.message}</p>}
      </section>

      <section className="updates-grid">
        <article className="account-card update-status-card">
          <div className="account-card-head">
            <RefreshCw size={18} />
            <div>
              <h2>立即执行队列</h2>
              <p>{statusValue?.running ? `正在更新 ${statusValue.current_creator ?? '-'}` : '当前没有正在执行的更新任务'}</p>
            </div>
          </div>
          <div className="row-actions update-card-actions">
            <button
              type="button"
              className="danger"
              onClick={() => {
                if (window.confirm('确定重置立即更新队列？当前任务会被取消，历史记录也会被清空。')) resetImmediate.mutate();
              }}
            >
              重置
            </button>
          </div>
          <div className="progress-line">
            <span style={{ width: `${statusValue?.progress ?? 0}%` }} />
          </div>
          <div className="metric-grid">
            <Metric label="队列中UP主" value={jobSummary?.pending ?? 0} active={jobStatusFilter === 'pending'} onClick={() => selectJobFilter('pending')} />
            <Metric label="正在更新UP主" value={jobSummary?.running ?? 0} active={jobStatusFilter === 'running'} onClick={() => selectJobFilter('running')} />
            <Metric label="成功UP主" value={jobSummary?.succeeded ?? 0} active={jobStatusFilter === 'succeeded'} onClick={() => selectJobFilter('succeeded')} />
            <Metric label="失败UP主" value={jobSummary?.failed ?? 0} active={jobStatusFilter === 'failed'} onClick={() => selectJobFilter('failed')} />
            <Metric label="新增作品" value={jobSummary?.added_works ?? 0} active={jobStatusFilter === ''} onClick={() => selectJobFilter('')} />
          </div>
          <section className="running-job-section">
            <h3>正在更新</h3>
            <div className="job-list">
              {runningJobs.map((job) => (
                <RunningJobCard key={job.id} job={job} />
              ))}
              {runningJobs.length === 0 && <div className="empty-state">当前没有正在执行的更新任务。</div>}
            </div>
          </section>
          <section className="job-history-section">
            <div className="section-title-row">
              <h3>更新记录</h3>
              <span>{activeFilterLabel} · 共 {updateJobs.data?.total ?? 0} 条</span>
              {jobStatusFilter && (
                <button type="button" className="tiny-button" onClick={() => selectJobFilter('')}>
                  全部
                </button>
              )}
            </div>
          <div className="job-list">
            {historyJobs.map((job) => (
              <UpdateJobRow
                key={job.id}
                job={job}
                onRetry={() => retryJob.mutate(job.id)}
                onDelete={() => deleteJob.mutate(job.id)}
                retryDisabled={retryJob.isPending}
                deleteDisabled={deleteJob.isPending || job.status === 'running'}
              />
            ))}
            {!updateJobs.isLoading && historyJobs.length === 0 && <div className="empty-state">暂无更新任务。</div>}
          </div>
            <div className="pagination-bar">
              <button
                type="button"
                disabled={jobPage <= 1}
                onClick={() => setJobPage((current) => Math.max(1, current - 1))}
              >
                上一页
              </button>
              <span>
                第 {updateJobs.data?.page ?? jobPage} / {updateJobs.data?.total_pages || 1} 页 · 共 {updateJobs.data?.total ?? 0} 条
              </span>
              <button
                type="button"
                disabled={!updateJobs.data || jobPage >= updateJobs.data.total_pages}
                onClick={() => setJobPage((current) => current + 1)}
              >
                下一页
              </button>
            </div>
          </section>
        </article>

        <article className="account-card">
          <div className="account-card-head">
            <CalendarClock size={18} />
            <div>
              <h2>最近 3 天要执行的队列</h2>
              <p>未来任务可直接加入立即执行队列。</p>
            </div>
          </div>
          <div className="queue-list">
            {nextThreeDays.map((queue) => (
              <QueueRow key={queue.id} queue={queue} onRun={() => runQueue.mutate(queue.id)} />
            ))}
            {nextThreeDays.length === 0 && <div className="empty-state">最近 3 天没有定时任务。</div>}
          </div>
        </article>
      </section>

      <section className="account-card">
        <div className="account-card-head">
          <CalendarClock size={18} />
          <div>
            <h2>定时更新的队列</h2>
            <p>使用 5 段 crontab 表达式，例如每天 3 点：0 3 * * *</p>
          </div>
        </div>
        <form
          className="inline-form"
          onSubmit={(event) => {
            event.preventDefault();
            createQueue.mutate();
          }}
        >
          <input aria-label="队列名称" value={newName} onChange={(event) => setNewName(event.target.value)} placeholder="队列名称" />
          <input aria-label="cron 表达式" value={newCron} onChange={(event) => setNewCron(event.target.value)} placeholder="0 3 * * *" />
          <button type="submit" disabled={!newName.trim() || !newCron.trim()}>
            <Plus size={16} /> 新增队列
          </button>
        </form>
        <div className="queue-list">
          {queueList.map((queue) => (
            <div key={queue.id} className="queue-row">
              <div>
                <strong>{queue.name}</strong>
                <span>{queue.cron} · {queue.subscribed_count} 个UP主 · 下次 {formatTime(queue.next_run_at)}</span>
                <small>{queue.last_run_summary || '暂无上次运行摘要'}</small>
              </div>
              <div className="row-actions">
                <button type="button" onClick={() => runQueue.mutate(queue.id)}>
                  <Play size={16} /> 立即更新
                </button>
                <button type="button" onClick={() => setEditingQueue(queue)}>
                  编辑
                </button>
                <button type="button" disabled={queue.is_default} onClick={() => patchQueue.mutate({ id: queue.id, body: { is_default: true } })}>
                  设为默认
                </button>
                <button type="button" onClick={() => patchQueue.mutate({ id: queue.id, body: { enabled: !queue.enabled } })}>
                  {queue.enabled ? '停用' : '启用'}
                </button>
                <button type="button" className="danger" onClick={() => deleteQueue.mutate(queue.id)}>
                  <Trash2 size={16} /> 删除
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="account-card">
        <div className="account-card-head">
          <Cookie size={18} />
          <div>
            <h2>Cookie 管理</h2>
            <p>当前状态：{cookie.data?.cookie.configured ? '已保存' : '未保存'} · 校验 {cookie.data?.cookie.validation_status ?? 'unknown'}</p>
          </div>
        </div>
        <textarea className="cookie-editor" value={cookieText} onChange={(event) => setCookieText(event.target.value)} placeholder="粘贴 bilibili.com Cookie，仅管理员可见。" />
        <div className="row-actions">
          <button type="button" onClick={() => saveCookie.mutate()} disabled={!cookieText.trim()}>保存 Cookie</button>
          <button type="button" onClick={() => validateCookie.mutate()} disabled={!cookie.data?.cookie.configured}>校验 Cookie</button>
          <button type="button" onClick={() => setCookieHelp((value) => !value)}>浏览器获取 Cookie</button>
        </div>
        {cookie.data?.cookie.validation_message && <p className="muted-line">{cookie.data.cookie.validation_message}</p>}
        {cookieHelp && (
          <div className="hint-box">
            fnOS Web 环境不支持稳定自动读取 bilibili.com Cookie。请在电脑浏览器登录B站后，从开发者工具手动复制 Cookie，再粘贴到这里保存。
            <code>msToken=...; ttwid=...; odin_tt=...; passport_csrf_token=...; s_v_web_id=...</code>
          </div>
        )}
      </section>
      <section className="account-card">
        <div className="account-card-head">
          <RefreshCw size={18} />
          <div>
            <h2>自动修复 / 下载诊断</h2>
            <p>这里集中展示最近 MP4 seekability 修复告警和下载后的自动入库线索。</p>
          </div>
        </div>
        <div className="job-list">
          {(playback?.recent_repair_warnings ?? []).map((warning) => (
            <div key={warning.id} className="job-row">
              <strong>{warning.event_type}</strong>
              <span>{warning.severity}</span>
              <small>{warning.message}</small>
            </div>
          ))}
          {(playback?.recent_repair_warnings?.length ?? 0) === 0 && (
            <div className="empty-state">最近没有自动修复告警。</div>
          )}
        </div>
      </section>
      {editingQueue && <QueueEditor queue={editingQueue} onClose={() => setEditingQueue(null)} />}
    </div>
  );
}

function QueueEditor({ queue, onClose }: { queue: UpdateQueue; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(queue.name);
  const [cron, setCron] = useState(queue.cron);
  const [creatorSearch, setCreatorSearch] = useState('');
  const dialogRef = useRef<HTMLDivElement>(null);
  const members = useQuery({ queryKey: ['update-queue-creators', queue.id], queryFn: () => api.updateQueueCreators(queue.id) });
  const creators = useQuery({ queryKey: ['creators', creatorSearch], queryFn: () => api.creators(creatorSearch) });
  const memberList = members.data?.creators ?? [];
  const creatorList = creators.data?.creators ?? [];
  const memberIDs = new Set(memberList.map((creator) => creator.id));
  const save = useMutation({
    mutationFn: () => api.updateQueue(queue.id, { name, cron }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      onClose();
    },
  });
  const addCreator = useMutation({
    mutationFn: (creatorID: number) => api.addUpdateQueueCreators(queue.id, [creatorID]),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['update-queue-creators', queue.id] });
    },
  });
  const removeCreator = useMutation({
    mutationFn: (creatorID: number) => api.removeUpdateQueueCreator(queue.id, creatorID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['update-queues'] });
      queryClient.invalidateQueries({ queryKey: ['update-queue-creators', queue.id] });
    },
  });

  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  return (
    <div
      ref={dialogRef}
      className="modal-backdrop"
      role="dialog"
      aria-label="编辑定时队列"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onClose();
      }}
    >
      <div className="modal queue-editor-modal">
        <div className="modal-head">
          <div>
            <h2>编辑定时队列</h2>
          </div>
          <button type="button" className="icon-button" onClick={onClose} title="关闭" aria-label="关闭弹窗">
            <X size={18} />
          </button>
        </div>
        <div className="account-form">
          <label>
            <span>队列名称</span>
            <input aria-label="队列名称" value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label>
            <span>cron 表达式</span>
            <input aria-label="cron 表达式" value={cron} onChange={(event) => setCron(event.target.value)} />
          </label>
        </div>
        <section className="queue-editor-section">
          <h3>已订阅UP主</h3>
          <div className="queue-member-list">
            {memberList.map((creator) => (
              <CreatorMemberRow key={creator.id} creator={creator} actionLabel="移除" onAction={() => removeCreator.mutate(creator.id)} />
            ))}
            {!members.isLoading && memberList.length === 0 && <div className="empty-state">这个队列还没有UP主。</div>}
          </div>
        </section>
        <section className="queue-editor-section">
          <h3>添加UP主</h3>
          <input className="queue-search-input" aria-label="搜索可添加UP主" value={creatorSearch} onChange={(event) => setCreatorSearch(event.target.value)} placeholder="搜索UP主" />
          <div className="queue-member-list">
            {creatorList.filter((creator) => !memberIDs.has(creator.id)).map((creator) => (
              <CreatorMemberRow key={creator.id} creator={creator} actionLabel="添加" onAction={() => addCreator.mutate(creator.id)} />
            ))}
          </div>
        </section>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button type="button" disabled={!name.trim() || !cron.trim() || save.isPending} onClick={() => save.mutate()}>保存</button>
        </div>
      </div>
    </div>
  );
}

function CreatorMemberRow({ creator, actionLabel, onAction }: { creator: Creator; actionLabel: string; onAction: () => void }) {
  return (
    <div className="queue-member-row">
      <div>
        <strong>{creator.name}</strong>
        <span>{creator.work_count} 个作品</span>
      </div>
      <button type="button" onClick={onAction}>{actionLabel}</button>
    </div>
  );
}

function QueueRow({ queue, onRun }: { queue: UpdateQueue; onRun: () => void }) {
  return (
    <div className="queue-row">
      <div>
        <strong>{queue.name}</strong>
        <span>{formatTime(queue.next_run_at)} · {queue.subscribed_count} 个UP主</span>
      </div>
      <div className="row-actions">
        <button type="button" onClick={onRun}>
          <Play size={16} /> 立即更新
        </button>
      </div>
    </div>
  );
}

function Metric({ label, value, active, onClick }: { label: string; value: number; active?: boolean; onClick?: () => void }) {
  return (
    <button type="button" className={`metric-card${active ? ' active' : ''}`} onClick={onClick} aria-label={`按${label}过滤更新记录`} aria-pressed={active}>
      <strong>{value}</strong>
      <span>{label}</span>
    </button>
  );
}

function RunningJobCard({ job }: { job: UpdateJob }) {
  const progress = job.total_works ? `${job.processed_works ?? 0}/${job.total_works}` : '-';
  return (
    <div className="job-row running-job-row">
      <div className="job-creator">
        <strong>{job.creator_name}</strong>
        <span>{job.progress_message || '正在下载'}</span>
      </div>
      <div className="job-status">
        <span>处理作品 {progress}</span>
        <small>媒体 {job.downloaded_media ?? 0} · 跳过 {job.skipped_works ?? 0}</small>
      </div>
      <div className="job-detail">
        <span>{formatSpeed(job.download_speed_bytes_per_second ?? 0)}</span>
        {job.download_directory && <small>{job.download_directory}</small>}
      </div>
    </div>
  );
}

function UpdateJobRow({
  job,
  onRetry,
  onDelete,
  retryDisabled,
  deleteDisabled,
}: {
  job: UpdateJob;
  onRetry: () => void;
  onDelete: () => void;
  retryDisabled: boolean;
  deleteDisabled: boolean;
}) {
  return (
    <div className="job-row">
      <div className="job-creator">
        <strong>{job.creator_name}</strong>
        <span>{job.status}{job.attempt && job.attempt > 1 ? ` · 第 ${job.attempt} 次` : ''}</span>
      </div>
      <div className="job-detail">
        <span>{formatJobSummary(job)}</span>
        <small>{formatJobDetail(job)}</small>
        <small className="job-time-line">触发 {formatTimeOrMissing(job.started_at)} · 结束 {formatTimeOrMissing(job.finished_at)} · {formatTriggerSource(job.trigger_source)}</small>
      </div>
      <div className="row-actions job-actions">
        {job.status === 'failed' && (
          <button type="button" className="tiny-button" disabled={retryDisabled} onClick={onRetry}>
            重试
          </button>
        )}
        {job.status !== 'running' && (
          <button type="button" className="tiny-button danger" disabled={deleteDisabled} onClick={onDelete}>
            删除
          </button>
        )}
      </div>
    </div>
  );
}

function formatJobSummary(job: UpdateJob) {
  if (job.status === 'running') return `${job.progress_message || '正在下载'} · ${formatSpeed(job.download_speed_bytes_per_second ?? 0)}`;
  if (job.added_works) return `新增 ${job.added_works}`;
  if (job.status === 'failed') return '更新失败';
  if (job.status === 'succeeded') return '更新完成';
  return job.progress_message || '-';
}

function formatJobDetail(job: UpdateJob) {
  if (job.error) return job.error;
  const detail = [
    job.total_works ? `作品 ${job.processed_works ?? 0}/${job.total_works}` : '',
    job.downloaded_media ? `媒体 ${job.downloaded_media}` : '',
    job.skipped_works ? `跳过 ${job.skipped_works}` : '',
    job.download_directory || '',
  ].filter(Boolean);
  return detail.join(' · ') || job.download_notice || '-';
}

function withinThreeDays(value: string) {
  if (!value) return false;
  const date = new Date(value);
  const now = Date.now();
  return Number.isFinite(date.getTime()) && date.getTime() >= now - 60_000 && date.getTime() <= now + 3 * 24 * 60 * 60 * 1000;
}

function formatTime(value: string) {
  if (!value) return '-';
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return date.toLocaleString();
}

function formatTimeOrMissing(value?: string) {
  if (!value) return '未记录';
  return formatTime(value);
}

function formatTriggerSource(value?: string) {
  if (value === 'scheduled') return '定时更新';
  if (value === 'manual') return '人工触发';
  return '来源未记录';
}

function formatJobStatusFilter(value: JobStatusFilter) {
  switch (value) {
    case 'pending':
      return '队列中';
    case 'running':
      return '正在更新';
    case 'succeeded':
      return '成功';
    case 'failed':
      return '失败';
  }
}

function formatSpeed(value: number) {
  if (!value || value < 1) return '0 B/s';
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB/s`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB/s`;
  return `${Math.round(value)} B/s`;
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : '操作失败';
}
