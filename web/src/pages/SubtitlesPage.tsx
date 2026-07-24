import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Captions, Play, RefreshCw, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api, ApiError } from '../lib/api';
import type { SubtitleConfig, SubtitleJob } from '../lib/types';

const defaultConfig: SubtitleConfig = {
  mode: 'local',
  concurrency: 1,
  language: 'zh',
  gpu: true,
};

export function SubtitlesPage() {
  const queryClient = useQueryClient();
  const configQuery = useQuery({ queryKey: ['subtitle-config'], queryFn: api.subtitleConfig });
  const statusQuery = useQuery({ queryKey: ['subtitle-status'], queryFn: api.subtitleStatus });
  const jobsQuery = useQuery({ queryKey: ['subtitle-jobs'], queryFn: () => api.subtitleJobs({ page: 1, pageSize: 10 }) });
  const queuesQuery = useQuery({ queryKey: ['subtitle-queues'], queryFn: api.subtitleQueues });
  const [draft, setDraft] = useState<SubtitleConfig>(defaultConfig);
  const [queueName, setQueueName] = useState('');
  const [queueCron, setQueueCron] = useState('30 3 * * *');
  const [notice, setNotice] = useState('');

  const current = draft;
  const status = statusQuery.data?.status;
  const jobs = jobsQuery.data?.jobs ?? [];
  const queues = queuesQuery.data?.queues ?? [];

  useEffect(() => {
    if (configQuery.data?.config) setDraft(configQuery.data.config);
  }, [configQuery.data?.config]);

  const saveConfig = useMutation({
    mutationFn: api.saveSubtitleConfig,
    onSuccess: (data) => {
      setDraft(data.config);
      setNotice('字幕配置已保存');
      queryClient.invalidateQueries({ queryKey: ['subtitle-config'] });
    },
    onError: (cause) => setNotice(errorMessage(cause)),
  });
  const check = useMutation({
    mutationFn: () => api.checkSubtitles(current),
    onSuccess: (data) => {
      const label = subtitleModeLabel(data.check.mode);
      setNotice(data.check.ok ? `${label}检测通过：${data.check.message}` : `${label}检测失败：${data.check.message}`);
    },
    onError: (cause) => setNotice(errorMessage(cause)),
  });
  const reset = useMutation({
    mutationFn: api.resetImmediateSubtitles,
    onSuccess: () => {
      setNotice('立即字幕队列已重置');
      invalidate(queryClient);
    },
  });
  const retry = useMutation({
    mutationFn: api.retrySubtitleJob,
    onSuccess: () => invalidate(queryClient),
  });
  const removeJob = useMutation({
    mutationFn: api.deleteSubtitleJob,
    onSuccess: () => invalidate(queryClient),
  });
  const createQueue = useMutation({
    mutationFn: () => api.createSubtitleQueue(queueName, queueCron),
    onSuccess: () => {
      setQueueName('');
      invalidate(queryClient);
    },
    onError: (cause) => setNotice(errorMessage(cause)),
  });
  const runQueue = useMutation({
    mutationFn: api.runSubtitleQueueNow,
    onSuccess: () => invalidate(queryClient),
  });
  const deleteQueue = useMutation({
    mutationFn: api.deleteSubtitleQueue,
    onSuccess: () => invalidate(queryClient),
  });
  const scanExisting = useMutation({
    mutationFn: async () => {
      const saved = await api.saveSubtitleConfig({ ...current, mode: 'scan' });
      setDraft(saved.config);
      return api.scanExistingSubtitles();
    },
    onSuccess: (data) => {
      setNotice(`已加入 CLI 扫描队列：${data.creator_count} 个UP主`);
      invalidate(queryClient);
    },
    onError: (cause) => setNotice(errorMessage(cause)),
  });

  const updateDraft = (patch: Partial<SubtitleConfig>) => {
    setDraft((value) => ({ ...value, ...patch }));
    if (typeof patch.mode === 'string') setNotice('');
  };
  const mode = current.mode === 'api' || current.mode === 'scan' ? current.mode : 'local';
  const isLocalMode = mode === 'local';
  const isAPIMode = mode === 'api';
  const isScanMode = mode === 'scan';

  return (
    <div className="page updates-page subtitles-page">
      <header className="page-header">
        <h1>字幕管理</h1>
        <p>管理视频转字幕运行模式、立即队列、定时队列和历史任务。</p>
      </header>

      <section className="settings-card">
        <h2><Captions size={22} /> 字幕运行配置</h2>
        <div className="settings-grid subtitle-config-grid">
          <label>
            模式
            <select value={current.mode} onChange={(event) => updateDraft({ mode: event.target.value })}>
              <option value="local">本地内置</option>
              <option value="api">外部 API</option>
              <option value="scan">CLI 扫描</option>
            </select>
          </label>
          {isLocalMode && (
            <>
              <label>
                并发数量
                <input type="number" min={1} max={6} value={current.concurrency} onChange={(event) => updateDraft({ concurrency: Number(event.target.value) || 1 })} />
              </label>
              <label>
                语言
                <input value={current.language} onChange={(event) => updateDraft({ language: event.target.value })} />
              </label>
              <label className="inline-check">
                <input type="checkbox" checked={current.gpu} onChange={(event) => updateDraft({ gpu: event.target.checked })} />
                启用 GPU
              </label>
              <label>
                subtitle-cli 路径
                <input value={current.cli_path ?? ''} onChange={(event) => updateDraft({ cli_path: event.target.value })} placeholder="留空使用内置 subtitle-cli" />
              </label>
              <label>
                whisper-cli
                <input value={current.whisper_path ?? ''} onChange={(event) => updateDraft({ whisper_path: event.target.value })} />
              </label>
              <label>
                ffmpeg
                <input value={current.ffmpeg_path ?? ''} onChange={(event) => updateDraft({ ffmpeg_path: event.target.value })} />
              </label>
              <label>
                模型路径
                <input value={current.model_path ?? ''} onChange={(event) => updateDraft({ model_path: event.target.value })} />
              </label>
            </>
          )}
          {isAPIMode && (
            <label>
              外部 API
              <input value={current.api_url ?? ''} onChange={(event) => updateDraft({ api_url: event.target.value })} placeholder="http://host:9099" />
            </label>
          )}
          {isScanMode && (
            <div className="mode-help subtitle-scan-help">
              <strong>CLI 扫描</strong>
              <span>只扫描UP主目录中已经由外部 CLI 生成的同名字幕文件，不执行 whisper 转写。</span>
            </div>
          )}
        </div>
        <div className="modal-actions subtitle-config-actions">
          <button type="button" onClick={() => saveConfig.mutate(current)}>保存字幕配置</button>
          <button type="button" onClick={() => check.mutate()}>检测配置</button>
          {isScanMode && <button type="button" onClick={() => scanExisting.mutate()} disabled={scanExisting.isPending}>扫描已有字幕</button>}
        </div>
        {notice && <p className="success-line">{notice}</p>}
      </section>

      <div className="updates-grid">
        <section className="update-card update-status-card">
          <h2><RefreshCw size={22} /> 立即转字幕队列</h2>
          <p>{status?.running ? `正在转字幕 ${status.current_creator ?? ''}` : '当前没有正在执行的字幕任务'}</p>
          <button type="button" className="danger-button" onClick={() => reset.mutate()}>重置</button>
          <div className="progress-bar"><span style={{ width: `${status?.progress ?? 0}%` }} /></div>
          <div className="update-stats">
            <div><strong>{status?.pending_creators ?? 0}</strong><span>待转UP主</span></div>
            <div><strong>{status?.succeeded_creators ?? 0}</strong><span>成功UP主</span></div>
            <div><strong>{status?.failed_creators ?? 0}</strong><span>失败UP主</span></div>
            <div><strong>{status?.subtitle_works ?? 0}</strong><span>字幕作品</span></div>
          </div>
          {(status?.running_jobs ?? []).map((job) => <SubtitleJobRow key={job.id} job={job} running />)}
        </section>

        <section className="update-card">
          <h2>定时转字幕队列</h2>
          <div className="queue-form">
            <input placeholder="队列名称" value={queueName} onChange={(event) => setQueueName(event.target.value)} />
            <input placeholder="30 3 * * *" value={queueCron} onChange={(event) => setQueueCron(event.target.value)} />
            <button type="button" onClick={() => createQueue.mutate()} disabled={!queueName.trim()}>新增队列</button>
          </div>
          {queues.map((queue) => (
            <div className="queue-row" key={queue.id}>
              <div>
                <strong>{queue.name}</strong>
                <span>{queue.cron} · {queue.subscribed_count} 个UP主 · 下次 {queue.next_run_at || '-'}</span>
                <small>{queue.last_run_summary || '暂无上次运行摘要'}</small>
              </div>
              <div className="queue-actions">
                <button type="button" onClick={() => runQueue.mutate(queue.id)}><Play size={16} /> 立即运行</button>
                <button type="button" className="danger-button" onClick={() => deleteQueue.mutate(queue.id)}><Trash2 size={16} /> 删除</button>
              </div>
            </div>
          ))}
        </section>
      </div>

      <section className="settings-card">
        <h2>字幕任务历史</h2>
        <div className="job-list">
          {jobs.map((job) => (
            <SubtitleJobRow
              key={job.id}
              job={job}
              onRetry={job.status === 'failed' ? () => retry.mutate(job.id) : undefined}
              onDelete={job.status !== 'running' ? () => removeJob.mutate(job.id) : undefined}
            />
          ))}
        </div>
      </section>
    </div>
  );
}

