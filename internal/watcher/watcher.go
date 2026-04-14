package watcher

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"git-repo-sync/internal/auth"
	"git-repo-sync/internal/config"
	"git-repo-sync/internal/sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

const defaultInterval = 5 * time.Minute

func logf(format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("[%s] %s\n", ts, fmt.Sprintf(format, args...))
}

func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

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

	runPollCycle(ctx, cfg, globalAuth, lastHash)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		nextTime := time.Now().Add(interval).Format("2006-01-02 15:04:05")
		logf("下次检查: %s", nextTime)

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

func runPollCycle(ctx context.Context, cfg *config.Config, globalAuth transport.AuthMethod, lastHash map[int]string) {
	logf("轮询检查 (共 %d 个任务)", len(cfg.Tasks))

	for i, task := range cfg.Tasks {
		select {
		case <-ctx.Done():
			return
		default:
		}

		currentHash, err := sync.FetchTaskCommit(task, globalAuth, cfg.HTTPProxy, cfg.NoProxy)
		if err != nil {
			logf("[%d/%d] %s: 获取提交失败: %v", i+1, len(cfg.Tasks), task.Name, err)
			continue
		}

		prev, exists := lastHash[i]
		if exists && prev == currentHash {
			logf("[%d/%d] %s: 暂无变化", i+1, len(cfg.Tasks), task.Name)
			continue
		}

		if exists {
			logf("[%d/%d] %s: 发生变化 (%s.. → %s..)", i+1, len(cfg.Tasks), task.Name, shortHash(prev), shortHash(currentHash))
		} else {
			logf("[%d/%d] %s: 首次同步", i+1, len(cfg.Tasks), task.Name)
		}

		fmt.Printf("[%d/%d] 同步 %s\n", i+1, len(cfg.Tasks), task.Name)
		if err := sync.SyncTask(task, globalAuth, cfg.HTTPProxy, cfg.NoProxy); err != nil {
			logf("[%d/%d] %s: 同步失败: %v", i+1, len(cfg.Tasks), task.Name, err)
			continue
		}
		lastHash[i] = currentHash
	}
}
