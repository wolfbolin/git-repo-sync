# GitRepoSync Daemon Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split GitRepoSync into two binaries (one-shot sync + daemon watcher) with Docker multi-arch support.

**Architecture:** Restructure to Go standard `cmd/` multi-entry layout. Extract shared sync logic into `internal/`, add `watcher` package for polling. Dockerfile builds both binaries, `build.sh` handles buildx multi-arch push.

**Tech Stack:** Go 1.25, go-git v5, Cobra (sync binary), standard `flag` (daemon binary), Docker buildx

**Spec:** `docs/superpowers/specs/2026-04-10-daemon-mode-design.md`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `cmd/git-repo-sync/main.go` | One-shot sync entry point (Cobra: root + sync + template) |
| Create | `cmd/git-repo-sync-daemon/main.go` | Daemon entry point (flag -c, calls watcher.Watch) |
| Modify | `internal/config/config.go` | Add `Interval` field to Config, update template |
| Modify | `internal/sync/sync.go` | Export `FetchRemoteCommit` and `ResolveTaskAuth` |
| Create | `internal/watcher/watcher.go` | Poll loop: ls-remote compare, trigger sync, signal handling |
| Create | `Dockerfile` | Multi-stage build, both binaries, alpine runtime |
| Create | `.dockerignore` | Exclude binaries, config, docs from build context |
| Create | `build.sh` | docker buildx multi-arch build + push script |
| Delete | `main.go` | Old single entry point |
| Delete | `cmd/root.go` | Merged into cmd/git-repo-sync/main.go |
| Delete | `cmd/sync.go` | Merged into cmd/git-repo-sync/main.go |
| Delete | `cmd/template.go` | Merged into cmd/git-repo-sync/main.go |

---

### Task 1: Config — Add `Interval` field

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add Interval field to Config struct**

In `internal/config/config.go`, add `Interval` to `Config`:

```go
type Config struct {
	SSHKey    string `yaml:"ssh_key,omitempty"`
	HTTPProxy string `yaml:"http_proxy,omitempty"`
	NoProxy   string `yaml:"no_proxy,omitempty"`
	Interval  string `yaml:"interval,omitempty"`
	Tasks     []Task `yaml:"tasks"`
}
```

- [ ] **Step 2: Update ExportTemplate to include Interval**

In `ExportTemplate`, add `Interval` to the template struct:

```go
tmpl := Config{
	SSHKey:    "/path/to/ssh/key",
	HTTPProxy: "",
	NoProxy:   "",
	Interval:  "5m",
	Tasks: []Task{
		// ... existing ...
	},
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success, no errors

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add interval field for daemon polling"
```

---

### Task 2: Sync — Export functions for watcher

**Files:**
- Modify: `internal/sync/sync.go`

- [ ] **Step 1: Rename `fetchRemoteCommit` → `FetchRemoteCommit`**

In `internal/sync/sync.go`, rename the function and its doc comment:

```go
// FetchRemoteCommit 通过 ls-remote 获取远程分支的最新 commit ID。
func FetchRemoteCommit(source config.RepoEndpoint, authMethod transport.AuthMethod) (string, error) {
```

Update the call site in `SyncTask` (same file):

```go
commitHash, err := FetchRemoteCommit(task.Source, cloneAuth)
```

- [ ] **Step 2: Rename `resolveTaskAuth` → `ResolveTaskAuth`**

In `internal/sync/sync.go`, rename the function and its doc comment:

```go
// ResolveTaskAuth 根据源仓库协议确定克隆和推送使用的认证方式。
// HTTP(S) 源仓库克隆时无需认证，推送始终使用 SSH 认证。
func ResolveTaskAuth(task config.Task, defaultAuth transport.AuthMethod) (cloneAuth, pushAuth transport.AuthMethod, err error) {
```

Update the call site in `SyncTask` (same file):

