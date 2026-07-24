import {
  QueryClient,
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  BrowserRouter,
  Navigate,
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  Captions,
  ChevronsLeft,
  ChevronsRight,
  FolderHeart,
  Gauge,
  Heart,
  Home,
  Info,
  List,
  LogOut,
  Menu,
  RefreshCw,
  ScrollText,
  Settings,
  UserRound,
  UsersRound,
} from "lucide-react";
import { api } from "./lib/api";
import { useRealtimeEvents } from "./lib/useRealtimeEvents";
import { AboutPage } from "./pages/AboutPage";
import { CreatorsPage } from "./pages/CreatorsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { FavoriteCreatorsPage, FavoriteWorksPage } from "./pages/FavoritesPage";
import { FeedPage } from "./pages/FeedPage";
import { LoginPage } from "./pages/LoginPage";
import { LogsPage } from "./pages/LogsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { SubtitlesPage } from "./pages/SubtitlesPage";
import { UpdatesPage } from "./pages/UpdatesPage";
import { UserManagementPage } from "./pages/UserManagementPage";
import type { User } from "./lib/types";

export function App() {
  const [queryClient] = useState(() => createQueryClient());

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
      },
    },
  });
}

function AppRoutes() {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const auth = useQuery({ queryKey: ["auth-me"], queryFn: api.me });

  useEffect(() => {
    const onUnauthorized = () => {
      queryClient.invalidateQueries({ queryKey: ["auth-me"] });
    };
    window.addEventListener("localbilibili:unauthorized", onUnauthorized);
    return () =>
      window.removeEventListener("localbilibili:unauthorized", onUnauthorized);
  }, [queryClient]);

  const redirectTo =
    new URLSearchParams(location.search).get("redirect") || "/";
  const user = auth.data?.user ?? null;

  if (auth.isLoading) {
    return <div className="empty-state">加载登录状态...</div>;
  }

  if (!user) {
    return (
      <Routes>
        <Route
          path="/login"
          element={
            <LoginPage
              redirectTo={redirectTo === "/login" ? "/" : redirectTo}
            />
          }
        />
        <Route
          path="*"
          element={
            <Navigate
              to={`/login?redirect=${encodeURIComponent(location.pathname + location.search)}`}
              replace
            />
          }
        />
      </Routes>
    );
  }

  if (location.pathname === "/login") {
    return <Navigate to={redirectTo === "/login" ? "/" : redirectTo} replace />;
  }

  return (
    <AppShell
      user={user}
      onLogout={async () => await handleLogout(queryClient, navigate)}
    />
  );
}

