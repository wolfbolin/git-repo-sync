package sync

import (
	"fmt"
	"os"
	"strings"
	"time"

	"git-repo-sync/internal/auth"
	"git-repo-sync/internal/config"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/storage/memory"
)

// SyncAll 执行配置中所有任务的全量同步，返回失败任务数。
func SyncAll(cfg *config.Config) (int, error) {
	globalAuth, keyPath, err := auth.ResolveAuth(cfg.SSHKey, nil)
	if err != nil {
		return 0, err
	}

	printHeader(len(cfg.Tasks), keyPath, globalAuth, cfg.HTTPProxy)

	var failed int
	for i, task := range cfg.Tasks {
		fmt.Printf("[%d/%d] 同步 %s\n", i+1, len(cfg.Tasks), task.Name)
		if err := SyncTask(task, globalAuth, cfg.HTTPProxy, cfg.NoProxy); err != nil {
			fmt.Fprintf(os.Stderr, "  任务失败: %v\n", err)
			failed++
		}
		fmt.Println()
	}
	return failed, nil
}

// SyncTask 执行一个同步任务：克隆源仓库，推送到所有目标仓库。
func SyncTask(task config.Task, defaultAuth transport.AuthMethod, httpProxy, noProxy string) error {
	cloneAuth, pushAuth, err := resolveTaskAuth(task, defaultAuth)
	if err != nil {
		return err
	}

	// HTTP(S) 源仓库使用代理（覆盖 fetch + clone）
	var restoreProxy func()
	if isHTTPURL(task.Source.Repo) && httpProxy != "" {
		restoreProxy = setHTTPProxy(httpProxy, noProxy)
	}

	commitHash, err := fetchRemoteCommit(task.Source, cloneAuth)
	if err != nil {
		if restoreProxy != nil {
			restoreProxy()
		}
		fmt.Fprintf(os.Stderr, "  获取提交失败: %v\n", err)
		return err
	}
	fmt.Printf("  编号: %s\n", commitHash)

	fmt.Printf("  克隆: %s (%s) ", task.Source.Repo, task.Source.Branch)
	start := time.Now()
	repo, ref, err := cloneSource(task.Source, cloneAuth)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "失败 (%v)\n", err)
		if restoreProxy != nil {
			restoreProxy()
		}
		return err
	}
	fmt.Printf("完成 (%.1fs)\n", elapsed.Seconds())

	if restoreProxy != nil {
		restoreProxy()
	}

	for i, target := range task.Targets {
		pushToTarget(repo, ref, target, i, pushAuth, task.Force)
	}
	return nil
}

// FetchTaskCommit 解析任务认证和代理，获取远程分支最新 commit ID。
// 供 watcher 变化检测使用。
func FetchTaskCommit(task config.Task, defaultAuth transport.AuthMethod, httpProxy, noProxy string) (string, error) {
	cloneAuth, _, err := resolveTaskAuth(task, defaultAuth)
	if err != nil {
		return "", err
	}

	if isHTTPURL(task.Source.Repo) && httpProxy != "" {
		restore := setHTTPProxy(httpProxy, noProxy)
		defer restore()
	}

	return fetchRemoteCommit(task.Source, cloneAuth)
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

// resolveTaskAuth 根据源仓库协议确定克隆和推送使用的认证方式。
func resolveTaskAuth(task config.Task, defaultAuth transport.AuthMethod) (cloneAuth, pushAuth transport.AuthMethod, err error) {
	pushAuth, _, err = auth.ResolveAuth(task.SSHKey, defaultAuth)
	if err != nil {
		return nil, nil, err
	}

	if isHTTPURL(task.Source.Repo) {
		return nil, pushAuth, nil
	}
	return pushAuth, pushAuth, nil
}

func cloneSource(source config.RepoEndpoint, authMethod transport.AuthMethod) (*git.Repository, *plumbing.Reference, error) {
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:           source.Repo,
		Auth:          authMethod,
		ReferenceName: plumbing.NewBranchReferenceName(source.Branch),
		SingleBranch:  true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("克隆源仓库失败: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, nil, fmt.Errorf("获取 HEAD 引用失败: %w", err)
	}
	return repo, ref, nil
}

func pushToTarget(repo *git.Repository, ref *plumbing.Reference, target config.RepoEndpoint, index int, authMethod transport.AuthMethod, force bool) {
	remoteName := fmt.Sprintf("target_%d", index)

	if r, _ := repo.Remote(remoteName); r != nil {
		_ = repo.DeleteRemote(remoteName)
	}

	if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: remoteName,
		URLs: []string{target.Repo},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "  推送: %s (%s) 创建失败 (创建远程失败: %v)\n", target.Repo, target.Branch, err)
		return
	}

	refSpec := gitconfig.RefSpec(ref.Name().String() + ":" + plumbing.NewBranchReferenceName(target.Branch).String())

	fmt.Printf("  推送: %s (%s) ", target.Repo, target.Branch)

	start := time.Now()
	err := repo.Push(&git.PushOptions{
		RemoteName: remoteName,
		Auth:       authMethod,
		RefSpecs:   []gitconfig.RefSpec{refSpec},
		Force:      force,
	})
	elapsed := time.Since(start)

	switch {
	case err == nil:
		fmt.Printf("完成 (%.1fs)\n", elapsed.Seconds())
	case err == git.NoErrAlreadyUpToDate:
		fmt.Printf("跳过 (%.1fs)\n", elapsed.Seconds())
	default:
		fmt.Fprintf(os.Stderr, "更新失败 (%v)\n", err)
	}
}

func fetchRemoteCommit(source config.RepoEndpoint, authMethod transport.AuthMethod) (string, error) {
	ep, err := transport.NewEndpoint(source.Repo)
	if err != nil {
		return "", fmt.Errorf("解析仓库地址失败: %w", err)
	}
	cli, err := gitclient.NewClient(ep)
	if err != nil {
		return "", fmt.Errorf("创建客户端失败: %w", err)
	}
	sess, err := cli.NewUploadPackSession(ep, authMethod)
	if err != nil {
		return "", fmt.Errorf("连接远程仓库失败: %w", err)
	}
	defer sess.Close()

	refs, err := sess.AdvertisedReferences()
	if err != nil {
		return "", fmt.Errorf("获取远程引用失败: %w", err)
	}
	allRefs, err := refs.AllReferences()
	if err != nil {
		return "", fmt.Errorf("解析引用失败: %w", err)
	}
	branchRef := plumbing.NewBranchReferenceName(source.Branch)
	if ref, ok := allRefs[branchRef]; ok {
		return ref.Hash().String(), nil
	}
	return "", fmt.Errorf("分支 %s 不存在", source.Branch)
}

func setHTTPProxy(httpProxy, noProxy string) func() {
	origHTTP := os.Getenv("HTTP_PROXY")
	origHTTPS := os.Getenv("HTTPS_PROXY")
	origNoProxy := os.Getenv("NO_PROXY")

	os.Setenv("HTTP_PROXY", httpProxy)
	os.Setenv("HTTPS_PROXY", httpProxy)
	if noProxy != "" {
		os.Setenv("NO_PROXY", noProxy)
	}

	return func() {
		os.Setenv("HTTP_PROXY", origHTTP)
		os.Setenv("HTTPS_PROXY", origHTTPS)
		os.Setenv("NO_PROXY", origNoProxy)
	}
}

func isHTTPURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}