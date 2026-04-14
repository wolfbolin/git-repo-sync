# GitRepoSync

将代码从源 Git 仓库同步到多个目标仓库，支持跨平台（GitHub、GitCode、Gitee 等）镜像。

## 功能

- 从一个源仓库同步到多个目标仓库
- 支持 SSH 和 HTTP(S) 协议的源仓库
- SSH 密钥自动检测（`id_ed25519` → `id_ecdsa` → `id_rsa`）
- 支持任务级别的独立 SSH 密钥配置
- 支持强制推送（覆盖目标分支历史）
- 推送失败自动跳过，不影响其他目标
- HTTP(S) 代理支持（仅用于 HTTP 协议的源仓库）
- 守护进程模式：定时轮询检测源仓库变化，自动触发同步
- Docker 容器部署，支持 x86_64 / aarch64 双架构

## 安装

### 从源码构建

```bash
git clone https://github.com/wolfbolin/git-repo-sync.git
cd git-repo-sync

# 一次性同步工具
go build -o git-repo-sync ./cmd/git-repo-sync/

# 守护进程
go build -o git-repo-sync-daemon ./cmd/git-repo-sync-daemon/

# 容器镜像
podman build --tag git-repo-sync:latest .
```

## 使用

### 生成配置模板

```bash
git-repo-sync template -o config.yaml
```

### 一次性同步

```bash
git-repo-sync sync -c config.yaml
```

### 守护进程模式

持续运行，按配置的 `interval` 间隔轮询源仓库，检测到变化时自动触发同步：

```bash
git-repo-sync-daemon -c config.yaml
```

### Docker 运行

```bash
docker run -d \
  -v ./config.yaml:/etc/git-repo-sync/config.yaml:ro \
  -v ~/.ssh:/root/.ssh:ro \
  --name git-repo-sync \
  git-repo-sync:latest
```

容器内也可执行一次性同步：

```bash
docker exec git-repo-sync git-repo-sync sync -c /etc/git-repo-sync/config.yaml
```

### 输出示例

#### 一次性同步

```
共 3 个同步任务
SSH 密钥: /root/.ssh/id_rsa

[1/3] 同步 my-project
  编号: 3ba138085f9a2b4c6d7e8f9a0b1c2d3e4f5a6b7c
  克隆: git@gitcode.com:org/repo.git (master) 完成 (1.2s)
  推送: git@github.com:org/repo.git (master) 完成 (0.8s)
  推送: git@gitee.com:org/repo.git (master) 跳过 (0.3s)

[2/3] 同步 my-lib
  编号: fd4229f1a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5
  克隆: https://gitcode.com/org/lib.git (main) 完成 (0.9s)
  推送: git@github.com:org/lib.git (main) 完成 (0.6s)

[3/3] 同步 my-tool
  获取提交失败: 克隆源仓库失败: repository not found

1/3 个任务失败
```

#### 守护进程模式

```
[2026-04-10 15:30:00] 启动守护进程 (轮询间隔: 5m0s, 任务数: 3)
[2026-04-10 15:30:00] SSH 密钥: /root/.ssh/id_rsa
[2026-04-10 15:30:00] 轮询检查 (共 3 个任务)
[2026-04-10 15:30:00] [1/3] my-project: 首次同步
[1/3] 同步 my-project
  编号: 3ba138085f9a2b4c6d7e8f9a0b1c2d3e4f5a6b7c
  克隆: git@gitcode.com:org/repo.git (master) 完成 (1.2s)
  推送: git@github.com:org/repo.git (master) 完成 (0.8s)
[2026-04-10 15:30:02] [2/3] my-lib: 首次同步
...
[2026-04-10 15:30:05] 下次检查: 2026-04-10 15:35:05
[2026-04-10 15:35:05] 轮询检查 (共 3 个任务)
[2026-04-10 15:35:05] [1/3] my-project: 暂无变化
[2026-04-10 15:35:06] [2/3] my-lib: 发生变化 (fd4229f1.. → a1b2c3d4..)
[2/3] 同步 my-lib
  ...
```

## 配置文件

```yaml
# 全局 SSH 密钥路径（可选，留空则自动检测）
ssh_key: ""

# HTTP 代理（可选，仅用于 HTTP(S) 协议的源仓库，SSH 不使用）
http_proxy: ""
# 不走代理的域名列表（逗号分隔）
no_proxy: ""

# 轮询间隔（仅守护进程模式使用，一次性同步忽略此字段）
interval: 5m

tasks:
  - name: my-project           # 任务名称
    force: true                 # 是否强制推送（可选，默认 false）
    # ssh_key: /path/to/key    # 任务级 SSH 密钥（可选，覆盖全局配置）
    source:
      repo: git@gitcode.com:org/repo.git   # 源仓库（支持 SSH 和 HTTP(S)）
      branch: master
    targets:
      - repo: git@github.com:org/repo.git  # 目标仓库（SSH）
        branch: master
      - repo: git@gitee.com:org/repo.git
        branch: master
```

### 配置说明

| 字段 | 必填 | 说明 |
|------|------|------|
| `ssh_key` | 否 | 全局 SSH 密钥路径，留空自动检测 |
| `http_proxy` | 否 | HTTP 代理地址，仅 HTTP(S) 源仓库使用 |
| `no_proxy` | 否 | 不走代理的域名，逗号分隔 |
| `interval` | 否 | 守护进程轮询间隔，如 `5m`、`30s`，默认 `5m` |
| `tasks[].name` | 是 | 任务名称，用于日志输出 |
| `tasks[].source.repo` | 是 | 源仓库地址，支持 `git@` 和 `https://` |
| `tasks[].source.branch` | 是 | 源分支名 |
| `tasks[].targets[].repo` | 是 | 目标仓库地址 |
| `tasks[].targets[].branch` | 是 | 目标分支名 |
| `tasks[].force` | 否 | 强制推送，覆盖目标分支历史 |
| `tasks[].ssh_key` | 否 | 任务级 SSH 密钥，覆盖全局配置 |

### SSH 密钥优先级

1. 任务级 `ssh_key` 配置
2. 全局 `ssh_key` 配置
3. 自动检测 `~/.ssh/id_ed25519` → `id_ecdsa` → `id_rsa`
4. 回退到 SSH Agent

### SSH 端口注意事项

使用非标准 SSH 端口时，必须使用 `ssh://` 协议格式：

```yaml
# 正确 — 连接 2222 端口
repo: ssh://git@host.com:2222/org/repo.git

# 错误 — 2222 会被解析为路径，实际连接 22 端口
repo: git@host.com:2222/org/repo.git
```

## 项目结构

```
├── cmd/
│   ├── git-repo-sync/
│   │   └── main.go                # 一次性同步入口
│   └── git-repo-sync-daemon/
│       └── main.go                # 守护进程入口
├── internal/
│   ├── auth/auth.go               # SSH 认证解析
│   ├── config/config.go           # 配置加载与模板导出
│   ├── sync/sync.go               # 核心同步逻辑
│   └── watcher/watcher.go         # 轮询检测与调度
├── Dockerfile                      # 多阶段构建
├── build.sh                        # 双架构镜像构建脚本
└── config.yaml                     # 配置文件
```

## 许可证

MIT
