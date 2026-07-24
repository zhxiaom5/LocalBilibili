# 部署指南

## 本地二进制部署

1. 安装 Go 1.24+、Node.js 20+ 和 npm。
2. 复制 `.env.example` 为仅本机可读的 `.env`，把数据库路径改到仓库外的持久化目录，并为首次创建数据库设置唯一用户名和高强度 `LOCALBILIBILI_BOOTSTRAP_PASSWORD`。
3. 执行 `npm ci --prefix web && npm run build --prefix web`。
4. 执行 `go build -o bin/localbilibili ./cmd/server`。
5. 加载 `.env` 后运行 `./bin/localbilibili`。

不要把数据库、配置、Cookie、下载媒体或 `.env` 放入源码目录的 Git 追踪范围。

## 反向代理与网络边界

默认监听地址为 `:8080`。仅在可信局域网使用，或在反向代理前配置 HTTPS、访问控制和防火墙规则。不要把服务或 SQLite 文件直接暴露到公网。

## fnOS 打包

fnOS 模板位于 `packaging/fnos/localbilibili/`。构建命令为：

```bash
bash scripts/build-fnos-fpk.sh
```

该过程会生成 `dist/` 下的 FPK，`dist/` 已被忽略。构建前应确认第三方运行时、`yt-dlp` 的许可与分发条件，并在目标设备测试升级和回滚。部署脚本需要本机保存的 fnOS 登录状态；该状态只可保留在忽略目录中。
