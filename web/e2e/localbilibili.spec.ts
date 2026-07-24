import { expect, test, type Page, type Route } from '@playwright/test';

const authedUser = { id: 1, username: 'ted', is_admin: true, created_at: '', updated_at: '' };
let authState: 'guest' | 'authed' = 'authed';

test.beforeEach(() => {
  authState = 'authed';
});

async function installApiMock(page: Page, handler: (url: URL, route: Route) => Promise<boolean> | boolean) {
  await page.route('/api/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/auth/me') {
      if (authState === 'authed') {
        await route.fulfill({ json: { user: authedUser } });
      } else {
        await route.fulfill({ status: 401, json: { error: 'unauthorized' } });
      }
      return;
    }
    if (url.pathname === '/api/auth/login') {
      authState = 'authed';
      await route.fulfill({ json: { user: authedUser } });
      return;
    }
    if (url.pathname === '/api/auth/logout') {
      authState = 'guest';
      await route.fulfill({ json: { ok: true } });
      return;
    }
    if (url.pathname === '/api/auth/password') {
      await route.fulfill({ json: { user: authedUser } });
      return;
    }
    if (url.pathname === '/api/events') {
      await route.abort();
      return;
    }
    if (url.pathname === '/api/users') {
      await route.fulfill({ json: { users: [authedUser, { id: 2, username: 'alice', is_admin: false, created_at: '', updated_at: '' }] } });
      return;
    }
    if (url.pathname.startsWith('/api/users/')) {
      await route.fulfill({ json: { ok: true } });
      return;
    }
    const handled = await handler(url, route);
    if (!handled) {
      await route.fulfill({ json: { ok: true } });
    }
  });
}

async function prepareMockVideoDuration(page: Page, duration: number) {
  await page.evaluate((nextDuration) => {
    const video = document.querySelector('video');
    if (!video) throw new Error('video element not found');
    Object.defineProperty(video, 'duration', { configurable: true, value: nextDuration });
    video.dispatchEvent(new Event('loadedmetadata', { bubbles: true }));
  }, duration);
}

async function dragRangeByLabel(page: Page, label: string, ratio: number) {
  return page.evaluate(
    ({ labelText, targetRatio }) => {
      const input = Array.from(document.querySelectorAll<HTMLInputElement>('input[type="range"]')).find((item) => item.getAttribute('aria-label') === labelText);
      if (!input) throw new Error(`range not found: ${labelText}`);
      const rect = input.getBoundingClientRect();
      const startX = rect.left;
      const targetX = rect.left + rect.width * targetRatio;
      const clientY = rect.top + rect.height / 2;
      const dispatch = (type: string, clientX: number) => {
        input.dispatchEvent(
          new PointerEvent(type, {
            bubbles: true,
            cancelable: true,
            pointerId: 1,
            pointerType: 'touch',
            clientX,
            clientY,
          }),
        );
      };
      dispatch('pointerdown', startX);
      dispatch('pointermove', targetX);
      dispatch('pointerup', targetX);
      return input.value;
    },
    { labelText: label, targetRatio: ratio },
  );
}

test('settings page supports directory workflow shell', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/directories') {
      await route.fulfill({ json: { directories: [{ id: 1, path: '/sample/bilibili', name: 'bilibili', status: 'idle', creator_count: 1, work_count: 1, created_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/scan/status') {
      await route.fulfill({ json: { status: { running: true, queue_length: 0, current_directory: '/sample/bilibili', current_creator: 'creator', scanned_creators: 1, total_creators: 1, scanned_works: 1, progress: 100, state: 'completed', errors: [] } } });
      return true;
    }
    if (url.pathname === '/api/fs/roots') {
      await route.fulfill({ json: { roots: [{ path: '/sample', name: 'sample' }] } });
      return true;
    }
    return false;
  });

  await page.goto('/settings');
  await expect(page.getByText('目录配置')).toBeVisible();
  await expect(page.locator('.directory-row').getByText('/sample/bilibili')).toBeVisible();
  await expect(page.getByText('creator')).toBeVisible();
  await page.getByRole('button', { name: /浏览/ }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
});

test('unauthenticated users see the login page and can sign in', async ({ page }) => {
  authState = 'guest';
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/directories') {
      await route.fulfill({ json: { directories: [] } });
      return true;
    }
    if (url.pathname === '/api/scan/status') {
      await route.fulfill({ json: { status: { running: false, queue_length: 0, scanned_creators: 0, total_creators: 0, scanned_works: 0, progress: 0, state: 'idle', errors: [] } } });
      return true;
    }
    if (url.pathname === '/api/fs/roots') {
      await route.fulfill({ json: { roots: [] } });
      return true;
    }
    return false;
  });

  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: '登录 LocalBilibili' })).toBeVisible();
  await expect(page.getByText('忘记密码请联系管理员重置。')).toBeVisible();
  await page.getByLabel('用户名').fill('ted');
  await page.getByLabel('密码').fill('ted');
  await page.getByRole('button', { name: '登录' }).click();
  await expect(page.getByText('目录配置')).toBeVisible();
});

