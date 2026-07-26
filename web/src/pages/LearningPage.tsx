import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Brain, KeyRound, Play, RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api, ApiError } from '../lib/api';
import type { LearningConfig, LearningJob, LearningRun } from '../lib/types';

const empty: LearningConfig = { default_cron: '0 4 * * *', password_configured: false, work_prompt: '', creator_prompt: '' };

export function LearningPage() {
  const qc = useQueryClient();
  const config = useQuery({ queryKey: ['learning-config'], queryFn: api.learningConfig });
  const codex = useQuery({ queryKey: ['codex-login'], queryFn: api.codexLoginStatus, refetchInterval: query => query.state.data?.login.state === 'pending' ? 2000 : false });
  const status = useQuery({ queryKey: ['learning-status'], queryFn: api.learningStatus, refetchInterval: query => query.state.data?.status.running ? 1500 : false });
  const jobs = useQuery({ queryKey: ['learning-jobs'], queryFn: api.learningJobs, refetchInterval: () => status.data?.status.running ? 1500 : false });
  const runs = useQuery({ queryKey: ['learning-runs'], queryFn: api.learningRuns, refetchInterval: () => status.data?.status.running ? 1500 : false });
  const queues = useQuery({ queryKey: ['learning-queues'], queryFn: api.learningQueues });
  const creators = useQuery({ queryKey: ['creators'], queryFn: () => api.creators() });
  const [draft, setDraft] = useState<LearningConfig>(empty);
  const [creatorIDs, setCreatorIDs] = useState<number[]>([]);
  const [workID, setWorkID] = useState('');
  const [queueName, setQueueName] = useState('');
  const [queueCron, setQueueCron] = useState('0 4 * * *');
  const [notice, setNotice] = useState('');

  useEffect(() => { if (config.data?.config) setDraft(config.data.config); }, [config.data]);
  useEffect(() => {
    if (status.data?.status.running === false) {
      void qc.invalidateQueries({ queryKey: ['learning-jobs'] });
      void qc.invalidateQueries({ queryKey: ['learning-runs'] });
    }
  }, [qc, status.data?.status.running]);
  const refresh = () => {
    for (const key of [['learning-status'], ['learning-jobs'], ['learning-runs'], ['learning-queues'], ['learning-config'], ['codex-login']]) {
      void qc.invalidateQueries({ queryKey: key });
    }
  };
  const save = useMutation({
    mutationFn: api.saveLearningConfig,
    onSuccess: data => { setDraft({ ...data.config, wisemapping_password: '' }); setNotice('学习配置已保存，后续导图会使用新提示词'); refresh(); },
    onError: error => setNotice(message(error)),
  });
  const check = useMutation({ mutationFn: api.checkLearning, onSuccess: data => setNotice(data.check.ok ? `检测通过：${data.check.codex_version ?? ''}` : `检测失败：${data.check.message ?? ''}`), onError: error => setNotice(message(error)) });
  const checkWise = useMutation({ mutationFn: api.checkWiseMapping, onSuccess: () => setNotice('WiseMapping 连接成功'), onError: error => setNotice(message(error)) });
  const startLogin = useMutation({ mutationFn: api.startCodexLogin, onSuccess: refresh, onError: error => setNotice(message(error)) });
  const cancelLogin = useMutation({ mutationFn: api.cancelCodexLogin, onSuccess: refresh });
  const logout = useMutation({ mutationFn: api.logoutCodex, onSuccess: refresh, onError: error => setNotice(message(error)) });
  const workIDs = parseIDs(workID);
  const enqueue = useMutation({ mutationFn: () => api.enqueueImmediateLearning(creatorIDs, workIDs), onSuccess: () => { setNotice(`已加入学习生成队列：${workIDs.length || creatorIDs.length} 项`); refresh(); }, onError: error => setNotice(message(error)) });
  const reset = useMutation({ mutationFn: api.resetImmediateLearning, onSuccess: refresh });
  const create = useMutation({ mutationFn: () => api.createLearningQueue(queueName, queueCron), onSuccess: () => { setQueueName(''); refresh(); }, onError: error => setNotice(message(error)) });
  const run = useMutation({ mutationFn: api.runLearningQueueNow, onSuccess: refresh });
  const login = codex.data?.login;
  const migration = draft.credential_migration;
  const activeJobs = (status.data?.status.jobs ?? []).filter(job => job.status === 'pending' || job.status === 'running');
  const immediateJobs = (jobs.data?.jobs ?? []).filter(job => !job.queue_id);
  const selectedCreatorNames = (creators.data?.creators ?? []).filter(creator => creatorIDs.includes(creator.id)).map(creator => creator.name);
  const toggleCreator = (creatorID: number) => setCreatorIDs(ids => ids.includes(creatorID) ? ids.filter(id => id !== creatorID) : [...ids, creatorID]);

  return <div className="page updates-page subtitles-page">
    <header className="page-header"><h1>学习管理</h1><p>将已生成字幕输入 Codex，生成私有 WiseMapping 思维导图。每作品一张，并汇总为UP主总图。</p></header>
    {migration && <section className="settings-card learning-migration-card"><h2>认证初始化</h2><p>{migration.codex === 'copied' || migration.wisemapping === 'copied' ? '已从 LocalDouyin 首次复制认证；当前配置已独立，后续不会同步。' : '首次认证复制已检查；未复制的项目请在本页独立配置。'}</p><small>Codex：{migration.codex} · WiseMapping：{migration.wisemapping}</small></section>}

    <section className="settings-card">
      <h2><KeyRound size={22} /> Codex 登录、运行状态与提示词</h2>
      <p>安装状态：{login?.state === 'logged_in' ? '已登录' : '未登录'} {login?.message && `· ${login.message}`}</p>
      {login?.state === 'pending'
        ? <div className="mode-help"><strong>在另一台浏览器打开：<a href={login.verification_url} target="_blank" rel="noreferrer">{login.verification_url || '等待授权链接'}</a></strong><span>一次性代码：<b>{login.user_code || '正在获取'}</b>。完成授权后本页会自动刷新。</span><button type="button" className="danger-button" onClick={() => cancelLogin.mutate()}>取消登录</button></div>
        : <div className="modal-actions"><button type="button" onClick={() => startLogin.mutate()} disabled={startLogin.isPending}>开始设备登录</button>{login?.state === 'logged_in' && <button type="button" className="danger-button" onClick={() => logout.mutate()}>退出登录</button>}</div>}
      <div className="settings-grid subtitle-config-grid">
        <label>Codex CLI 路径<input value={draft.codex_path ?? ''} onChange={event => setDraft({ ...draft, codex_path: event.target.value })} placeholder="留空使用 fnOS 内置 codex" /></label>
        <label>单作品提示词<textarea value={draft.work_prompt} onChange={event => setDraft({ ...draft, work_prompt: event.target.value })} rows={4} placeholder="使用 XX 代表当前作品的完整字幕" /></label>
        <label>UP主提示词<textarea value={draft.creator_prompt} onChange={event => setDraft({ ...draft, creator_prompt: event.target.value })} rows={4} placeholder="使用 XX 代表该UP主的全部字幕" /></label>
      </div>
      <p className="mode-help">提示词中的 <code>XX</code> 会替换为字幕原文；若未填写 <code>XX</code>，系统会自动在提示词后附加字幕。保存后会用于下一次 Codex 生成，提示词变化会重新生成对应导图。</p>
      <div className="modal-actions subtitle-config-actions"><button onClick={() => save.mutate(draft)} type="button" disabled={save.isPending}>保存 Codex 与提示词配置</button></div>
    </section>

    <section className="settings-card">
      <h2><Brain size={22} /> WiseMapping 配置</h2>
      <div className="settings-grid subtitle-config-grid">
        <label>WiseMapping 地址<input value={draft.wisemapping_url ?? ''} onChange={event => setDraft({ ...draft, wisemapping_url: event.target.value })} placeholder="http://wisemapping:8080" /></label>
        <label>WiseMapping 账号<input value={draft.wisemapping_user ?? ''} onChange={event => setDraft({ ...draft, wisemapping_user: event.target.value })} /></label>
        <label>WiseMapping 密码<input type="password" value={draft.wisemapping_password ?? ''} onChange={event => setDraft({ ...draft, wisemapping_password: event.target.value })} placeholder={draft.password_configured ? '已配置；留空则不修改' : '输入密码'} /></label>
        <label>默认 cron<input value={draft.default_cron} onChange={event => setDraft({ ...draft, default_cron: event.target.value })} /></label>
        <div className="mode-help"><strong>密码状态：{draft.password_configured ? '已配置' : '未配置'}</strong><span>密码只保存到 fnOS 私有文件，不会回传网页。</span></div>
      </div>
      <div className="modal-actions subtitle-config-actions"><button onClick={() => save.mutate(draft)} type="button" disabled={save.isPending}>保存 WiseMapping 配置</button><button onClick={() => checkWise.mutate()} type="button">测试 WiseMapping</button><button onClick={() => check.mutate()} type="button">检测全部</button></div>
      {notice && <p className="success-line">{notice}</p>}
    </section>

    <section className="update-card update-status-card immediate-learning-card">
      <h2><RefreshCw size={22} /> 即时队列</h2>
      <p>{status.data?.status.running ? `正在处理 ${status.data.status.current_creator ?? ''} · ${status.data.status.progress ?? 0}%` : '当前没有任务；请选择UP主或批量作品。'}</p>
      <div className="creator-picker-field"><span>UP主（可多选）</span><details className="creator-multi-select"><summary>{selectedCreatorNames.length ? `已选择 ${selectedCreatorNames.length} 位UP主：${selectedCreatorNames.join('、')}` : '请选择UP主'}</summary><div className="creator-multi-options">{(creators.data?.creators ?? []).map(creator => <label key={creator.id}><input type="checkbox" checked={creatorIDs.includes(creator.id)} onChange={() => toggleCreator(creator.id)} /><span>{creator.name}</span><small>{creator.work_count} 个作品</small></label>)}</div><div className="creator-picker-actions"><button type="button" onClick={() => setCreatorIDs([])} disabled={!creatorIDs.length}>清空选择</button></div></details></div>
      <label>作品 ID（可批量：空格、逗号或换行分隔）<textarea value={workID} onChange={event => setWorkID(event.target.value)} placeholder={'例如：146607, 146608\n146609'} rows={3} /></label>
      <div className="modal-actions"><button type="button" disabled={(!creatorIDs.length && !workIDs.length) || enqueue.isPending} onClick={() => enqueue.mutate()}><Play size={16} />生成导图{creatorIDs.length + workIDs.length > 1 ? `（${creatorIDs.length + workIDs.length} 项）` : ''}</button><button type="button" className="danger-button" onClick={() => reset.mutate()}>取消并重置</button></div>
      <div className="progress-bar"><span style={{ width: `${status.data?.status.progress ?? 0}%` }} /></div>
      <div className="metric-grid"><Metric label="等待" value={status.data?.status.pending ?? 0} /><Metric label="成功" value={status.data?.status.succeeded ?? 0} /><Metric label="失败" value={status.data?.status.failed ?? 0} /></div>
      <h3>正在处理</h3><div className="job-list">{activeJobs.map(job => <Job key={job.id} job={job} />)}{!activeJobs.length && <div className="empty-state">当前没有正在处理的学习任务。</div>}</div>
      <h3>即时队列记录</h3><div className="job-list">{immediateJobs.map(job => <Job key={job.id} job={job} />)}{!immediateJobs.length && <div className="empty-state">暂无即时生成记录。</div>}</div>
    </section>

    <section className="settings-card scheduled-learning-card">
      <h2>定时学习队列与运行历史</h2>
      <div className="queue-form"><input placeholder="队列名称" value={queueName} onChange={event => setQueueName(event.target.value)} /><input value={queueCron} onChange={event => setQueueCron(event.target.value)} /><button type="button" disabled={!queueName.trim()} onClick={() => create.mutate()}>新增队列</button></div>
      <div className="job-list">{(queues.data?.queues ?? []).map(queue => <div className="queue-row" key={queue.id}><div><strong>{queue.name}</strong><span>{queue.cron} · {queue.subscribed_count} 个UP主</span><small>{queue.last_run_summary || '定时任务只处理字幕内容变化的作品'}</small></div><button type="button" onClick={() => run.mutate(queue.id)}><Play size={16} />立即运行</button></div>)}</div>
      <h3>定时队列运行历史</h3><div className="job-list">{(runs.data?.runs ?? []).map(runItem => <Run key={runItem.id} run={runItem} />)}{!(runs.data?.runs ?? []).length && <div className="empty-state">暂无定时队列运行记录。</div>}</div>
    </section>
  </div>;
}

