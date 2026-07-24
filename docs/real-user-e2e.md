# 真实用户 E2E 验收

本验收覆盖真实用户路径：从 0 添加 B站 UP主、在线更新下载、自动入库、Feed 播放、收藏夹、多用户隔离、更新队列、移动端布局、诊断和日志安全。

## 运行方式

先构建前端：

```bash
rtk npm run build --prefix web
```

无 Cookie 时运行本地样例兜底验收：

```bash
rtk node scripts/real-user-e2e.mjs
```

完整真实链路验收需要把 B站 Cookie 放在环境变量里，不要写入仓库、文档或截图：

```bash
BILIBILI_COOKIE_FROM_USER='<paste-cookie-here>' \
LOCALBILIBILI_REAL_E2E_REQUIRE_COOKIE=1 \
rtk node scripts/real-user-e2e.mjs
```

默认测试目标：

- 服务地址：`http://127.0.0.1:18188`
- 真实 UP主：由 `LOCALBILIBILI_REAL_E2E_CREATOR_URL` 显式提供；公开仓库不内置个人或特定创作者账号。
- 数据库目录：`/tmp/localbilibili-real-user-test`
- 下载目录：`/tmp/localbilibili-real-user-media`
- 下载器：PATH 中可执行的 `yt-dlp`，或通过 `LOCALBILIBILI_YTDLP` 指定。
- 真实更新最多作品数：`3`

## 可选环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `LOCALBILIBILI_REAL_E2E_BASE_URL` | `http://127.0.0.1:18188` | 使用已有服务时的访问地址 |
| `LOCALBILIBILI_REAL_E2E_USE_EXISTING_SERVER` | `0` | 设为 `1` 时不启动新服务，直接打当前服务 |
| `LOCALBILIBILI_REAL_E2E_CREATOR_URL` | 用户1主页 | 替换真实 UP主 |
| `LOCALBILIBILI_REAL_E2E_DB_DIR` | `/tmp/localbilibili-real-user-test` | 干净测试库目录 |
| `LOCALBILIBILI_REAL_E2E_MEDIA_DIR` | `/tmp/localbilibili-real-user-media` | 真实下载保存目录 |
| `LOCALBILIBILI_REAL_E2E_KEEP_DATA` | `0` | 设为 `1` 时不清理测试库和下载目录 |
| `LOCALBILIBILI_REAL_E2E_HEADLESS` | `1` | 设为 `0` 时显示浏览器 |
| `LOCALBILIBILI_YTDLP_MAX_WORKS` | `3` | 控制真实 UP主最多下载作品数 |
| `LOCALBILIBILI_YTDLP_DOWNLOAD_SECTIONS` | 空 | 可传 yt-dlp section，仅下载视频片段 |

## 验收内容

脚本会自动断言：

- `ted/ted` 登录成功。
- 保存 Bilibili Cookie 后状态为已配置；Cookie 校验失败不直接终止，但会进入后续真实更新断言。
- 批量新增关注能解析真实 UP主、保留非法链接失败行、重复导入不重复建 UP主。
- 立即更新任务有明确状态；若 B站反爬、403、412、429 或网络错误导致失败，记录为外部依赖失败，并继续执行本地样例兜底验收。
- 扫描 `samples/bilibili` 后 Feed 覆盖 `video_long`、`video_short`、`image_text`。
- 视频媒体 Range 返回 `206`。
- Feed 搜索、排序接口可用。
- 默认和自定义作品收藏夹可收藏、取消、去重。
- 默认和自定义作者收藏夹可收藏、取消、去重。
- 多用户收藏隔离，普通用户不能访问更新管理和批量新增关注 API。
- 定时更新队列可创建、添加 UP主、编辑和禁用。
- 首页、UP主详情、作品收藏夹、作者收藏夹、更新管理、设置页可在真实浏览器打开。
- 移动端 `/`、`/creators`、`/updates`、`/favorite-works`、`/favorite-creators`、`/settings` 无横向溢出。
- 诊断和日志接口可用。
- 审计日志、系统日志、下载源数据和规范 metadata 不包含 Cookie 片段。

## 输出

报告和截图写入 `output/`，该目录已被 `.gitignore` 忽略：

- `output/real-user-e2e/latest.json`
- `output/playwright/real-user-feed-desktop.png`
- `output/playwright/real-user-mobile.png`

报告只记录场景状态和非敏感摘要，不记录 Cookie 明文。
