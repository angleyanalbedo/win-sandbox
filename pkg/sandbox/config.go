package sandbox

import "time"

// Config 沙箱配置
type Config struct {
	// ImageRef 容器镜像引用
	ImageRef string
	// MemoryMB 内存限制 (MB)
	MemoryMB int
	// CPUs CPU 核数
	CPUs int
	// Network 网络模式: none, default
	Network string
	// Timeout 命令超时
	Timeout time.Duration
	// WorkDir 工作目录
	WorkDir string
	// Env 环境变量
	Env []string
	// Verbose 详细输出
	Verbose bool
}

// Option 配置选项函数
type Option func(*Config)

// WithImage 设置镜像
func WithImage(image string) Option {
	return func(c *Config) { c.ImageRef = image }
}

// WithMemory 设置内存
func WithMemory(mb int) Option {
	return func(c *Config) { c.MemoryMB = mb }
}

// WithCPUs 设置 CPU
func WithCPUs(cpus int) Option {
	return func(c * Config) { c.CPUs = cpus }
}

// WithNetwork 设置网络
func WithNetwork(network string) Option {
	return func(c *Config) { c.Network = network }
}

// WithTimeout 设置超时
func WithTimeout(d time.Duration) Option {
	return func(c *Config) { c.Timeout = d }
}

// WithWorkDir 设置工作目录
func WithWorkDir(dir string) Option {
	return func(c *Config) { c.WorkDir = dir }
}

// WithEnv 设置环境变量
func WithEnv(env []string) Option {
	return func(c *Config) { c.Env = env }
}

// WithVerbose 设置详细输出
func WithVerbose(v bool) Option {
	return func(c *Config) { c.Verbose = v }
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ImageRef: "mcr.microsoft.com/windows/nanoserver:ltsc2022",
		MemoryMB: 1024,
		CPUs:     2,
		Network:  "none",
		Timeout:  5 * time.Minute,
	}
}