function AppShell({
  user,
  onLogout,
}: {
  user: User;
  onLogout: () => Promise<void>;
}) {
  const queryClient = useQueryClient();
  useRealtimeEvents();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const closeMobile = () => setMobileOpen(false);

  return (
    <div
      className={`app-shell ${mobileOpen ? "mobile-nav-open" : ""} ${sidebarCollapsed ? "sidebar-collapsed" : ""}`}
    >
      {!mobileOpen && (
        <button
          className="mobile-nav-toggle"
          type="button"
          onClick={() => setMobileOpen(true)}
          aria-label="打开菜单"
        >
          <Menu size={22} />
        </button>
      )}
      <aside className="sidebar">
        <div className="sidebar-top">
          {!sidebarCollapsed && (
            <div className="brand">
              <img className="brand-logo" src="/favicon.svg" alt="" />
              <span>LocalBilibili</span>
            </div>
          )}
          <button
            className="sidebar-collapse-toggle"
            type="button"
            onClick={() =>
              mobileOpen
                ? closeMobile()
                : setSidebarCollapsed((current) => !current)
            }
            aria-label={
              mobileOpen ? "关闭菜单" : sidebarCollapsed ? "展开菜单" : "隐藏菜单"
            }
            title={
              mobileOpen ? "关闭菜单" : sidebarCollapsed ? "展开菜单" : "隐藏菜单"
            }
          >
            {mobileOpen ? (
              <ChevronsLeft size={20} />
            ) : sidebarCollapsed ? (
              <ChevronsRight size={20} />
            ) : (
              <ChevronsLeft size={20} />
            )}
          </button>
        </div>
        <nav>
          <NavLink to="/" end title="首页" onClick={closeMobile}>
            <Home size={20} /> <span>首页</span>
          </NavLink>
          <NavLink to="/creators" title="关注" onClick={closeMobile}>
            <List size={20} /> <span>关注</span>
          </NavLink>
          <NavLink
            to="/favorite-works"
            title="作品收藏夹"
            onClick={closeMobile}
          >
            <Heart size={20} /> <span>作品收藏夹</span>
          </NavLink>
          <NavLink
            to="/favorite-creators"
            title="作者收藏夹"
            onClick={closeMobile}
          >
            <FolderHeart size={20} /> <span>作者收藏夹</span>
          </NavLink>
          <NavLink to="/users" title="用户管理" onClick={closeMobile}>
            <UsersRound size={20} /> <span>用户管理</span>
          </NavLink>
          {user.is_admin && (
            <NavLink to="/updates" title="更新管理" onClick={closeMobile}>
              <RefreshCw size={20} /> <span>更新管理</span>
            </NavLink>
          )}
          {user.is_admin && (
            <NavLink to="/subtitles" title="字幕管理" onClick={closeMobile}>
              <Captions size={20} /> <span>字幕管理</span>
            </NavLink>
          )}
          {user.is_super_admin && (
            <NavLink to="/logs" title="日志记录" onClick={closeMobile}>
              <ScrollText size={20} /> <span>日志记录</span>
            </NavLink>
          )}
          <NavLink to="/diagnostics" title="诊断中心" onClick={closeMobile}>
            <Gauge size={20} /> <span>诊断中心</span>
          </NavLink>
          <NavLink to="/settings" title="设置" onClick={closeMobile}>
            <Settings size={20} /> <span>设置</span>
          </NavLink>
          <NavLink to="/about" title="关于" onClick={closeMobile}>
            <Info size={20} /> <span>关于</span>
          </NavLink>
        </nav>
        <div className="sidebar-user">
          <div className="sidebar-user-info">
            <UserRound size={18} />
            {!sidebarCollapsed && (
              <div>
                <strong>{user.username}</strong>
                <small>
                  {user.is_super_admin
                    ? "超级管理员"
                    : user.is_admin
                      ? "管理员"
                      : "普通用户"}
                </small>
              </div>
            )}
          </div>
          <button
            className="sidebar-logout"
            type="button"
            onClick={() => void onLogout()}
            title="退出登录"
            aria-label="退出登录"
          >
            <LogOut size={18} />
            {!sidebarCollapsed && <span>退出</span>}
          </button>
        </div>
      </aside>
      <main className="main-area">
        <Routes>
          <Route path="/" element={<FeedPage />} />
          <Route
            path="/creators"
            element={<CreatorsPage currentUser={user} />}
          />
          <Route path="/creator/:creatorId" element={<FeedPage />} />
          <Route path="/favorite-works" element={<FavoriteWorksPage />} />
          <Route
            path="/favorite-works/:folderId"
            element={<FavoriteWorksPage />}
          />
          <Route
            path="/favorite-works/:favoriteFolderId/play"
            element={<FeedPage />}
          />
          <Route path="/favorite-creators" element={<FavoriteCreatorsPage />} />
          <Route
            path="/favorite-creators/:folderId"
            element={<FavoriteCreatorsPage />}
          />
          <Route
            path="/updates"
            element={
              user.is_admin ? <UpdatesPage /> : <Navigate to="/" replace />
            }
          />
          <Route
            path="/subtitles"
            element={
              user.is_admin ? <SubtitlesPage /> : <Navigate to="/" replace />
            }
          />
          <Route
            path="/users"
            element={
              <UserManagementPage
                currentUser={user}
                onUserChange={() =>
                  void queryClient.invalidateQueries({ queryKey: ["auth-me"] })
                }
              />
            }
          />
          <Route
            path="/logs"
            element={
              user.is_super_admin ? <LogsPage /> : <Navigate to="/" replace />
            }
          />
          <Route path="/diagnostics" element={<DiagnosticsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/about" element={<AboutPage />} />
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}

async function handleLogout(
  queryClient: QueryClient,
  navigate: ReturnType<typeof useNavigate>,
) {
  await api.logout();
  await queryClient.invalidateQueries({ queryKey: ["auth-me"] });
  navigate("/login", { replace: true });
}
