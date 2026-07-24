# Contributing

## Before contributing

本项目采用根目录的 MIT License。提交前请确认你有权按该许可证贡献代码。

保持改动聚焦；不得提交生成文件、数据库、媒体、凭据、日志或个人路径，并遵循 `AGENTS.md` 的产品与测试约束。

## Local checks

```bash
npm run build --prefix web
go test ./...
npm test --prefix web
bash scripts/verify-open-source-release.sh
```

## Pull requests

Describe the user-visible behavior, tests run, and any migration or privacy impact. Do not include secrets in issue bodies, comments, screenshots, recordings, or commits.
