import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { CreatorsPage } from './CreatorsPage';

const currentUser = { id: 1, username: 'ted', is_admin: true, is_super_admin: true, role: 'super_admin', created_at: '', updated_at: '' };

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><MemoryRouter><CreatorsPage currentUser={currentUser} /></MemoryRouter></QueryClientProvider>);
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost');
    if (url.pathname === '/api/creators') {
      return Response.json({ creators: [
        { id: 1, directory_id: 1, name: '阿尔法', work_count: 3, subtitle_count: 0, view_count: 0, online_identity_status: 'ready' },
        { id: 2, directory_id: 1, name: '贝塔', work_count: 2, subtitle_count: 0, view_count: 0, online_identity_status: 'ready' },
      ] });
    }
    if (url.pathname === '/api/directories') return Response.json({ directories: [] });
    if (url.pathname === '/api/update-queues') return Response.json({ queues: [] });
    return Response.json({ ok: true });
  }));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it('requests all-work-count sorting in both directions from the follow page', async () => {
  const user = userEvent.setup();
  renderPage();

  await screen.findByText('阿尔法');
  await user.selectOptions(screen.getByLabelText('UP主排序字段'), 'work_count');
  await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([input]) => String(input).includes('/api/creators?') && String(input).includes('sort=work_count') && String(input).includes('order=desc'))).toBe(true));

  await user.click(screen.getByRole('button', { name: '倒序' }));
  await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([input]) => String(input).includes('/api/creators?') && String(input).includes('sort=work_count') && String(input).includes('order=asc'))).toBe(true));
});