test('user management is a standalone page and settings no longer contains account panels', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/directories') {
      await route.fulfill({ json: { directories: [] } });
      return true;
    }
    if (url.pathname === '/api/scan/status') {
      await route.fulfill({ json: { status: { running: false, queue_length: 0, scanned_creators: 0, total_creators: 0, scanned_works: 0, progress: 0, state: 'idle', errors: [] } } });
      return true;
    }
    if (url.pathname === '/api/fs/roots') {
      await route.fulfill({ json: { roots: [] } });
      return true;
    }
    return false;
  });

  await page.goto('/settings');
  await expect(page.getByText('目录配置')).toBeVisible();
  await expect(page.getByRole('link', { name: /用户管理/ })).toBeVisible();
  await expect(page.getByText('我的账户')).not.toBeVisible();
  await expect(page.getByText('账户管理')).not.toBeVisible();

  await page.getByRole('link', { name: /用户管理/ }).click();
  await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '我的账户' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '账户列表' })).toBeVisible();
  await expect(page.locator('.user-row').filter({ hasText: 'alice' })).toBeVisible();
});

test('updates page supports reset queue editing and cookie echo', async ({ page }) => {
  let resetCalled = false;
  let queuePatched = false;
  let creatorAdded = false;
  let creatorRemoved = false;
  let retryCalled = false;
  let currentJob = { id: 1, creator_id: 1, creator_name: '阿言', status: 'failed', attempt: 1, added_works: 0, error: '缺少 Cookie' };
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/update/events') {
      await route.abort();
      return true;
    }
    if (url.pathname === '/api/update/status') {
      await route.fulfill({
        json: {
          status: {
            running: false,
            pending_creators: 1,
            total_creators: 2,
            succeeded_creators: 1,
            failed_creators: 1,
            added_works: 3,
            progress: 50,
            state: 'idle',
            errors: [],
            jobs: [currentJob],
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/jobs') {
      await route.fulfill({ json: { jobs: [currentJob], total: 1, page: 1, page_size: 10, total_pages: 1 } });
      return true;
    }
    if (url.pathname === '/api/update/jobs/1/retry') {
      retryCalled = true;
      currentJob = { ...currentJob, status: 'pending', attempt: 2, error: '' };
      await route.fulfill({
        json: {
          status: {
            running: false,
            pending_creators: 1,
            total_creators: 1,
            succeeded_creators: 0,
            failed_creators: 0,
            added_works: 0,
            progress: 0,
            state: 'queued',
            errors: [],
            jobs: [currentJob],
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/immediate/reset') {
      resetCalled = true;
      await route.fulfill({ json: { status: { running: false, pending_creators: 0, total_creators: 0, succeeded_creators: 0, failed_creators: 0, added_works: 0, progress: 0, state: 'idle', errors: [], jobs: [] } } });
      return true;
    }
    if (url.pathname === '/api/update/queues') {
      await route.fulfill({
        json: {
          queues: [
            {
              id: 1,
              name: '每日更新',
              cron: '0 3 * * *',
              enabled: true,
              is_default: true,
              next_run_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
              subscribed_count: 1,
              last_run_summary: '上次成功 1 个UP主，新增 3 个作品',
              created_at: '',
              updated_at: '',
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/queues/1') {
      queuePatched = route.request().method() === 'PATCH';
      await route.fulfill({ json: { ok: true } });
      return true;
    }
    if (url.pathname === '/api/update/queues/1/creators') {
      if (route.request().method() === 'POST') creatorAdded = true;
      await route.fulfill({ json: { creators: [{ id: 1, name: '阿言', work_count: 2, online_identity_status: 'ready' }] } });
      return true;
    }
    if (url.pathname === '/api/update/queues/1/creators/1') {
      creatorRemoved = true;
      await route.fulfill({ json: { ok: true } });
      return true;
    }
    if (url.pathname === '/api/update/cookie') {
      await route.fulfill({ json: { cookie: { configured: true, validation_status: 'valid', validation_message: 'ok', updated_at: '', value: 'msToken=one; ttwid=two' } } });
      return true;
    }
    if (url.pathname === '/api/creators') {
      await route.fulfill({
        json: {
          creators: [
            { id: 1, name: '阿言', work_count: 2, online_identity_status: 'ready' },
            { id: 2, name: '徐大川', work_count: 4, online_identity_status: 'ready' },
          ],
        },
      });
      return true;
    }
    return false;
  });

  await page.goto('/updates');
  await expect(page.getByRole('heading', { name: '更新管理' })).toBeVisible();
  await expect(page.locator('.cookie-editor')).toHaveValue('msToken=one; ttwid=two');
  await page.getByRole('button', { name: '浏览器获取 Cookie' }).click();
  await expect(page.getByText(/msToken=.*ttwid=.*odin_tt=/)).toBeVisible();
  await page.locator('.job-row').filter({ hasText: '阿言' }).getByRole('button', { name: '重试' }).click();
  await expect.poll(() => retryCalled).toBe(true);
  await expect(page.locator('.job-row').filter({ hasText: '阿言' })).toHaveCount(1);
  await expect(page.locator('.job-row').filter({ hasText: '阿言' })).toContainText('第 2 次');

  page.on('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', { name: '重置' }).click();
  await expect.poll(() => resetCalled).toBe(true);

  await page.getByRole('button', { name: '编辑' }).click();
  const editor = page.getByRole('dialog', { name: '编辑定时队列' });
  await expect(editor).toBeVisible();
  await expect(editor.getByLabel('队列名称')).toHaveValue('每日更新');
  await editor.getByLabel('队列名称').fill('早晨更新');
  await editor.locator('.queue-member-row').filter({ hasText: '阿言' }).getByRole('button', { name: '移除' }).click();
  await expect.poll(() => creatorRemoved).toBe(true);
  await editor.locator('.queue-member-row').filter({ hasText: '徐大川' }).getByRole('button', { name: '添加' }).click();
  await expect.poll(() => creatorAdded).toBe(true);
  await editor.getByRole('button', { name: '保存' }).click();
  await expect.poll(() => queuePatched).toBe(true);
});

test('updates page keeps long retry errors inside mobile viewport', async ({ page }) => {
  const pageErrors: string[] = [];
  const consoleErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('net::ERR_FAILED')) consoleErrors.push(message.text());
  });
  const longError = 'context deadline exceeded '.repeat(16) + '(Client.Timeout or context cancellation while reading body)';
  const job = { id: 9, creator_id: 3, creator_name: '全哥价值投资', status: 'failed', attempt: 3, added_works: 0, error: longError };
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/update/events') {
      await route.abort();
      return true;
    }
    if (url.pathname === '/api/update/status') {
      await route.fulfill({
        json: {
          status: {
            running: false,
            pending_creators: 0,
            total_creators: 1,
            succeeded_creators: 0,
            failed_creators: 1,
            added_works: 0,
            progress: 100,
            state: 'failed',
            errors: [longError],
            jobs: [job],
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/jobs') {
      await route.fulfill({ json: { jobs: [job], total: 1, page: 1, page_size: 10, total_pages: 1 } });
      return true;
    }
    if (url.pathname === '/api/update/queues') {
      await route.fulfill({ json: { queues: [] } });
      return true;
    }
    if (url.pathname === '/api/update/cookie') {
      await route.fulfill({ json: { cookie: { configured: true, validation_status: 'valid', validation_message: 'ok', updated_at: '', value: 'msToken=one' } } });
      return true;
    }
    if (url.pathname === '/api/config/runtime') {
      await route.fulfill({ json: { runtime: { downloader_concurrency: 1 } } });
      return true;
    }
    if (url.pathname === '/api/diagnostics') {
      await route.fulfill({ json: { diagnostics: { playback: { recent_errors: [] }, recent_warnings: [] } } });
      return true;
    }
    return false;
  });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/updates');
  await expect(page.locator('.job-row').filter({ hasText: '全哥价值投资' })).toBeVisible();
  const hasHorizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
  expect(hasHorizontalOverflow).toBe(false);
  expect(pageErrors).toEqual([]);
  expect(consoleErrors).toEqual([]);
});

test('subtitles page keeps its configuration form responsive on narrow screens', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/subtitles/events') {
      await route.abort();
      return true;
    }
    if (url.pathname === '/api/subtitles/config') {
      await route.fulfill({
        json: {
          config: {
            mode: 'local',
            concurrency: 1,
            language: 'zh',
            gpu: true,
            cli_path: '',
            api_url: '',
            whisper_path: '',
            ffmpeg_path: '',
            model_path: '',
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/subtitles/status') {
      await route.fulfill({
        json: {
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
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/subtitles/jobs') {
      await route.fulfill({ json: { jobs: [], total: 0, page: 1, page_size: 10, total_pages: 0 } });
      return true;
    }
    if (url.pathname === '/api/subtitles/queues') {
      await route.fulfill({
        json: {
          queues: [
            {
              id: 1,
              name: '默认字幕队列',
              cron: '30 3 * * *',
              enabled: true,
              is_default: true,
              next_run_at: '',
              subscribed_count: 0,
              last_run_summary: '',
              created_at: '',
              updated_at: '',
            },
          ],
        },
      });
      return true;
    }
    return false;
  });

  await page.setViewportSize({ width: 754, height: 837 });
  await page.goto('/subtitles');
  await expect(page.getByRole('heading', { name: '字幕管理' })).toBeVisible();
  await expect(page.getByLabel('subtitle-cli 路径')).toBeVisible();
  await expect(page.getByRole('button', { name: '保存字幕配置' })).toBeVisible();

  const hasHorizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
  expect(hasHorizontalOverflow).toBe(false);

  const configGrid = page.locator('.subtitle-config-grid');
  await expect(configGrid).toBeVisible();
  const gridBox = await configGrid.boundingBox();
  const pageBox = await page.locator('.subtitles-page').boundingBox();
  expect(gridBox?.width ?? 0).toBeLessThanOrEqual((pageBox?.width ?? 0) + 1);
});

test('immersive feed shows overlays and sends search query', async ({ page }) => {
  let searched = false;
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      searched = url.searchParams.get('search') === '夏天';
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: '夏天',
              description: '第一条描述',
              music_title: '音乐',
              tags: ['测试'],
              published_at: '',
              media: [{ id: 1, work_id: 1, type: 'image', file_name: 'a.webp', url: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    if (url.pathname === '/api/favorite-folders') {
      await route.fulfill({ json: { folders: [{ id: 1, type: url.searchParams.get('type'), name: '默认收藏夹', is_default: true, created_at: '', updated_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/favorites/status') {
      await route.fulfill({ json: { status: { work_folder_ids: [], creator_folder_ids: [] } } });
      return true;
    }
    return false;
  });

  await page.goto('/');
  await expect(page.getByLabel('作品播放器')).toBeVisible();
  await page.getByLabel('作品播放器').hover();
  await expect(page.getByLabel('打开搜索')).toBeVisible();
  await expect(page.locator('.top-meta')).toHaveCount(0);
  await expect(page.locator('.creator-info-panel')).toBeVisible();
  await expect(page.getByLabel('作品操作')).toBeVisible();
  await expect(page.getByTitle('收藏该作品')).toBeVisible();
  await expect(page.getByTitle('收藏该作者')).toBeVisible();
  await expect(page.getByTitle('评论')).not.toBeVisible();
  await expect(page.getByTitle('分享')).not.toBeVisible();
  await expect(page.getByTitle('音频')).not.toBeVisible();
  await expect(page.getByTitle('UP主')).not.toBeVisible();
  await page.getByLabel('打开搜索').click();
  await expect(page.getByLabel('搜索本地作品')).toBeVisible();
  await page.getByLabel('搜索本地作品').fill('夏天');
  await page.getByRole('button', { name: /搜索/ }).click();
  await expect.poll(() => searched).toBe(true);
});

test('desktop sidebar can collapse and restore', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: '第一条',
              description: '第一条描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [{ id: 1, work_id: 1, type: 'image', file_name: 'a.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23355%22/%3E%3C/svg%3E', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/favorite-folders') {
      await route.fulfill({ json: { folders: [{ id: 1, type: url.searchParams.get('type'), name: '默认收藏夹', is_default: true, created_at: '', updated_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/favorites/status') {
      await route.fulfill({ json: { status: { work_folder_ids: [], creator_folder_ids: [] } } });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  await page.goto('/');
  const before = await page.locator('.feed-viewer').boundingBox();
  await page.getByLabel('隐藏菜单').click();
  await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
  await expect(page.getByLabel('展开菜单')).toBeVisible();
  const after = await page.locator('.feed-viewer').boundingBox();
  expect(after!.width).toBeGreaterThan(before!.width);
  await page.getByLabel('展开菜单').click();
  await expect(page.locator('.app-shell')).not.toHaveClass(/sidebar-collapsed/);
});

test('mobile feed overlays do not collide or dominate the player', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: 'kkcuda1',
              aweme_id: '1001',
              title: '三天三夜',
              description: '三天三夜，三更半夜，永远不停歇 查看封面查看更多特别长的签名',
              music_title: '来源',
              tags: ['测试', '移动端', '长标签长标签'],
              published_at: '2026-02-22T00:00:00Z',
              media: [{ id: 1, work_id: 1, type: 'video', file_name: 'a.mp4', url: 'data:video/mp4;base64,', ordinal: 0 }],
            },
            {
              id: 2,
              directory_id: 1,
              creator_id: 1,
              creator_name: 'kkcuda1',
              aweme_id: '1002',
              title: '第二条移动端',
              description: '第二条移动端描述',
              music_title: '',
              tags: [],
              published_at: '2026-02-23T00:00:00Z',
              media: [{ id: 2, work_id: 2, type: 'image', file_name: 'b.webp', url: 'data:image/gif;base64,R0lGODlhAQABAAAAACw=', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/favorite-folders') {
      await route.fulfill({ json: { folders: [{ id: 1, type: url.searchParams.get('type'), name: '默认收藏夹', is_default: true, created_at: '', updated_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/favorites/status') {
      await route.fulfill({ json: { status: { work_folder_ids: [], creator_folder_ids: [] } } });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  await page.goto('/');
  await page.getByLabel('作品播放器').click({ position: { x: 180, y: 260 } });
  await expect(page.getByLabel('打开菜单')).toBeVisible();
  await expect(page.getByLabel('打开搜索')).toBeVisible();
  await expect(page.locator('.top-meta')).toHaveCount(0);
  await expect(page.locator('.creator-info-panel')).toBeVisible();
  await expect(page.getByRole('link', { name: '@kkcuda1' })).toBeVisible();
  await expect(page.getByTitle('收藏该作品')).toBeVisible();
  await expect(page.getByTitle('收藏该作者')).toBeVisible();
  await expect(page.locator('.feed-viewer')).toHaveCSS('touch-action', 'none');
  await expect(page.locator('.feed-viewer')).toHaveCSS('overscroll-behavior', 'contain');
  await page.getByLabel('更多播放控制').click();
  await expect(page.getByLabel('音量')).toHaveCSS('touch-action', 'pan-x');
  await expect(page.getByLabel('播放速度')).toHaveCSS('touch-action', 'pan-x');
  await expect(page.getByLabel('视频进度')).toHaveCSS('touch-action', 'pan-x');
  await expect(page.getByLabel('更多播放控制')).toBeVisible();
  await expect(page.getByText('目录')).toHaveCount(0);
  await expect(page.getByLabel('悬浮透明度')).not.toBeVisible();

  await prepareMockVideoDuration(page, 100);
  await expect(page.getByLabel('视频进度')).toHaveAttribute('max', '100');
  expect(await dragRangeByLabel(page, '视频进度', 0.5)).toBe('50');
  expect(await dragRangeByLabel(page, '音量', 0.75)).toBe('0.75');
  expect(await dragRangeByLabel(page, '播放速度', 0.5)).toBe('1.5');
  await expect(page.locator('video')).toHaveJSProperty('playbackRate', 1.5);
  await expect(page.getByLabel('悬浮透明度')).toHaveCount(0);
  await expect(page.getByText(/三天三夜/)).toBeVisible();
  await expect(page.getByText('第二条移动端描述')).not.toBeVisible();

  const boxes = await page.evaluate(() => {
    const box = (selector: string) => {
      const rect = document.querySelector(selector)!.getBoundingClientRect();
      return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, width: rect.width, height: rect.height };
    };
    return {
      menu: box('.mobile-nav-toggle'),
      search: box('.feed-top-actions'),
      creatorInfo: box('.creator-info-panel'),
      rail: box('.action-rail'),
      videoControls: box('.video-controls'),
      advancedControls: box('.advanced-control-panel'),
      viewer: box('.feed-viewer'),
      railButtons: Array.from(document.querySelectorAll('.rail-button')).map((button) => {
        const rect = button.getBoundingClientRect();
        return { width: rect.width, height: rect.height };
      }),
    };
  });

  expect(boxes.menu.right).toBeLessThanOrEqual(boxes.search.left - 8);
  expect(boxes.creatorInfo.right).toBeLessThanOrEqual(boxes.videoControls.left - 8);
  expect(boxes.creatorInfo.height).toBeLessThanOrEqual(96);
  expect(boxes.rail.width).toBeLessThanOrEqual(52);
  expect(boxes.rail.right).toBeLessThanOrEqual(boxes.viewer.right - 6);
  expect(boxes.videoControls.height).toBeLessThanOrEqual(120);
  expect(boxes.advancedControls.height).toBeLessThanOrEqual(boxes.viewer.height * 0.38 + 1);
  for (const button of boxes.railButtons) {
    expect(button.width).toBeLessThanOrEqual(46);
    expect(button.height).toBeLessThanOrEqual(48);
  }
});

test('hidden video overlays keep only a minimal timeline and restore control bar', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: '视频作品',
              description: '视频描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [{ id: 1, work_id: 1, type: 'video', file_name: 'a.mp4', url: 'data:video/mp4;base64,', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/favorite-folders') {
      await route.fulfill({ json: { folders: [{ id: 1, type: url.searchParams.get('type'), name: '默认收藏夹', is_default: true, created_at: '', updated_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/favorites/status') {
      await route.fulfill({ json: { status: { work_folder_ids: [], creator_folder_ids: [] } } });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  await page.goto('/');
  await page.getByLabel('作品播放器').hover();
  await expect(page.getByLabel('视频控制')).toBeVisible();
  await prepareMockVideoDuration(page, 100);
  await page.getByLabel('更多播放控制').click();
  await page.getByLabel('关闭悬浮').click();
  await expect(page.getByLabel('搜索本地作品')).not.toBeVisible();
  await expect(page.getByLabel('作品操作')).not.toBeVisible();
  await expect(page.locator('.video-controls')).toHaveCount(0);
  await expect(page.getByLabel('视频极简进度')).toBeVisible();
  await expect(page.getByLabel('打开悬浮')).toBeVisible();
  await expect(page.getByLabel('视频极简进度')).toHaveCSS('touch-action', 'pan-x');
  expect(await dragRangeByLabel(page, '视频极简进度', 0.8)).toBe('80');

  const timelineBox = await page.locator('.minimal-video-timeline').boundingBox();
  expect(timelineBox!.height).toBeLessThanOrEqual(18);

  await page.getByLabel('打开悬浮').click();
  await expect(page.getByLabel('视频控制')).toBeVisible();
  await page.getByLabel('更多播放控制').click();
  await expect(page.getByLabel('音量')).toBeVisible();
});

test('video picture in picture control enters exits and fits viewports', async ({ page }) => {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('net::ERR_FAILED')) consoleErrors.push(message.text());
  });
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.addInitScript(() => {
    let pictureInPictureElement: Element | null = null;
    Object.defineProperty(document, 'pictureInPictureEnabled', {
      configurable: true,
      get: () => true,
    });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pictureInPictureElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: () => {
        const element = pictureInPictureElement;
        pictureInPictureElement = null;
        window.localStorage.setItem('pip-exited', 'yes');
        element?.dispatchEvent(new Event('leavepictureinpicture'));
        return Promise.resolve();
      },
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', {
      configurable: true,
      value: function requestPictureInPicture() {
        pictureInPictureElement = this;
        window.localStorage.setItem('pip-requested', 'yes');
        this.dispatchEvent(new Event('enterpictureinpicture'));
        return Promise.resolve({ width: 320, height: 180 });
      },
    });
    Object.defineProperty(HTMLMediaElement.prototype, 'play', {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });
  let feedRequests = 0;
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      feedRequests += 1;
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: '视频作品',
              description: '视频描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [{ id: 1, work_id: 1, type: 'video', file_name: 'a.mp4', url: 'data:video/mp4;base64,', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/directories') {
      await route.fulfill({ json: { directories: [] } });
      return true;
    }
    if (url.pathname === '/api/scan/status') {
      await route.fulfill({ json: { status: { running: false, queue_length: 0, scanned_creators: 0, total_creators: 0, scanned_works: 0, progress: 0, state: 'idle', errors: [] } } });
      return true;
    }
    if (url.pathname === '/api/fs/roots') {
      await route.fulfill({ json: { roots: [] } });
      return true;
    }
    if (url.pathname === '/api/favorite-folders') {
      await route.fulfill({ json: { folders: [{ id: 1, type: url.searchParams.get('type'), name: '默认收藏夹', is_default: true, created_at: '', updated_at: '' }] } });
      return true;
    }
    if (url.pathname === '/api/favorites/status') {
      await route.fulfill({ json: { status: { work_folder_ids: [], creator_folder_ids: [] } } });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  for (const size of [
    { width: 1280, height: 720 },
    { width: 754, height: 837 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(size);
    await page.goto('/');
    await page.getByLabel('作品播放器').hover();
    await expect(page.getByLabel('视频控制')).toBeVisible();
    await page.getByLabel('更多播放控制').click();
    const pipButton = page.getByLabel('画中画');
    await expect(pipButton).toBeVisible();
    const boxes = await page.evaluate(() => {
      const button = document.querySelector('[aria-label="画中画"]')!.getBoundingClientRect();
      return {
        viewportWidth: window.innerWidth,
        scrollWidth: document.documentElement.scrollWidth,
        button: { left: button.left, right: button.right, width: button.width, height: button.height },
      };
    });
    expect(boxes.scrollWidth).toBeLessThanOrEqual(boxes.viewportWidth + 1);
    expect(boxes.button.left).toBeGreaterThanOrEqual(0);
    expect(boxes.button.right).toBeLessThanOrEqual(boxes.viewportWidth);
    expect(boxes.button.width).toBeGreaterThanOrEqual(38);
    expect(boxes.button.height).toBeGreaterThanOrEqual(38);
  }

  await page.getByLabel('画中画').click();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('pip-requested'))).toBe('yes');
  await expect(page.getByLabel('退出画中画')).toHaveClass(/active-toggle/);
  await page.getByLabel('退出画中画').click();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('pip-exited'))).toBe('yes');
  await expect(page.getByLabel('画中画')).toBeVisible();
  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: '目录配置' })).toBeVisible();
  expect(feedRequests).toBeLessThanOrEqual(3);
  expect(pageErrors).toEqual([]);
  expect(consoleErrors).toEqual([]);
});

test('mobile settings page keeps scan controls inside the viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/directories') {
      await route.fulfill({
        json: {
          directories: [
            {
              id: 1,
              path: '/vol1/1000/SynoDisks/disk_for_yule5/hermesclaw_data/B站',
              name: 'B站',
              status: 'idle',
              creator_count: 200,
              work_count: 17592,
              created_at: '',
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/scan/status') {
      await route.fulfill({
        json: {
          status: {
            running: false,
            queue_length: 0,
            current_directory: '',
            current_creator: '',
            scanned_creators: 0,
            total_creators: 0,
            scanned_works: 0,
            progress: 0,
            state: 'idle',
            errors: [],
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/fs/roots') {
      await route.fulfill({ json: { roots: [{ path: '/vol1/1000', name: '存储空间1' }] } });
      return true;
    }
    return false;
  });

  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: '目录配置' })).toBeVisible();

  const boxes = await page.evaluate(() => {
    const box = (selector: string) => {
      const rect = document.querySelector(selector)!.getBoundingClientRect();
      return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, width: rect.width, height: rect.height };
    };
    return {
      viewportWidth: window.innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      menu: box('.mobile-nav-toggle'),
      heading: box('.page-header h1'),
      pathForm: box('.path-form'),
      directoryRow: box('.directory-row'),
      scanButton: box('.directory-row button:not(.danger)'),
      deleteButton: box('.directory-row .danger'),
      progressPanel: box('.progress-panel'),
    };
  });

  expect(boxes.scrollWidth).toBeLessThanOrEqual(boxes.viewportWidth + 1);
  expect(boxes.menu.right).toBeLessThanOrEqual(boxes.heading.left - 8);
  for (const target of [boxes.pathForm, boxes.directoryRow, boxes.scanButton, boxes.deleteButton, boxes.progressPanel]) {
    expect(target.left).toBeGreaterThanOrEqual(0);
    expect(target.right).toBeLessThanOrEqual(boxes.viewportWidth);
  }
});

test('mobile updates page keeps queue controls and cookie editor inside the viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/update/events') {
      await route.abort();
      return true;
    }
    if (url.pathname === '/api/update/status') {
      await route.fulfill({
        json: {
          status: {
            running: false,
            pending_creators: 0,
            total_creators: 1,
            succeeded_creators: 0,
            failed_creators: 1,
            added_works: 0,
            progress: 100,
            state: 'idle',
            errors: [],
            jobs: [
              {
                id: 1,
                creator_id: 1,
                creator_name: 'B型血文字',
                status: 'failed',
                added_works: 0,
                error: 'open /vol1/1000/SynoDisks/disk_for_yule5/hermesclaw_data/B站/B型血文字/2026-06-03_#卡点舞#覆面_7647065113531180644.mp4.tmp: permission denied',
              },
            ],
          },
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/queues') {
      await route.fulfill({
        json: {
          queues: [
            {
              id: 1,
              name: '默认更新队列',
              cron: '0 3 * * *',
              enabled: true,
              is_default: true,
              next_run_at: '',
              subscribed_count: 1,
              last_run_summary: '暂无上次运行摘要',
              created_at: '',
              updated_at: '',
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/update/cookie') {
      await route.fulfill({ json: { cookie: { configured: true, validation_status: 'valid', validation_message: 'Cookie 可用', updated_at: '', value: 'msToken=very-long-cookie-value; ttwid=another-long-cookie-value; odin_tt=sample;' } } });
      return true;
    }
    if (url.pathname === '/api/creators') {
      await route.fulfill({ json: { creators: [{ id: 1, name: 'B型血文字', work_count: 10, online_identity_status: 'ready' }] } });
      return true;
    }
    return false;
  });

  await page.goto('/updates');
  await expect(page.getByRole('heading', { name: '更新管理' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Cookie 管理' })).toBeVisible();

  const boxes = await page.evaluate(() => {
    const box = (selector: string) => {
      const rect = document.querySelector(selector)!.getBoundingClientRect();
      return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, width: rect.width, height: rect.height };
    };
    return {
      viewportWidth: window.innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      menu: box('.mobile-nav-toggle'),
      heading: box('.page-header h1'),
      updatesGrid: box('.updates-grid'),
      accountCard: box('.account-card'),
      queueRow: box('.queue-row'),
      queueActions: box('.queue-row .row-actions'),
      cookieEditor: box('.cookie-editor'),
      jobRow: box('.job-row'),
    };
  });

  expect(boxes.scrollWidth).toBeLessThanOrEqual(boxes.viewportWidth + 1);
  expect(boxes.menu.right).toBeLessThanOrEqual(boxes.heading.left - 8);
  for (const target of [boxes.updatesGrid, boxes.accountCard, boxes.queueRow, boxes.queueActions, boxes.cookieEditor, boxes.jobRow]) {
    expect(target.left).toBeGreaterThanOrEqual(0);
    expect(target.right).toBeLessThanOrEqual(boxes.viewportWidth);
  }
});

test('media keeps natural ratio across viewport sizes', async ({ page }) => {
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: 'ratio',
              description: 'ratio',
              music_title: '',
              tags: [],
              published_at: '',
              media: [
                {
                  id: 1,
                  work_id: 1,
                  type: 'image',
                  file_name: 'ratio.svg',
                  url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22800%22 height=%22400%22 viewBox=%220 0 800 400%22%3E%3Crect width=%22800%22 height=%22400%22 fill=%22%23f43%22/%3E%3C/svg%3E',
                  ordinal: 0,
                },
              ],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  for (const size of [
    { width: 1280, height: 720 },
    { width: 900, height: 560 },
    { width: 1500, height: 460 },
  ]) {
    await page.setViewportSize(size);
    await page.goto('/');
    const result = await page.locator('.media-content').evaluate((node) => {
      const media = node as HTMLImageElement;
      const mediaBox = media.getBoundingClientRect();
      const stageBox = document.querySelector('.immersive-stage')!.getBoundingClientRect();
      return {
        mediaRatio: mediaBox.width / mediaBox.height,
        naturalRatio: media.naturalWidth / media.naturalHeight,
        inside:
          mediaBox.left >= stageBox.left - 1 &&
          mediaBox.top >= stageBox.top - 1 &&
          mediaBox.right <= stageBox.right + 1 &&
          mediaBox.bottom <= stageBox.bottom + 1,
      };
    });
    expect(result.inside).toBe(true);
    expect(Math.abs(result.mediaRatio - result.naturalRatio)).toBeLessThan(0.02);
  }
});

test('media arrows and fullscreen work reliably', async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.removeItem('localbilibili.feedOrder');
    window.localStorage.removeItem('localbilibili.autoPlayNext');
    Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: function requestFullscreen() {
        window.localStorage.setItem('fullscreen-requested', 'yes');
        return Promise.resolve();
      },
    });
    Object.defineProperty(document, 'exitFullscreen', {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });
  await installApiMock(page, async (url, route) => {
    if (url.pathname === '/api/feed') {
      await route.fulfill({
        json: {
          works: [
            {
              id: 1,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1001',
              title: '第一条',
              description: '第一条描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [
                { id: 1, work_id: 1, type: 'image', file_name: 'a.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23355%22/%3E%3C/svg%3E', ordinal: 0 },
                { id: 2, work_id: 1, type: 'image', file_name: 'b.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23553%22/%3E%3C/svg%3E', ordinal: 1 },
                { id: 3, work_id: 1, type: 'image', file_name: 'c.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23535%22/%3E%3C/svg%3E', ordinal: 2 },
              ],
            },
            {
              id: 2,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1002',
              title: '第二条',
              description: '第二条描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [{ id: 4, work_id: 2, type: 'image', file_name: 'd.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23333%22/%3E%3C/svg%3E', ordinal: 0 }],
            },
            {
              id: 3,
              directory_id: 1,
              creator_id: 1,
              creator_name: '阿言',
              aweme_id: '1003',
              title: '第三条',
              description: '第三条描述',
              music_title: '',
              tags: [],
              published_at: '',
              media: [{ id: 5, work_id: 3, type: 'image', file_name: 'e.svg', url: 'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22400%22 height=%22600%22%3E%3Crect width=%22400%22 height=%22600%22 fill=%22%23666%22/%3E%3C/svg%3E', ordinal: 0 }],
            },
          ],
        },
      });
      return true;
    }
    if (url.pathname === '/api/scan/events') {
      await route.abort();
      return true;
    }
    return false;
  });

  await page.goto('/');
  await page.getByLabel('作品播放器').hover();
  await expect(page.getByText('1/3')).toBeVisible();
  await page.getByTitle('下一张').click();
  await expect(page.getByText('2/3')).toBeVisible();
  await page.getByTitle('下一张').click();
  await expect(page.getByText('3/3')).toBeVisible();

  await page.getByLabel('网页全屏').click();
  await expect(page.locator('.feed-viewer')).toHaveClass(/page-fullscreen/);
  await expect(page.locator('.sidebar')).toBeHidden();
  await page.getByLabel('退出网页全屏').click();
  await expect(page.locator('.feed-viewer')).not.toHaveClass(/page-fullscreen/);

  await page.getByLabel('完整全屏').click();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('fullscreen-requested'))).toBe('yes');
});
