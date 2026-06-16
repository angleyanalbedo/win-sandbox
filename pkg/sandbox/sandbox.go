package sandbox

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Microsoft/hcsshim"
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
	id        string
	config    *SandboxConfig
	container hcsshim.Container
	running   bool
	mu        sync.Mutex
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

	// 构建 HCS 配置
	cfg, err := s.config.ToContainerConfig()
	if err != nil {
		return fmt.Errorf("构建配置失败: %w", err)
	}

	// 创建 compute system
	s.container, err = hcsshim.CreateContainer(s.id, cfg)
	if err != nil {
		return fmt.Errorf("创建 compute system 失败: %w", err)
	}

	logrus.Info("正在启动沙箱...")
	if err := s.container.Start(); err != nil {
		s.container.Terminate()
		return fmt.Errorf("启动沙箱失败: %w", err)
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
	container := s.container
	s.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"cmd":  cmd,
		"args": args,
	}).Info("正在执行命令...")

	startTime := time.Now()

	// 创建进程
	processConfig := buildProcessConfig(cmd, args, "")
	process, err := container.CreateProcess(processConfig)
	if err != nil {
		return nil, fmt.Errorf("创建进程失败: %w", err)
	}
	defer process.Close()

	// 获取 stdin/stdout/stderr 管道
	stdin, stdout, stderr, err := process.Stdio()
	if err != nil {
		return nil, fmt.Errorf("获取标准管道失败: %w", err)
	}
	defer stdin.Close()
	defer stdout.Close()
	defer stderr.Close()

	// 异步读取 stdout 和 stderr
	var stdoutBuf, stderrBuf []byte
	var wg sync.WaitGroup
	var stdoutErr, stderrErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutBuf, stdoutErr = io.ReadAll(stdout)
	}()
	go func() {
		defer wg.Done()
		stderrBuf, stderrErr = io.ReadAll(stderr)
	}()

	// 等待进程退出
	var waitErr error
	if timeout > 0 {
		waitErr = process.WaitTimeout(timeout)
	} else {
		waitErr = process.Wait()
	}

	// 关闭管道以结束读取
	stdin.Close()
	stdout.Close()
	stderr.Close()

	wg.Wait()

	elapsed := time.Since(startTime)

	// 获取退出码
	exitCode := -1
	if exitErr, err := process.ExitCode(); err == nil {
		exitCode = exitErr
	}

	result := &ExecutionResult{
		ExitCode: exitCode,
		Stdout:   string(stdoutBuf),
		Stderr:   string(stderrBuf),
		Elapsed:  elapsed,
	}

	if waitErr != nil {
		if exitCode == -1 {
			return result, fmt.Errorf("进程等待超时或失败: %w", waitErr)
		}
	}

	if stdoutErr != nil {
		logrus.WithError(stdoutErr).Warn("读取 stdout 失败")
	}
	if stderrErr != nil {
		logrus.WithError(stderrErr).Warn("读取 stderr 失败")
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

	err := s.container.Shutdown()
	if err != nil {
		logrus.WithError(err).Warn("优雅关闭失败，强制终止...")
		err = s.container.Terminate()
	} else {
		// 等待关闭完成
		s.container.Wait()
	}

	s.running = false
	s.container.Close()

	logrus.Info("沙箱已终止")
	return err
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

// PrintConfig 打印 HCS JSON 配置（用于调试）
func (s *Sandbox) PrintConfig() error {
	cfg, err := s.config.ToContainerConfig()
	if err != nil {
		return err
	}
	// 简单输出配置信息
	fmt.Printf("沙箱名称: %s\n", cfg.Name)
	fmt.Printf("系统类型: %s\n", cfg.SystemType)
	fmt.Printf("Hyper-V 分区: %v\n", cfg.HvPartition)
	fmt.Printf("CPU 数量: %d\n", cfg.ProcessorCount)
	fmt.Printf("最大内存: %d MB\n", cfg.MemoryMaximumInMB)
	if cfg.HvRuntime != nil {
		fmt.Printf("镜像路径: %s\n", cfg.HvRuntime.ImagePath)
		fmt.Printf("内核文件: %s\n", cfg.HvRuntime.LinuxKernelFile)
	}
	if len(cfg.Layers) > 0 {
		fmt.Printf("镜像层数: %d\n", len(cfg.Layers))
		for i, layer := range cfg.Layers {
			fmt.Printf("  层 %d: %s\n", i, layer.Path)
		}
	}
	if len(cfg.MappedDirectories) > 0 {
		fmt.Printf("共享目录:\n")
		for _, dir := range cfg.MappedDirectories {
			fmt.Printf("  %s -> %s (只读: %v)\n", dir.HostPath, dir.ContainerPath, dir.ReadOnly)
		}
	}
	return nil
}

// CheckPrerequisites 检查运行前提条件
func CheckPrerequisites() error {
	status := CheckComponents()

	if !status.Admin {
		return fmt.Errorf("需要管理员权限运行此程序")
	}
	if !status.VmCompute {
		return fmt.Errorf("vmcompute 服务不可用，请确保已启用容器功能")
	}

	return nil
}
