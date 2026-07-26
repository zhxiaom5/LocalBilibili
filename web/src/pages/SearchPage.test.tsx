import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SearchPage } from './SearchPage';

const works = [
  { id: 11, creator_id: 1, creator_name: '阿言', bvid: 'BVvideo', title: '夏天视频', description: '', music_title: '', tags: [], published_at: '2026-07-01T00:00:00Z', media: [{ id: 1, work_id: 11, type: 'video', file_name: 'a.mp4', url: '/a.mp4', ordinal: 0 }] },
  { id: 12, creator_id: 1, creator_name: '阿言', bvid: 'dynamic', title: '夏天图文', description: '', music_title: '', tags: [], published_at: '2026-07-02T00:00:00Z', media: [{ id: 2, work_id: 12, type: 'image', file_name: 'a.jpg', url: '/a.jpg', ordinal: 0 }] },
];

function Location() { const location = useLocation(); return <output data-testid="location">{location.pathname + location.search}</output>; }
function renderSearch(path = '/search') {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter initialEntries={[path]}><Location /><Routes><Route path="/search" element={<SearchPage />} /><Route path="/search/play" element={<div>搜索播放页</div>} /></Routes></MemoryRouter></QueryClientProvider>);
}

beforeEach(() => vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
  const url = new URL(String(input), 'http://localhost');
  if (url.pathname !== '/api/feed') return Response.json({ ok: true });
  if (url.searchParams.get('search') === '错误') return new Response(JSON.stringify({ error: '服务异常' }), { status: 500, headers: { 'Content-Type': 'application/json' } });
  return Response.json({ works: url.searchParams.get('search') === '空' ? [] : works });
}))); 
afterEach(() => vi.unstubAllGlobals());

it('submits independent search and opens the selected work in the search playback route', async () => {
  const user = userEvent.setup(); renderSearch();
  await user.type(screen.getByLabelText('搜索本地作品'), '夏天');
  await user.click(screen.getByRole('button', { name: '搜索' }));
  await waitFor(() => expect(screen.getByText('夏天视频')).toBeInTheDocument());
  expect(screen.getByTestId('location')).toHaveTextContent('/search?q=%E5%A4%8F%E5%A4%A9');
  await user.click(screen.getByRole('link', { name: /夏天图文/ }));
  expect(screen.getByTestId('location')).toHaveTextContent('/search/play?q=%E5%A4%8F%E5%A4%A9&work_id=12');
});

it('renders initial, empty, and error states', async () => {
  const user = userEvent.setup(); renderSearch();
  expect(screen.getByText('输入关键词开始搜索本地作品。')).toBeInTheDocument();
  await user.type(screen.getByLabelText('搜索本地作品'), '空'); await user.click(screen.getByRole('button', { name: '搜索' }));
  await waitFor(() => expect(screen.getByText(/没有找到与/)).toBeInTheDocument());
  await user.clear(screen.getByLabelText('搜索本地作品')); await user.type(screen.getByLabelText('搜索本地作品'), '错误'); await user.click(screen.getByRole('button', { name: '搜索' }));
  await waitFor(() => expect(screen.getByText(/搜索失败：服务异常/)).toBeInTheDocument());
});
