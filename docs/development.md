# 开发

构建与检查。代码结构、包的分层与导入规则，以及改动时的同步点与必须守住的不变量，都记在 [CLAUDE.md](../CLAUDE.md)；分层与导入由 `make lint` 检查。

```bash
make ci        # 提交前必须全过，GitHub Actions 在每次推送与 PR 上跑它；查些什么以 Makefile 的 ci 目标为准
make ci-linux  # 推送前在 Docker 里按 GitHub Actions 的环境跑一遍 make ci（linux/amd64，Go 取 go.mod 的版本，Node 取 web/.nvmrc 的）
make test      # Go 测试
make wasm      # 浏览器用的语言服务：web/dist/funroute.wasm
make web       # 前端产物：web/dist/*.js（需要先在 web/ 里 npm install；不提交）
make test-js   # 前端纯逻辑与 wasm 会话测试（node --test）
make site      # 组装发布目录 site/（make run 与 Pages 都用它）
make run       # 启动工作台
go test ./internal/machine -bench . -benchtime 2000x   # VM 基准
```

**推送前先在本地跑一遍线上的 CI**：`make ci` 在这台机器上跑，`make ci-linux` 在 Docker 里按 CI 的机器跑（`tools/ci/Dockerfile`），操作系统或架构带来的差别——比如 arm64 会把 `x * y + z` 融合成 FMA、amd64 不会——在推送前就能看到。检出目录原样挂进容器；按版本固定的检查工具、`web/node_modules` 与 Go 的缓存各放在自己的 Docker 卷里（本机的那些是为本机构建的），第二次起只重跑检查。需要 Docker；Apple 芯片上 amd64 是模拟的，第一次要几分钟。模拟的 CPU 没有 FMA 与 AVX，标准库按 CPU 特性选的路径仍可能与线上机器不同。

`make lint` 强制三条预算：函数不超过 50 行、嵌套不超过 3 层、文件不超过 800 行。
