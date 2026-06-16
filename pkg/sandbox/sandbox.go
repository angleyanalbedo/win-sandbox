package sandbox

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ExecutionResult 进程执行结果
type ExecutionResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Elapsed  time.Duration
}

// Sandbox 沙箱实例
type Sandbox struct {
	id       string
	config   *SandboxConfig
	running  bool
	mu       sync.Mutex
}

// New 创建新的沙箱实例
func New(cfg *SandboxConfig) *Sandbox {
	if cfg.Name == "" {
		cfg.Name = "sandbox-" + uuid.New().String()[:8]
	}
	return &Sandbox{
		id:     cfg.Name,
		config: cfg,
	}
}

// Start 启动沙箱
func (s *Sandbox) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("沙箱已在运行中")
	}

	logrus.WithFields(logrus.Fields{
		"id":   s.id,
		"type": s.config.SandboxType,
	}).Info("正在创建沙箱...")

	switch s.config.SandboxType {
	case SandboxContainer:
		return s.startDockerContainer()
	default:
		return fmt.Errorf("暂不支持 %s 模式，请使用 --sandbox-type container", s.config.SandboxType)
	}
}

// startDockerContainer 使用 Docker 创建容器
func (s *Sandbox) startDockerContainer() error {
	// 检查 Docker 是否可用
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("Docker 不可用: %w", err)
	}

	// 构建 docker create 参数
	args := []string{"create", "--name", s.id}

	// 资源限制
	if s.config.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", s.config.MemoryMB))
	}
	if s.config.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%d", s.config.CPUs))
	}

	// 共享目录
	for _, dir := range s.config.SharedDirs {
		mount := fmt.Sprintf("%s:%s", dir.HostPath, dir.GuestPath)
		if dir.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	// 网络
	if !s.config.EnableNetwork {
		args = append(args, "--network", "none")
	}

	// 使用 nanoserver 作为基础镜像
	args = append(args, "mcr.microsoft.com/windows/nanoserver:ltsc2022")

	// 保持容器运行
	args = append(args, "cmd", "/c", "ping", "-t", "localhost")

	logrus.WithField("args", args).Debug("创建 Docker 容器")

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建容器失败: %s - %w", string(out), err)
	}

	// 启动容器
	out, err = exec.Command("docker", "start", s.id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动容器失败: %s - %w", string(out), err)
	}

	s.running = true
	logrus.Info("沙箱启动成功")
	return nil
}

// Execute 在沙箱中执行命令
func (s *Sandbox) Execute(cmd string, args []string, timeout time.Duration) (*ExecutionResult, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil, fmt.Errorf("沙箱未启动")
	}
	containerID := s.id
	s.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"cmd":  cmd,
		"args": args,
	}).Info("正在执行命令...")

	startTime := time.Now()

	// 构建 docker exec 参数
	execArgs := []string{"exec", containerID}
	execArgs = append(execArgs, cmd)
	execArgs = append(execArgs, args...)

	logrus.WithField("args", execArgs).Debug("执行命令")

	// 执行命令
	var stdout, stderr bytes.Buffer
	execCmd := exec.Command("docker", execArgs...)
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// 设置超时
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() {
			execCmd.Process.Kill()
		})
		defer timer.Stop()
	}

	err := execCmd.Run()
	elapsed := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("执行命令失败: %w", err)
		}
	}

	result := &ExecutionResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Elapsed:  elapsed,
	}

	logrus.WithFields(logrus.Fields{
		"exitCode": exitCode,
		"elapsed":  elapsed,
	}).Info("命令执行完成")

	return result, nil
}

// Terminate 终止并清理沙箱
func (s *Sandbox) Terminate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	logrus.Info("正在终止沙箱...")

	// 停止容器
	exec.Command("docker", "stop", "-t", "0", s.id).Run()

	// 删除容器
	out, err := exec.Command("docker", "rm", "-f", s.id).CombinedOutput()
	if err != nil {
		logrus.WithError(err).WithField("output", string(out)).Warn("删除容器失败")
	}

	s.running = false
	logrus.Info("沙箱已终止")
	return nil
}

// IsRunning 沙箱是否正在运行
func (s *Sandbox) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// ID 返回沙箱 ID
func (s *Sandbox) ID() string {
	return s.id
}

// PrintConfig 打印配置信息
func (s *Sandbox) PrintConfig() error {
	fmt.Printf("沙箱名称:     %s\n", s.id)
	fmt.Printf("沙箱类型:     %s\n", s.config.SandboxType)
	fmt.Printf("CPU 数量:     %d\n", s.config.CPUs)
	fmt.Printf("最大内存:     %d MB\n", s.config.MemoryMB)
	fmt.Printf("网络:         %v\n", s.config.EnableNetwork)
	if len(s.config.SharedDirs) > 0 {
		fmt.Printf("共享目录:\n")
		for _, dir := range s.config.SharedDirs {
			fmt.Printf("  %s -> %s (只读: %v)\n", dir.HostPath, dir.GuestPath, dir.ReadOnly)
		}
	}
	return nil
}

// CheckPrerequisites 检查运行前提条件
func CheckPrerequisites() error {
	// 检查 Docker
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("Docker 不可用，请确保 Docker 已安装并运行")
	}
	return nil
}

// CheckDockerMode 检查 Docker 是否在 Windows 容器模式
func CheckDockerMode() (string, error) {
	out, err := exec.Command("docker", "info", "--format", "{{.OSType}}").Output()
	if err != nil {
		return "", fmt.Errorf("获取 Docker 信息失败: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
