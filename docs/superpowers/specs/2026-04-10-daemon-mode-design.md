# GitRepoSync Daemon Mode Design

## Overview

将 GitRepoSync 拆分为两个二进制：
- **git-repo-sync**：一次性同步，行为不变
- **git-repo-sync-daemon**：守护进程，定时轮询源仓库变化并触发同步

## Directory Structure

```
├── cmd/
│   ├── git-repo-sync/
│   │   └── main.go             # 一次性同步入口（sync, template 命令）
│   └── git-repo-sync-daemon/
│       └── main.go             # 守护进程入口
├── internal/
│   ├── auth/auth.go            # SSH 认证（不变）
│   ├── config/config.go        # 配置结构（增加 interval）
│   ├── sync/sync.go            # 核心同步逻辑
│   └── watcher/watcher.go      # 轮询检测与调度（新增）
├── Dockerfile
├── build.sh                    # 双架构镜像构建脚本
├── go.mod
└── config.yaml
```

### git-repo-sync 入口迁移

现有 `cmd/root.go`、`cmd/sync.go`、`cmd/template.go` 移入 `cmd/git-repo-sync/main.go`，保留 Cobra 命令结构（root + sync/template 子命令），原有 `cmd/` 包和顶层 `main.go` 删除。

### git-repo-sync-daemon 入口

`cmd/git-repo-sync-daemon/main.go` 使用标准库 `flag` 解析 `-c` 参数（单命令，无需 Cobra），调用 `watcher.Watch()`。

## Config Changes

`config.yaml` 顶层增加 `interval` 字段：

```yaml
ssh_key: ""
http_proxy: ""
no_proxy: ""
interval: 5m

tasks:
  - name: yuanrong
    ...
```

`config.Config` struct 增加：

```go
Interval string `yaml:"interval,omitempty"` // e.g. "5m", "30s"
```

`interval` 仅 daemon 使用，sync 二进制忽略该字段。

## Sync Package Changes

需要导出两个函数供 watcher 包调用：
- `fetchRemoteCommit` → `FetchRemoteCommit(source config.RepoEndpoint, authMethod transport.AuthMethod) (string, error)`
- `resolveTaskAuth` → `ResolveTaskAuth(task config.Task, defaultAuth transport.AuthMethod) (cloneAuth, pushAuth transport.AuthMethod, err error)`

其余逻辑不变。

## Watcher Package (`internal/watcher/watcher.go`)

### Core Loop

```
Watch(cfg *config.Config, globalAuth transport.AuthMethod) error

1. 解析 cfg.Interval 为 time.Duration（默认 5m）
2. 初始化 lastCommitHash map[int]string（key = task 索引，避免 task name 重复问题）
3. 注册 SIGINT/SIGTERM 信号处理，创建 context.Context 用于取消
4. 主循环（使用 time.Ticker 保持固定间隔）:
   a. 打印轮询开始信息（带时间戳）
   b. 遍历所有 task:
      - 调用 sync.ResolveTaskAuth 解析该 task 的认证方式
      - 调用 sync.FetchRemoteCommit(task.Source, cloneAuth) 获取当前 commit hash
      - 与 lastCommitHash[i] 对比
      - 若不同或首次运行 → 调用 sync.SyncTask 执行同步，更新 hash
      - 若相同 → 打印 "无变化，跳过"
   c. 打印下次检查时间
   d. select { case <-ticker.C: continue; case <-ctx.Done(): return }
5. 收到信号后，等待当前正在执行的同步完成，然后退出
```

### Log Format

所有输出加时间戳前缀 `[2006-01-02 15:04:05]`：

```
[2026-04-10 15:30:00] 轮询检查 (共 7 个任务)
[2026-04-10 15:30:00] [1/7] yuanrong: 无变化，跳过
[2026-04-10 15:30:01] [2/7] yuanrong-datasystem: 检测到变化 (abc123.. → def456..)
[2026-04-10 15:30:01]   克隆: git@gitcode.com:... (master) 完成✓ (1.2s)
[2026-04-10 15:30:02]   推送: git@github.com:... (master) 完成✓ (0.8s)
[2026-04-10 15:30:05] 下次检查: 2026-04-10 15:35:05
```

## Daemon Binary Behavior

- `git-repo-sync-daemon -c config.yaml`
- 启动时立即执行一次全量同步（首次 lastCommitHash 为空）
- 之后按 interval 固定间隔轮询（使用 time.Ticker）
- SIGINT/SIGTERM 优雅退出：等待当前同步完成后退出

## Sync Binary

行为完全不变。`interval` 字段被忽略。

## Dockerfile

多阶段构建，alpine 运行镜像。同时打包 `git-repo-sync` 和 `git-repo-sync-daemon` 两个二进制。go-git 使用纯 Go SSH 实现，运行时不需要 openssh-client：

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /git-repo-sync ./cmd/git-repo-sync
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /git-repo-sync-daemon ./cmd/git-repo-sync-daemon

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /git-repo-sync /usr/local/bin/
COPY --from=builder /git-repo-sync-daemon /usr/local/bin/
ENTRYPOINT ["git-repo-sync-daemon"]
CMD ["-c", "/etc/git-repo-sync/config.yaml"]
```

使用 `TARGETOS`/`TARGETARCH` 是 Docker BuildKit 的内置 ARG，`docker buildx` 多平台构建时自动注入。

### 双架构构建脚本 (`build.sh`)

```bash
#!/bin/bash
IMAGE="hub.wiolfi.net:23333/wolfbolin/git-repo-sync:20260410"

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --tag "$IMAGE" \
  --push \
  .
```

- 使用 `docker buildx` 一次构建 amd64 (x86_64) 和 arm64 (aarch64) 双架构
- 构建完成后直接推送到 `hub.wiolfi.net:23333/wolfbolin/git-repo-sync:20260410`
- 需要提前 `docker login hub.wiolfi.net:23333` 并确保 buildx builder 已创建（`docker buildx create --use`）

### 运行

```bash
docker run -d \
  -v ./config.yaml:/etc/git-repo-sync/config.yaml:ro \
  -v ~/.ssh:/root/.ssh:ro \
  --name git-repo-sync \
  hub.wiolfi.net:23333/wolfbolin/git-repo-sync:20260410
```

容器内也可直接执行一次性同步：
```bash
docker exec git-repo-sync git-repo-sync sync -c /etc/git-repo-sync/config.yaml
```

注意：需挂载 `~/.ssh` 目录，其中应包含 SSH 私钥和 `known_hosts` 文件。go-git 默认校验 host key，若 `known_hosts` 不包含目标主机会连接失败。可提前用 `ssh-keyscan` 生成。

## Error Handling

- 单个 task 的 ls-remote 或同步失败不影响其他 task
- 失败的 task 不更新 lastCommitHash，下次轮询会重试
- 网络断开等临时错误在下次轮询自动恢复

## Not In Scope

- Web UI / API
- Webhook 触发模式
- 持久化 lastCommitHash 到磁盘（进程重启后首次全量同步即可）
- 多分支监控（一个 task 对应一个 source branch，多分支需配置多个 task）