function SubtitleJobRow({ job, running = false, onRetry, onDelete }: { job: SubtitleJob; running?: boolean; onRetry?: () => void; onDelete?: () => void }) {
  return (
    <div className="job-row">
      <div>
        <strong>{job.creator_name}</strong>
        {job.work_title && <small>{job.work_title}</small>}
      </div>
      <span>{job.status}{running ? ' · running' : ''}</span>
      <p>{job.progress_message || job.error || `来源 ${job.source} · ${job.processed_works ?? 0}/${job.total_works ?? 0}`}</p>
      <div className="job-actions">
        {onRetry && <button type="button" onClick={onRetry}>重试</button>}
        {onDelete && <button type="button" className="danger-button" onClick={onDelete}>删除</button>}
      </div>
    </div>
  );
}

function invalidate(queryClient: ReturnType<typeof useQueryClient>) {
  queryClient.invalidateQueries({ queryKey: ['subtitle-status'] });
  queryClient.invalidateQueries({ queryKey: ['subtitle-jobs'] });
  queryClient.invalidateQueries({ queryKey: ['subtitle-queues'] });
  queryClient.invalidateQueries({ queryKey: ['creators'] });
}

function errorMessage(cause: unknown) {
  if (cause instanceof ApiError) return cause.message;
  if (cause instanceof Error) return cause.message;
  return '操作失败';
}

function subtitleModeLabel(mode: string) {
  if (mode === 'api') return '外部 API ';
  if (mode === 'scan') return 'CLI 扫描 ';
  return '本地内置 ';
}
