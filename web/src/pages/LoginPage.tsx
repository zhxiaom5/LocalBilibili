import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../lib/api';

export function LoginPage({ redirectTo = '/' }: { redirectTo?: string }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const login = useMutation({
    mutationFn: () => api.login(username, password),
    onSuccess: async () => {
      setError('');
      await queryClient.invalidateQueries({ queryKey: ['auth-me'] });
      navigate(redirectTo || '/', { replace: true });
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : '登录失败');
    },
  });

  return (
    <div className="page login-page">
      <div className="login-card">
        <div className="login-brand">LocalBilibili</div>
        <h1>登录 LocalBilibili</h1>
        <p>忘记密码请联系管理员重置。</p>
        <form
          className="login-form"
          onSubmit={(event) => {
            event.preventDefault();
            login.mutate();
          }}
        >
          <label>
            <span>用户名</span>
            <input aria-label="用户名" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
          </label>
          <label>
            <span>密码</span>
            <input aria-label="密码" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
          </label>
          <button type="submit" disabled={login.isPending}>
            登录
          </button>
        </form>
        {error && <p className="error-line">{error}</p>}
      </div>
    </div>
  );
}