```go
cloneAuth, pushAuth, err := ResolveTaskAuth(task, defaultAuth)
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success, no errors

- [ ] **Step 4: Commit**

```bash
git add internal/sync/sync.go
git commit -m "refactor(sync): export FetchRemoteCommit and ResolveTaskAuth"
```

---

### Task 3: Restructure — Migrate sync binary entry point

**Files:**
- Create: `cmd/git-repo-sync/main.go`
- Delete: `main.go`, `cmd/root.go`, `cmd/sync.go`, `cmd/template.go`

- [ ] **Step 1: Create `cmd/git-repo-sync/main.go`**

Merge the content of `main.go`, `cmd/root.go`, `cmd/sync.go`, `cmd/template.go` into a single file. All Cobra commands stay the same, just consolidated:

```go
package main

import (
	"fmt"
	"os"

	"git-repo-sync/internal/auth"
	"git-repo-sync/internal/config"
	"git-repo-sync/internal/sync"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "git-repo-sync",
	Short: "Git 仓库同步工具",
	Long:  "将代码从源仓库同步到多个目标仓库，支持 SSH 和 HTTP(S) 协议。",
}

var configPath string

var syncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "根据配置文件同步 Git 仓库",
	Example:      "  git-repo-sync sync -c config.yaml",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("加载配置失败: %w", err)
		}

		globalAuth, keyPath, err := auth.ResolveAuth(cfg.SSHKey, nil)
		if err != nil {
			return err
		}

		printHeader(len(cfg.Tasks), keyPath, globalAuth, cfg.HTTPProxy)

		var failed int
		for i, task := range cfg.Tasks {
			fmt.Printf("[%d/%d] 同步 %s\n", i+1, len(cfg.Tasks), task.Name)
			if err := sync.SyncTask(task, globalAuth, cfg.HTTPProxy, cfg.NoProxy); err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ 任务失败: %v\n", err)
				failed++
			}
			fmt.Println()
		}

		if failed > 0 {
			return fmt.Errorf("%d/%d 个任务失败", failed, len(cfg.Tasks))
		}
		fmt.Println("✓ 所有任务同步完成")
		return nil
	},
}

var templatePath string

var templateCmd = &cobra.Command{
	Use:          "template",
	Short:        "导出模板配置文件",
	Example:      "  git-repo-sync template -o config.yaml",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.ExportTemplate(templatePath); err != nil {
			return fmt.Errorf("导出模板失败: %w", err)
		}
		fmt.Printf("模板已导出: %s\n", templatePath)
		return nil
	},
}

func printHeader(taskCount int, keyPath string, globalAuth interface{}, httpProxy string) {
	fmt.Printf("共 %d 个同步任务\n", taskCount)
	if keyPath != "" {
		fmt.Printf("SSH 密钥: %s\n", keyPath)
	} else if globalAuth == nil {
		fmt.Println("SSH 密钥: 未配置，尝试使用 SSH Agent")
	}
	if httpProxy != "" {
		fmt.Printf("HTTP 代理: %s\n", httpProxy)
	}
	fmt.Println()
}

