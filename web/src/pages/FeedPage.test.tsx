import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, vi } from 'vitest';
import { FeedPage } from './FeedPage';

const randomWorks = [
  {
    id: 3,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    bvid: '1003',
    title: '第三条',
    description: '第三条',
    music_title: '',
    tags: [],
    published_at: '2026-03-12T00:00:00Z',
    media: [{ id: 4, work_id: 3, type: 'video', file_name: 'd.mp4', url: '/d.mp4', ordinal: 0 }],
  },
  {
    id: 2,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    bvid: '1002',
    title: '第二条',
    description: '第二条',
    music_title: '',
    tags: [],
    published_at: '2026-03-11T00:00:00Z',
    media: [{ id: 3, work_id: 2, type: 'video', file_name: 'c.mp4', url: '/c.mp4', ordinal: 0 }],
  },
  {
    id: 1,
    directory_id: 1,
    creator_id: 1,
    creator_name: '阿言',
    bvid: '1001',
    title: '第一条',
    description: '第一条',
    music_title: '',
    tags: [],
    published_at: '2026-03-10T00:00:00Z',
    media: [{ id: 1, work_id: 1, type: 'image', file_name: 'a.webp', url: '/a.webp', ordinal: 0 }],
  },
];

const descWorks = [
  {
    ...randomWorks[0],
  },
  {
    ...randomWorks[1],
  },
  {
    ...randomWorks[2],
  },
];

const ascWorks = [
  {
    ...randomWorks[2],
  },
  {
    ...randomWorks[1],
  },
  {
    ...randomWorks[0],
  },
];

function renderPage(path = '/') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<FeedPage />} />
          <Route path="/creator/:creatorId" element={<FeedPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  if (!window.localStorage) {
    const values = new Map<string, string>();
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
        clear: () => values.clear(),
        removeItem: (key: string) => values.delete(key),
      },
    });
  }
  window.localStorage?.clear();
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: vi.fn().mockResolvedValue(undefined) });
  Object.defineProperty(HTMLMediaElement.prototype, 'pause', { configurable: true, value: vi.fn() });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://localhost');
      if (url.pathname !== '/api/feed') {
        return Response.json({ ok: true });
      }
      const order = url.searchParams.get('order') ?? 'random';
      const works = order === 'asc' ? ascWorks : order === 'desc' ? descWorks : randomWorks;
      return Response.json({ works });
    }),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  window.localStorage?.clear();
});

function fireTouchSwipe(viewer: Element, startY = 620, moveY = 520, endY = 500) {
  fireEvent.touchStart(viewer, { touches: [{ clientY: startY }] });
  fireEvent.touchMove(viewer, { touches: [{ clientY: moveY }] });
  fireEvent.touchEnd(viewer, { changedTouches: [{ clientY: endY }] });
}

async function openAdvancedControls(user: ReturnType<typeof userEvent.setup>) {
  const button = await screen.findByLabelText('更多播放控制');
  if (button.getAttribute('aria-expanded') !== 'true') {
    await user.click(button);
  }
}

it('re-fetches feed with the selected order and keeps the current work when possible', async () => {
  const user = userEvent.setup();
  renderPage('/creator/1');

  expect(await screen.findByText(/第三条/)).toBeInTheDocument();
  await openAdvancedControls(user);
  await user.click(screen.getByLabelText('播放顺序'));

  await waitFor(() => {
    const calls = vi.mocked(fetch).mock.calls.map(([input]) => String(input));
    expect(calls.some((url) => url.includes('/api/feed') && url.includes('order=desc'))).toBe(true);
  });
  expect(screen.getByText(/第三条/)).toBeInTheDocument();
  expect(window.localStorage.getItem('localbilibili.feedOrder')).toBe('desc');
});

it('allows wheel navigation after opening a creator feed with an initial work id', async () => {
  renderPage('/creator/1230?work_id=2');

  expect(await screen.findByText(/第二条/)).toBeInTheDocument();
  fireEvent.wheel(screen.getByLabelText('作品播放器').closest('.feed-viewer')!, { deltaY: 120 });
  expect(screen.getByText(/第一条/)).toBeInTheDocument();

  const calls = vi.mocked(fetch).mock.calls.map(([input]) => String(input));
  expect(calls.some((url) => url.includes('/api/feed') && url.includes('creator_id=1230'))).toBe(true);
});

it('does not feed the current work back as a new initial work after touch navigation', async () => {
  renderPage('/creator/1230?work_id=3');

  expect(await screen.findByText(/第三条/)).toBeInTheDocument();
  const viewer = screen.getByLabelText('作品播放器').closest('.feed-viewer')!;

  act(() => {
    fireTouchSwipe(viewer);
  });
  expect(screen.getByText(/第二条/)).toBeInTheDocument();

  act(() => {
    fireTouchSwipe(viewer);
  });
  expect(screen.getByText(/第一条/)).toBeInTheDocument();
  expect(screen.queryByText(/第三条/)).not.toBeInTheDocument();
});

it('cycles play order through desc asc and back to random', async () => {
  const user = userEvent.setup();
  renderPage('/');

  await screen.findByText(/第三条/);
  await openAdvancedControls(user);
  const orderButton = screen.getByLabelText('播放顺序');
  expect(orderButton).toHaveTextContent('随机');

  fireEvent.click(orderButton);
  await openAdvancedControls(user);
  await waitFor(() => expect(screen.getByLabelText('播放顺序')).toHaveTextContent('倒序'));

  fireEvent.click(screen.getByLabelText('播放顺序'));
  await openAdvancedControls(user);
  await waitFor(() => expect(screen.getByLabelText('播放顺序')).toHaveTextContent('顺序'));

  fireEvent.click(screen.getByLabelText('播放顺序'));
  await openAdvancedControls(user);
  await waitFor(() => expect(screen.getByLabelText('播放顺序')).toHaveTextContent('随机'));
});