function Metric({ label, value }: { label: string; value: number }) { return <div><small>{label}</small><strong>{value}</strong></div>; }
function Job({ job }: { job: LearningJob }) { return <div className="job-row learning-job-row"><div><strong title={`${job.creator_name}${job.work_title ? ` · ${job.work_title}` : ' · UP主聚合'}`}>{job.creator_name}{job.work_title ? ` · ${job.work_title}` : ' · UP主聚合'}</strong><span>{job.status} · {job.progress_message || job.error || '等待处理'} · 触发 {formatTime(job.created_at)}{job.started_at && ` · 开始 ${formatTime(job.started_at)}`}{job.finished_at && ` · 结束 ${formatTime(job.finished_at)}`}</span></div></div>; }
function Run({ run }: { run: LearningRun }) { const progress = run.total_jobs ? Math.round((run.succeeded + run.failed) * 100 / run.total_jobs) : 0; return <div className="job-row learning-job-row"><div><strong>{run.queue_name}</strong><span>{run.status} · {run.summary || '等待处理'} · {run.succeeded}/{run.total_jobs} 成功，{run.failed} 失败 · {progress}%</span><small>触发 {formatTime(run.enqueued_at)}{run.started_at && ` · 开始 ${formatTime(run.started_at)}`}{run.finished_at && ` · 结束 ${formatTime(run.finished_at)}`}</small></div></div>; }
function parseIDs(value: string) { return [...new Set((value.match(/\d+/g) ?? []).map(Number).filter(Number.isSafeInteger))]; }
function formatTime(value?: string) { return value ? new Date(value).toLocaleString() : '-'; }
function message(error: unknown) { return error instanceof ApiError ? error.message : error instanceof Error ? error.message : '请求失败'; }
