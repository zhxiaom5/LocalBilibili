# 开源发布审计记录

## 分支与历史边界

- 公开版从干净的已提交基线创建于独立 worktree。
- `open-source` 已重建为单一无父提交，避免把日常仓库历史、提交者邮箱和不可达 Git 对象带入新远程仓库。
- 当前日常开发 worktree 和其未提交内容不属于公开发布范围。

## 排除项

`.gitignore` 排除数据库、配置、环境文件、Cookie、密钥、日志、构建产物、下载媒体、测试输出和会话档案。公开树不保留个人绝对路径、指定创作者链接或原始会话索引。

## 发布前命令

```bash
bash scripts/verify-open-source-release.sh
go test ./...
npm test --prefix web
npm run build --prefix web
git diff --check
git diff --cached --check
```

## 阻断门禁

仓库根目录现已加入项目所有者选择的 MIT `LICENSE`。公开推送前仍必须通过秘密扫描、测试、构建和 staged diff 审查。

## 发布结果

- 公开仓库：`https://github.com/zhxiaom5/LocalBilibili`
- 默认分支：`open-source`
- 发布提交：单一无父提交；提交者身份为 `LocalBilibili contributors <opensource@localbilibili.invalid>`。
- 已通过：`bash scripts/verify-open-source-release.sh`、`npm run build --prefix web`、`go test ./...`（115 项）和 `npm test --prefix web`（97 项）。发布前会在最终提交上再次执行这些命令。
- GitHub 基础设置：公开仓库、MIT 许可证、Issues 启用、Wiki/Projects/Discussions 禁用、合并后删除分支、主题与秘密扫描/推送保护已启用。

## 依赖审计与持续检查

- `react-router-dom` 已从 `6.28.1` 升至兼容现有 SPA API 的 `6.30.4`。
- `npm audit --omit=dev` 仍报告两项中危 React Router 通告；该发布版未启用 SSR、RSC 或服务端 action，相关攻击面不在当前应用运行模型中。升级到 7.18.1 会引入一项高危 RSC 通告，故不采用。
- `.github/dependabot.yml` 为 Go 和 Web 依赖启用每周更新检查；GitHub 的秘密扫描和推送保护已启用。后续应在有不引入高危回归的修复版本后优先升级。
