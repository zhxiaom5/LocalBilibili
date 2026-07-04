import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';
import { UserManagementPage } from './UserManagementPage';

const adminUser = { id: 1, username: 'ted', is_admin: true, created_at: '', updated_at: '' };
const normalUser = { id: 2, username: 'alice', is_admin: false, created_at: '', updated_at: '' };

function renderPage(currentUser = adminUser) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <UserManagementPage currentUser={currentUser} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/users' && init?.method === 'POST') {
        return Response.json({ user: { id: 3, username: 'bob', is_admin: false, created_at: '', updated_at: '' } }, { status: 201 });
      }
      if (url === '/api/users') {
        return Response.json({ users: [adminUser, normalUser] });
      }
      if (url.startsWith('/api/users/') && url.endsWith('/reset-password')) {
        return Response.json({ user: normalUser });
      }
      if (url.startsWith('/api/users/') && init?.method === 'DELETE') {
        return Response.json({ ok: true });
      }
      if (url === '/api/auth/password') {
        return Response.json({ user: adminUser });
      }
      return Response.json({ ok: true });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it('lets admins create, reset and delete users in the user management page', async () => {
  const user = userEvent.setup();
  renderPage();
  expect(await screen.findByRole('heading', { name: '用户管理' })).toBeInTheDocument();
  expect(screen.getByText('账户列表')).toBeInTheDocument();

  await user.type(screen.getByLabelText('新用户名'), 'bob');
  await user.type(screen.getByLabelText('初始密码'), 'secret123');
  await user.type(screen.getByLabelText('确认密码'), 'secret123');
  await user.click(screen.getByRole('button', { name: '新增用户' }));
  await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/users', expect.objectContaining({ method: 'POST' })));

  await user.type(screen.getByLabelText('重置密码-2'), 'reset123');
  await user.click(screen.getAllByRole('button', { name: '重置密码' })[1]);
  await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/users/2/reset-password', expect.objectContaining({ method: 'POST' })));

  await user.click(screen.getAllByRole('button', { name: '删除' })[1]);
  await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/users/2', expect.objectContaining({ method: 'DELETE' })));
});

it('shows only self password controls to normal users', async () => {
  renderPage(normalUser);
  expect(await screen.findByRole('heading', { name: '用户管理' })).toBeInTheDocument();
  expect(screen.getByText('我的账户')).toBeInTheDocument();
  expect(screen.queryByText('账户列表')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('新用户名')).not.toBeInTheDocument();
});
