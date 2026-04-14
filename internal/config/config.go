package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// RepoEndpoint 表示一个 Git 仓库的地址和分支。
type RepoEndpoint struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
}

// Task 表示一个同步任务：从 Source 推送到多个 Targets。
type Task struct {
	Name    string         `yaml:"name"`
	Source  RepoEndpoint   `yaml:"source"`
	Targets []RepoEndpoint `yaml:"targets"`
	SSHKey  string         `yaml:"ssh_key,omitempty"`
	Force   bool           `yaml:"force,omitempty"`
}

// Config 是配置文件的顶层结构。
type Config struct {
	SSHKey    string `yaml:"ssh_key,omitempty"`
	HTTPProxy string `yaml:"http_proxy,omitempty"`
	NoProxy   string `yaml:"no_proxy,omitempty"`
	Interval  string `yaml:"interval,omitempty"`
	Tasks     []Task `yaml:"tasks"`
}

// LoadConfig 从指定路径加载 YAML 配置。
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ExportTemplate 将示例配置写入指定路径。
func ExportTemplate(path string) error {
	tmpl := Config{
		SSHKey:    "/path/to/ssh/key",
		HTTPProxy: "",
		NoProxy:   "",
		Interval:  "5m",
		Tasks: []Task{
			{
				Name:  "example-repo",
				Force: true,
				Source: RepoEndpoint{
					Repo:   "git@source.com:org/repo.git",
					Branch: "main",
				},
				Targets: []RepoEndpoint{
					{Repo: "git@target1.com:org/repo.git", Branch: "main"},
					{Repo: "git@target2.com:org/repo.git", Branch: "main"},
				},
			},
		},
	}
	data, err := yaml.Marshal(&tmpl)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
