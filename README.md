# LocalBilibili

LocalBilibili 是一个局域网内运行的本地 Bilibili 作品浏览器。它以一个 Go 单体程序提供 React Web 界面、SQLite 数据库、API 和本地媒体访问，支持长视频、短视频和图文作品。

## 许可证

本项目采用 [MIT License](LICENSE)。

## 最小运行

前提：Go 1.24+、Node.js 20+、npm。

```bash
cp .env.example .env
# Edit .env and set LOCALBILIBILI_BOOTSTRAP_PASSWORD before the first startup.
npm ci --prefix web
npm run build --prefix web
go build -o bin/localbilibili ./cmd/server
set -a; . ./.env; set +a
./bin/localbilibili
```

打开 `http://localhost:8080`。首次启动会使用 `.env` 中的 `LOCALBILIBILI_BOOTSTRAP_USERNAME` 和 `LOCALBILIBILI_BOOTSTRAP_PASSWORD` 创建超级管理员；请随后修改密码，并把数据库和媒体目录放在仅自己可访问的持久化目录中。

## 文档

- [用户指南](docs/user-guide.md)
- [部署指南](docs/deployment.md)
- [安全说明](SECURITY.md)
- [贡献指南](CONTRIBUTING.md)
- [开源发布审计记录](docs/open-source-release.md)

## 公开目录结构

```text
cmd/server/                  Go 程序入口
internal/server/             API、SQLite、扫描、下载与认证逻辑
web/                         React/Vite 前端和浏览器测试
tools/                       可选的样例与字幕命令行工具
scripts/                     本地测试、打包和部署辅助脚本
packaging/fnos/localbilibili fnOS 打包模板（不含生成物）
docs/                        用户、部署、安全和测试文档
```

本仓库不包含数据库、Cookie、登录会话、下载媒体、日志、构建产物、私有配置或个人会话档案。

## 开发与验证

```bash
npm run build --prefix web
go test ./...
npm test --prefix web
bash scripts/verify-open-source-release.sh
```

真实浏览器验收和需要 Cookie 的下载验收见 [docs/real-user-e2e.md](docs/real-user-e2e.md)。不要把 Cookie 写入 `.env.example`、文档、日志或 Git。
