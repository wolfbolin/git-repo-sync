package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// ResolveAuth 解析 SSH 认证方式。
// 优先级: sshKeyPath > fallback > 自动检测默认密钥。
// 返回 (认证方式, 实际密钥路径, 错误)。
func ResolveAuth(sshKeyPath string, fallback transport.AuthMethod) (transport.AuthMethod, string, error) {
	if sshKeyPath == "" {
		if fallback != nil {
			return fallback, "", nil
		}
		sshKeyPath = getDefaultSSHKey()
		if sshKeyPath == "" {
			return nil, "", nil
		}
	}

	if _, err := os.Stat(sshKeyPath); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("SSH 密钥文件不存在: %s", sshKeyPath)
	}

	key, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return nil, "", fmt.Errorf("读取密钥文件失败: %w", err)
	}

	publicKeys, err := ssh.NewPublicKeys("git", key, "")
	if err != nil {
		return nil, "", fmt.Errorf("解析密钥失败: %w", err)
	}

	return publicKeys, sshKeyPath, nil
}

// getDefaultSSHKey 按优先级搜索本地默认 SSH 密钥。
func getDefaultSSHKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	sshDir := filepath.Join(home, ".ssh")
	candidates := []string{
		filepath.Join(sshDir, "id_ed25519"),
		filepath.Join(sshDir, "id_ecdsa"),
		filepath.Join(sshDir, "id_rsa"),
	}

	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			winDir := filepath.Join(appData, "ssh")
			candidates = append(candidates,
				filepath.Join(winDir, "id_ed25519"),
				filepath.Join(winDir, "id_ecdsa"),
				filepath.Join(winDir, "id_rsa"),
			)
		}
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
