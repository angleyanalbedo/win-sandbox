package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Sandbox SandboxConfig `yaml:"sandbox"`
	Log     LogConfig     `yaml:"log"`
}

// SandboxConfig 沙箱默认配置
type SandboxConfig struct {
	Image     string        `yaml:"image"`      // 容器镜像
	MemoryMB  int           `yaml:"memory_mb"`  // 内存限制 (MB)
	CPUs      int           `yaml:"cpus"`       // CPU 核数
	Network   string        `yaml:"network"`    // 网络模式: none, default
	Timeout   time.Duration `yaml:"timeout"`    // 命令超时
	WorkDir   string        `yaml:"work_dir"`   // 工作目录
	Env       []string      `yaml:"env"`        // 环境变量
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text, json
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Sandbox: SandboxConfig{
			Image:    "mcr.microsoft.com/windows/nanoserver:ltsc2022",
			MemoryMB: 1024,
			CPUs:     2,
			Network:  "none",
			Timeout:  5 * time.Minute,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// DefaultConfigPath 返回默认配置文件路径
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".win-sandbox", "config.yaml")
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // 文件不存在，返回默认配置
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return cfg, nil
}

// Save 保存配置到文件
func Save(cfg *Config, path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}
