import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Shield, UserPlus, UsersRound } from "lucide-react";
import { useMemo, useState } from "react";
import { api } from "../lib/api";
import type { User } from "../lib/types";

type UserManagementPageProps = {
  currentUser: User;
  onUserChange?: () => void;
};

export function UserManagementPage({
  currentUser,
  onUserChange,
}: UserManagementPageProps) {
  const queryClient = useQueryClient();
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [newUsername, setNewUsername] = useState("");
  const [newUserPassword, setNewUserPassword] = useState("");
  const [newUserPasswordConfirm, setNewUserPasswordConfirm] = useState("");
  const [resetPasswords, setResetPasswords] = useState<Record<number, string>>(
    {},
  );
  const [passwordError, setPasswordError] = useState("");
  const [managementError, setManagementError] = useState("");
  const [viewError, setViewError] = useState("");
  const isAdmin = currentUser.is_admin;
  const isSuperAdmin = currentUser.is_super_admin;

  const users = useQuery({
    queryKey: ["users"],
    queryFn: api.users,
    enabled: isAdmin,
  });
  const viewContext = useQuery({
    queryKey: ["view-context"],
    queryFn: api.viewContext,
    enabled: isSuperAdmin,
  });
  const userList = users.data?.users ?? [];

  const changePassword = useMutation({
    mutationFn: () => api.changePassword(oldPassword, newPassword),
    onSuccess: () => {
      setOldPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setPasswordError("");
      onUserChange?.();
    },
    onError: (cause: unknown) => setPasswordError(errorMessage(cause)),
  });
  const createUser = useMutation({
    mutationFn: () => api.createUser(newUsername, newUserPassword),
    onSuccess: () => {
      setNewUsername("");
      setNewUserPassword("");
      setNewUserPasswordConfirm("");
      setManagementError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (cause: unknown) => setManagementError(errorMessage(cause)),
  });
  const resetUserPassword = useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) =>
      api.resetUserPassword(id, password),
    onSuccess: (_data, variables) => {
      setResetPasswords((current) => ({ ...current, [variables.id]: "" }));
      setManagementError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (cause: unknown) => setManagementError(errorMessage(cause)),
  });
  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setManagementError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (cause: unknown) => setManagementError(errorMessage(cause)),
  });
  const setViewContext = useMutation({
    mutationFn: api.setViewContext,
    onSuccess: () => {
      setViewError("");
      queryClient.invalidateQueries({ queryKey: ["view-context"] });
      onUserChange?.();
    },
    onError: (cause: unknown) => setViewError(errorMessage(cause)),
  });
  const clearViewContext = useMutation({
    mutationFn: api.clearViewContext,
    onSuccess: () => {
      setViewError("");
      queryClient.invalidateQueries({ queryKey: ["view-context"] });
      onUserChange?.();
    },
    onError: (cause: unknown) => setViewError(errorMessage(cause)),
  });

  const passwordMismatch = useMemo(
    () =>
      newPassword.trim() !== "" &&
      confirmPassword.trim() !== "" &&
      newPassword !== confirmPassword,
    [confirmPassword, newPassword],
  );
  const createMismatch = useMemo(
    () =>
      newUserPassword.trim() !== "" &&
      newUserPasswordConfirm.trim() !== "" &&
      newUserPassword !== newUserPasswordConfirm,
    [newUserPassword, newUserPasswordConfirm],
  );

  return (
    <div className="page narrow-page user-management-page">
      <header className="page-header">
        <h1>用户管理</h1>
        <p>管理本地访问账号和当前账户密码。</p>
      </header>

      <section className="account-layout">
        <article className="account-card">
          <div className="account-card-head">
            <Shield size={18} />
            <div>
              <h2>我的账户</h2>
              <p>
                @{currentUser.username} · {isAdmin ? "管理员" : "普通用户"}
              </p>
            </div>
          </div>
          <form
            className="account-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!passwordMismatch) changePassword.mutate();
            }}
          >
            <label>
              <span>旧密码</span>
              <input
                aria-label="旧密码"
                type="password"
                value={oldPassword}
                onChange={(event) => setOldPassword(event.target.value)}
                autoComplete="current-password"
              />
            </label>
            <label>
              <span>新密码</span>
              <input
                aria-label="新密码"
                type="password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                autoComplete="new-password"
              />
            </label>
            <label>
              <span>确认新密码</span>
              <input
                aria-label="确认新密码"
                type="password"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
                autoComplete="new-password"
              />
            </label>
            <button
              type="submit"
              disabled={
                changePassword.isPending ||
                passwordMismatch ||
                !oldPassword ||
                !newPassword ||
                !confirmPassword
              }
            >
              <KeyRound size={16} /> 修改密码
            </button>
          </form>
          {passwordMismatch && (
            <p className="error-line">两次输入的新密码不一致。</p>
          )}
          {passwordError && <p className="error-line">{passwordError}</p>}
        </article>

        {isAdmin && (
          <article className="account-card">
            <div className="account-card-head">
              <UserPlus size={18} />
              <div>
                <h2>新增用户</h2>
                <p>管理员创建的用户默认为普通用户。</p>
              </div>
            </div>
            <form
              className="account-form"
              onSubmit={(event) => {
                event.preventDefault();
                if (!createMismatch) createUser.mutate();
              }}
            >
              <label>
                <span>新用户名</span>
                <input
                  aria-label="新用户名"
                  value={newUsername}
                  onChange={(event) => setNewUsername(event.target.value)}
                  autoComplete="off"
                />
              </label>
              <label>
                <span>初始密码</span>
                <input
                  aria-label="初始密码"
                  type="password"
                  value={newUserPassword}
                  onChange={(event) => setNewUserPassword(event.target.value)}
                  autoComplete="new-password"
                />
              </label>
              <label>
                <span>确认密码</span>
                <input
                  aria-label="确认密码"
                  type="password"
                  value={newUserPasswordConfirm}
                  onChange={(event) =>
                    setNewUserPasswordConfirm(event.target.value)
                  }
                  autoComplete="new-password"
                />
              </label>
              <button
                type="submit"
                disabled={
                  createUser.isPending ||
                  createMismatch ||
                  !newUsername ||
                  !newUserPassword ||
                  !newUserPasswordConfirm
                }
              >
                <UserPlus size={16} /> 新增用户
              </button>
            </form>
            {createMismatch && (
              <p className="error-line">两次输入的密码不一致。</p>
            )}
          </article>
        )}
      </section>

      {isAdmin && (
        <section className="account-card user-list-card">
          <div className="account-card-head">
            <UsersRound size={18} />
            <div>
              <h2>账户列表</h2>
              <p>重置或删除普通用户；当前管理员请在“我的账户”修改密码。</p>
            </div>
          </div>
          <div className="user-list">
            {userList.map((user) => (
              <div key={user.id} className="user-row">
                <div className="user-identity">
                  <strong>{user.username}</strong>
                  <span>{user.is_admin ? "管理员" : "普通用户"}</span>
                </div>
                <div className="user-reset">
                  <label>
                    <span>重置密码</span>
                    <input
                      aria-label={`重置密码-${user.id}`}
                      type="password"
                      value={resetPasswords[user.id] ?? ""}
                      onChange={(event) =>
                        setResetPasswords((current) => ({
                          ...current,
                          [user.id]: event.target.value,
                        }))
                      }
                      placeholder={
                        user.id === currentUser.id
                          ? "请在我的账户修改"
                          : "新密码"
                      }
                      disabled={user.id === currentUser.id}
                      autoComplete="new-password"
                    />
                  </label>
                  <button
                    type="button"
                    onClick={() =>
                      resetUserPassword.mutate({
                        id: user.id,
                        password: resetPasswords[user.id] ?? "",
                      })
                    }
                    disabled={
                      user.id === currentUser.id || !resetPasswords[user.id]
                    }
                  >
                    重置密码
                  </button>
                </div>
                <button
                  type="button"
                  className="danger"
                  onClick={() => deleteUser.mutate(user.id)}
                  disabled={user.id === currentUser.id}
                >
                  删除
                </button>
              </div>
            ))}
            {users.isLoading && (
              <div className="empty-state">加载用户中...</div>
            )}
            {!users.isLoading && userList.length === 0 && (
              <div className="empty-state">暂无用户。</div>
            )}
          </div>
          {managementError && <p className="error-line">{managementError}</p>}
        </section>
      )}

      {isSuperAdmin && (
        <section className="account-card user-list-card">
          <div className="account-card-head">
            <Shield size={18} />
            <div>
              <h2>查看视角</h2>
              <p>超级管理员可以切换到其他用户视角，检查收藏夹等用户侧状态。</p>
            </div>
          </div>
          <div className="user-list">
            <div className="user-row">
              <div className="user-identity">
                <strong>当前视角</strong>
                <span>
                  {viewContext.data
                    ? `正在以 ${viewContext.data.view_user.username} 查看`
                    : "加载中..."}
                </span>
              </div>
              <button
                type="button"
                onClick={() => clearViewContext.mutate()}
                disabled={clearViewContext.isPending}
              >
                恢复自己
              </button>
            </div>
            {userList.map((user) => (
              <div key={`view-${user.id}`} className="user-row">
                <div className="user-identity">
                  <strong>{user.username}</strong>
                  <span>
                    {user.role || (user.is_admin ? "管理员" : "普通用户")}
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => setViewContext.mutate(user.id)}
                  disabled={setViewContext.isPending}
                >
                  切换视角
                </button>
              </div>
            ))}
          </div>
          {viewError && <p className="error-line">{viewError}</p>}
        </section>
      )}
    </div>
  );
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : "操作失败";
}
