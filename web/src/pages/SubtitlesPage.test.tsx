import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';
import { SubtitlesPage } from './SubtitlesPage';

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SubtitlesPage />
    </QueryClientProvider>,
  );
}

function stubSubtitleFetch(initialMode: 'local' | 'api' | 'scan' = 'local') {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === '/api/subtitles/config' && init?.method === 'PUT') {
      return Response.json({ config: JSON.parse(String(init.body)) });
    }
    if (url === '/api/subtitles/config') {
      return Response.json({
        config: {
          mode: initialMode,
          concurrency: 1,
          language: 'zh',
          gpu: true,
          cli_path: '/opt/localbilibili/subtitle-cli',
          api_url: 'http://host:9099',
          whisper_path: '/opt/whisper-cli',
          ffmpeg_path: '/opt/ffmpeg',
          model_path: '/opt/model.bin',
        },
      });
    }
    if (url === '/api/subtitles/check') {
      const body = init?.body ? JSON.parse(String(init.body)) : { mode: initialMode };
      return Response.json({ check: { ok: true, mode: body.mode, message: `${body.mode}-ok` } });
    }
    if (url === '/api/subtitles/status') {
      return Response.json({
        status: {
          running: false,
          pending_creators: 0,
          total_creators: 0,
          succeeded_creators: 0,
          failed_creators: 0,
          subtitle_works: 0,
          progress: 0,
          state: 'idle',
          errors: [],
          running_jobs: [],
          jobs: [],
        },
      });
    }
    if (url.startsWith('/api/subtitles/jobs')) {
      return Response.json({ jobs: [], page: 1, page_size: 10, total: 0, total_pages: 0 });
    }
    if (url === '/api/subtitles/queues') {
      return Response.json({
        queues: [
          {
            id: 1,
            name: '默认字幕队列',
            cron: '30 3 * * *',
            is_default: true,
            enabled: true,
            next_run_at: '',
            last_run_summary: '',
            subscribed_count: 0,
            created_at: '',
            updated_at: '',
          },
        ],
      });
    }
    if (url === '/api/subtitles/scan-existing') {
      return Response.json(
        {
          creator_count: 2,
          status: {
            running: true,
            pending_creators: 2,
            total_creators: 2,
            succeeded_creators: 0,
            failed_creators: 0,
            subtitle_works: 0,
            progress: 0,
            state: 'queued',
            errors: [],
            running_jobs: [],
            jobs: [],
          },
        },
        { status: 202 },
      );
    }
    return Response.json({ ok: true });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

it('shows only fields that belong to the selected subtitle mode', async () => {
  const user = userEvent.setup();
  stubSubtitleFetch('local');
  renderPage();
  expect(await screen.findByText('字幕运行配置')).toBeInTheDocument();
  expect(screen.getByText('subtitle-cli 路径')).toBeInTheDocument();
  expect(screen.getByText('whisper-cli')).toBeInTheDocument();
  expect(screen.getByText('ffmpeg')).toBeInTheDocument();
  expect(screen.getByText('模型路径')).toBeInTheDocument();
  expect(screen.queryByPlaceholderText('http://host:9099')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: '扫描已有字幕' })).not.toBeInTheDocument();
  expect(screen.queryByRole('option', { name: 'CLI 扫码' })).not.toBeInTheDocument();

  await user.selectOptions(screen.getByLabelText('模式'), 'api');
  expect(screen.getByPlaceholderText('http://host:9099')).toBeInTheDocument();
  expect(screen.queryByText('subtitle-cli 路径')).not.toBeInTheDocument();
  expect(screen.queryByText('whisper-cli')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: '扫描已有字幕' })).not.toBeInTheDocument();

  await user.selectOptions(screen.getByLabelText('模式'), 'scan');
  expect(screen.getByText(/只扫描UP主目录中已经由外部 CLI 生成的同名字幕文件/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: '扫描已有字幕' })).toBeInTheDocument();
  expect(screen.queryByPlaceholderText('http://host:9099')).not.toBeInTheDocument();
  expect(screen.queryByText('whisper-cli')).not.toBeInTheDocument();
});

it('saves scan mode before scanning existing subtitle files', async () => {
  const user = userEvent.setup();
  const fetchMock = stubSubtitleFetch('scan');
  renderPage();
  expect(await screen.findByRole('button', { name: '扫描已有字幕' })).toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: '扫描已有字幕' }));
  await waitFor(() => {
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/subtitles/config',
      expect.objectContaining({ method: 'PUT' }),
    );
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/subtitles/scan-existing',
      expect.objectContaining({ method: 'POST' }),
    );
  });
  expect(await screen.findByText('已加入 CLI 扫描队列：2 个UP主')).toBeInTheDocument();
});

it('clears stale check result when mode changes and checks the selected mode', async () => {
  const user = userEvent.setup();
  const fetchMock = stubSubtitleFetch('local');
  renderPage();
  expect(await screen.findByText('字幕运行配置')).toBeInTheDocument();

  await user.click(screen.getByRole('button', { name: '检测配置' }));
  expect(await screen.findByText('本地内置 检测通过：local-ok')).toBeInTheDocument();

  await user.selectOptions(screen.getByLabelText('模式'), 'api');
  expect(screen.queryByText('本地内置 检测通过：local-ok')).not.toBeInTheDocument();

  await user.click(screen.getByRole('button', { name: '检测配置' }));
  expect(await screen.findByText('外部 API 检测通过：api-ok')).toBeInTheDocument();
  await waitFor(() => {
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/subtitles/check',
      expect.objectContaining({
        method: 'POST',
        body: expect.stringContaining('"mode":"api"'),
      }),
    );
  });
});