func main() {
	syncCmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	syncCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(syncCmd)

	templateCmd.Flags().StringVarP(&templatePath, "output", "o", "", "输出文件路径")
	templateCmd.MarkFlagRequired("output")
	rootCmd.AddCommand(templateCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Delete old entry files**

```bash
rm main.go cmd/root.go cmd/sync.go cmd/template.go
```

Note: `cmd/` directory is kept — it now contains `cmd/git-repo-sync/`.

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/git-repo-sync/`
Expected: success, produces `git-repo-sync` binary

- [ ] **Step 4: Quick smoke test**

```bash
./git-repo-sync --help
./git-repo-sync template -o /tmp/test-template.yaml
cat /tmp/test-template.yaml
```

Expected: help output shows `sync` and `template` subcommands; template file contains `interval` field.

- [ ] **Step 5: Commit**

```bash
git add cmd/git-repo-sync/main.go
git rm main.go cmd/root.go cmd/sync.go cmd/template.go
git commit -m "refactor: migrate sync binary to cmd/git-repo-sync/"
```

---

### Task 4: Watcher — Implement polling loop

**Files:**
- Create: `internal/watcher/watcher.go`

- [ ] **Step 1: Create `internal/watcher/watcher.go`**

Note: spec defines `Watch(cfg, globalAuth)` with two params, but we simplify to `Watch(cfg)` — auth resolution is an internal concern and the daemon entry point stays cleaner.

```go
package watcher

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git-repo-sync/internal/auth"
	"git-repo-sync/internal/config"
	"git-repo-sync/internal/sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

const defaultInterval = 5 * time.Minute

// logf 打印带时间戳的日志。
func logf(format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("[%s] %s\n", ts, fmt.Sprintf(format, args...))
}

// shortHash 安全截取 hash 前 8 位。
func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// parseInterval 解析 interval 字符串为 time.Duration，为空则返回默认值。
func parseInterval(s string) (time.Duration, error) {
	if s == "" {
		return defaultInterval, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("解析 interval 失败: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("interval 必须为正数: %s", s)
	}
	return d, nil
}

// Watch 启动轮询循环，定时检测源仓库变化并触发同步。
func Watch(cfg *config.Config) error {
	interval, err := parseInterval(cfg.Interval)
	if err != nil {
		return err
	}

	globalAuth, keyPath, err := auth.ResolveAuth(cfg.SSHKey, nil)
	if err != nil {
		return err
	}

	logf("启动守护进程 (轮询间隔: %s, 任务数: %d)", interval, len(cfg.Tasks))
	if keyPath != "" {
		logf("SSH 密钥: %s", keyPath)
	}
	if cfg.HTTPProxy != "" {
		logf("HTTP 代理: %s", cfg.HTTPProxy)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lastHash := make(map[int]string)

	// 首次立即执行
	runPollCycle(ctx, cfg, globalAuth, lastHash)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		nextTime := time.Now().Add(interval).Format("2006-01-02 15:04:05")
		logf("下次检查: %s", nextTime)

		// 优先检查退出信号，避免 select 随机选择
		select {
		case <-ctx.Done():
			logf("收到退出信号，守护进程停止")
			return nil
		default:
		}

		select {
		case <-ctx.Done():
			logf("收到退出信号，守护进程停止")
			return nil
		case <-ticker.C:
			runPollCycle(ctx, cfg, globalAuth, lastHash)
		}
	}
}

// runPollCycle 执行一次完整的轮询检查。
func runPollCycle(ctx context.Context, cfg *config.Config, globalAuth transport.AuthMethod, lastHash map[int]string) {
	logf("轮询检查 (共 %d 个任务)", len(cfg.Tasks))

	for i, task := range cfg.Tasks {
		// 检查是否收到退出信号（当前 task 的 SyncTask 会运行完成）
		select {
		case <-ctx.Done():
			return
		default:
		}

		cloneAuth, _, err := sync.ResolveTaskAuth(task, globalAuth)
		if err != nil {
			logf("[%d/%d] %s: 认证解析失败: %v", i+1, len(cfg.Tasks), task.Name, err)
			continue
		}

		currentHash, err := sync.FetchRemoteCommit(task.Source, cloneAuth)
		if err != nil {
			logf("[%d/%d] %s: 获取提交失败: %v", i+1, len(cfg.Tasks), task.Name, err)
			continue
		}

		prev, exists := lastHash[i]
		if exists && prev == currentHash {
			logf("[%d/%d] %s: 无变化，跳过", i+1, len(cfg.Tasks), task.Name)
			continue
		}

		if exists {
			logf("[%d/%d] %s: 检测到变化 (%s.. → %s..)", i+1, len(cfg.Tasks), task.Name, shortHash(prev), shortHash(currentHash))
		} else {
			logf("[%d/%d] %s: 首次同步", i+1, len(cfg.Tasks), task.Name)
		}

		fmt.Printf("[%d/%d] 同步 %s\n", i+1, len(cfg.Tasks), task.Name)
		if err := sync.SyncTask(task, globalAuth, cfg.HTTPProxy, cfg.NoProxy); err != nil {
			logf("[%d/%d] %s: 同步失败: %v", i+1, len(cfg.Tasks), task.Name, err)
			// 不更新 hash，下次重试
			continue
		}
		lastHash[i] = currentHash
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/watcher/watcher.go
git commit -m "feat(watcher): implement polling loop with change detection"
```

---

### Task 5: Daemon binary — Create entry point

**Files:**
- Create: `cmd/git-repo-sync-daemon/main.go`

- [ ] **Step 1: Create `cmd/git-repo-sync-daemon/main.go`**

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"git-repo-sync/internal/config"
	"git-repo-sync/internal/watcher"
)

func main() {
	configPath := flag.String("c", "", "配置文件路径")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: git-repo-sync-daemon -c <config.yaml>\n\n")
		fmt.Fprintf(os.Stderr, "守护进程模式，定时检测源仓库变化并触发同步。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *configPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	if err := watcher.Watch(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "守护进程退出: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify both binaries build**

```bash
go build -o git-repo-sync ./cmd/git-repo-sync/
go build -o git-repo-sync-daemon ./cmd/git-repo-sync-daemon/
```

Expected: both succeed, two binaries produced.

- [ ] **Step 3: Smoke test daemon**

```bash
./git-repo-sync-daemon --help
```

Expected: prints usage with `-c` flag description.

- [ ] **Step 4: Commit**

```bash
git add cmd/git-repo-sync-daemon/main.go
git commit -m "feat: add git-repo-sync-daemon binary entry point"
```

---

### Task 6: Dockerfile + build script

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `build.sh`

- [ ] **Step 1: Create `.dockerignore`**

```
GitRepoSync
git-repo-sync
git-repo-sync-daemon
*.yaml
.claude/
docs/
README.md
```

- [ ] **Step 2: Create `Dockerfile`**

Note: `golang:1.25-alpine` 需确认 Docker Hub 上已发布。若未发布，改用 `golang:1.25.1-alpine` 匹配 go.mod。

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

- [ ] **Step 3: Create `build.sh`**

```bash
#!/bin/bash
set -e

IMAGE="hub.wiolfi.net:23333/wolfbolin/git-repo-sync:20260410"

echo "构建双架构镜像: ${IMAGE}"
echo "平台: linux/amd64, linux/arm64"
echo ""

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --tag "${IMAGE}" \
  --push \
  .

echo ""
echo "✓ 镜像推送完成: ${IMAGE}"
```

- [ ] **Step 4: Make build.sh executable**

```bash
chmod +x build.sh
```

- [ ] **Step 5: Verify Dockerfile builds locally (single arch)**

```bash
docker build -t git-repo-sync-test .
```

Expected: build succeeds.

- [ ] **Step 6: Verify both binaries exist in image**

```bash
docker run --rm git-repo-sync-test git-repo-sync --help
docker run --rm --entrypoint git-repo-sync-daemon git-repo-sync-test --help
```

Expected: both print help output.

- [ ] **Step 7: Commit**

```bash
git add Dockerfile .dockerignore build.sh
git commit -m "feat: add Dockerfile, .dockerignore and multi-arch build script"
```

---

### Task 7: Cleanup — Remove old binary and update .gitignore

**Files:**
- Delete: `GitRepoSync` (old binary in repo root)

- [ ] **Step 1: Remove old binary from repo**

```bash
rm -f GitRepoSync git-repo-sync git-repo-sync-daemon
```

- [ ] **Step 2: Add binaries to .gitignore**

Create or update `.gitignore`:

```
git-repo-sync
git-repo-sync-daemon
```

- [ ] **Step 3: Final full build verification**

```bash
go build -o git-repo-sync ./cmd/git-repo-sync/
go build -o git-repo-sync-daemon ./cmd/git-repo-sync-daemon/
./git-repo-sync --help
./git-repo-sync-daemon --help
```

Expected: both binaries build and show help.

- [ ] **Step 4: Commit**

```bash
git rm -f GitRepoSync 2>/dev/null; true
git add .gitignore
git commit -m "chore: cleanup old binary, add .gitignore"
```
