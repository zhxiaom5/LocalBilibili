# LocalBilibili

局域网版本地B站作品浏览 Web 应用。

## 运行

LocalBilibili 日常运行是一个单体 Go 程序：同一个进程同时提供 Web 页面、API、SQLite 和本地媒体访问。

```bash
npm install --prefix web
npm run build --prefix web
go build -o bin/localbilibili ./cmd/server
LOCALBILIBILI_DB=localbilibili.db ./bin/localbilibili
```

默认地址：

- Web/API: http://localhost:8080
- 数据库: `localbilibili.db`

可通过环境变量调整：

```bash
LOCALBILIBILI_ADDR=:8080 \
LOCALBILIBILI_DB=localbilibili.db \
./bin/localbilibili
```

`LOCALBILIBILI_WEB_DIR` 仅用于开发调试时覆盖内嵌前端资源。正常运行不需要设置它。

不要把长期使用的 `LOCALBILIBILI_DB` 指向 `/tmp` 目录，否则重启或切换启动方式后可能看不到之前扫描的数据。

## 开发

```bash
npm run build --prefix web
go test ./...
npm test --prefix web
npm run build --prefix web
npm run test:e2e --prefix web
```

前端开发服务器：

```bash
npm run dev --prefix web
```

前端开发服务器只用于前端开发调试；完整应用验收以单体 Go 服务为准。

## 会话归档

- [Thread 019f2661-85cf-7053-9de1-5a02f24f00f6](docs/threads/019f2661-85cf-7053-9de1-5a02f24f00f6/README.md)：LocalBilibili 初始规划、AGENTS.md 设计、项目骨架和首轮迁移开发。
