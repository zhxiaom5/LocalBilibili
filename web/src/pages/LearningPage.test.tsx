import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';
import { LearningPage } from './LearningPage';

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><LearningPage /></QueryClientProvider>);
}

function stubLearningFetch() {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const config = { codex_path: '', wisemapping_url: 'http://wise', wisemapping_user: 'ted', default_cron: '0 4 * * *', password_configured: true, work_prompt: '作品 XX', creator_prompt: 'UP主 XX' };
    if (url === '/api/learning/config' && init?.method === 'PUT') return Response.json({ config: JSON.parse(String(init.body)) });
    if (url === '/api/learning/config') return Response.json({ config });
    if (url === '/api/learning/codex/login') return Response.json({ login: { state: 'logged_in', message: 'codex-cli 0.144.4' } });
    if (url === '/api/learning/status') return Response.json({ status: { running: false, progress: 0, pending: 0, succeeded: 0, failed: 0, jobs: [] } });
    if (url === '/api/learning/jobs') return Response.json({ jobs: [] });
    if (url === '/api/learning/runs') return Response.json({ runs: [{ id: 1, queue_name: '每日学习', status: 'succeeded', total_jobs: 2, succeeded: 2, failed: 0, summary: '完成', enqueued_at: '2026-07-20T00:00:00Z' }] });
    if (url === '/api/learning/queues') return Response.json({ queues: [{ id: 1, name: '每日学习', cron: '0 4 * * *', subscribed_count: 2, last_run_summary: '完成' }] });
    if (url === '/api/creators') return Response.json({ creators: [{ id: 11, name: '甲UP主', work_count: 8 }, { id: 12, name: '乙UP主', work_count: 3 }] });
    return Response.json({ ok: true });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => vi.unstubAllGlobals());

it('places scheduled queue history below the immediate queue and persists both prompts', async () => {
  const user = userEvent.setup();
  const fetchMock = stubLearningFetch();
  renderPage();
  expect(await screen.findByRole('heading', { name: '即时队列' })).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: '定时学习队列与运行历史' })).toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText('单作品提示词')).toHaveValue('作品 XX'));
  expect(screen.getByLabelText('UP主提示词')).toHaveValue('UP主 XX');
  expect(screen.getByText('定时队列运行历史')).toBeInTheDocument();
  await user.clear(screen.getByLabelText('单作品提示词'));
  await user.type(screen.getByLabelText('单作品提示词'), '新的作品 XX');
  await user.click(screen.getByRole('button', { name: '保存 Codex 与提示词配置' }));
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/learning/config', expect.objectContaining({ method: 'PUT', body: expect.stringContaining('新的作品 XX') })));
  await user.click(screen.getByText('请选择UP主'));
  expect(await screen.findByText('8 个作品')).toBeInTheDocument();
  await user.click(screen.getByRole('checkbox', { name: /甲UP主/ }));
  await user.click(screen.getByRole('checkbox', { name: /乙UP主/ }));
  await user.click(screen.getByRole('button', { name: /生成导图（2 项）/ }));
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/learning/immediate', expect.objectContaining({ method: 'POST', body: expect.stringContaining('"creator_ids":[11,12]') })));
});

it('refreshes the final job rows after the learning runner becomes idle', async () => {
  const fetchMock = stubLearningFetch();
  renderPage();
  await screen.findByRole('heading', { name: '即时队列' });
  await waitFor(() => {
    const jobRequests = fetchMock.mock.calls.filter(([input]) => String(input) === '/api/learning/jobs');
    expect(jobRequests.length).toBeGreaterThanOrEqual(2);
  });
});
