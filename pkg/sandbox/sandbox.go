package sandbox

import (
	"fmt"
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
	system   HCSHandle
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

	// 检查 HCS API 可用性
	if err := CheckHCSAPI(); err != nil {
		return fmt.Errorf("HCS API 不可用: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"id":   s.id,
		"type": s.config.SandboxType,
	}).Info("正在创建沙箱...")

	// 生成 HCS JSON 配置
	configJSON, err := s.config.ToHCSJSON()
	if err != nil {
		return fmt.Errorf("构建配置失败: %w", err)
	}

	// 输出配置（调试模式）
	logrus.Debugf("HCS 配置:\n%s", configJSON)

	// 创建 compute system
	s.system, err = CreateComputeSystemV2(s.id, configJSON)
	if err != nil {
		return fmt.Errorf("创建 compute system 失败: %w", err)
	}

	logrus.Info("正在启动沙箱...")
	if err := StartComputeSystem(s.system); err != nil {
		TerminateComputeSystem(s.system)
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
	system := s.system
	s.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"cmd":  cmd,
		"args": args,
	}).Info("正在执行命令...")

	startTime := time.Now()

	// 生成进程配置 JSON
	processJSON := buildProcessConfigJSON(cmd, args, "")

	// 创建进程
	process, stdin, stdout, stderr, err := CreateProcessV2(system, processJSON)
	if err != nil {
		return nil, fmt.Errorf("创建进程失败: %w", err)
	}
	defer CloseHandle(process)
	defer CloseHandle(stdin)

	// 异步读取 stdout 和 stderr
	var stdoutBuf, stderrBuf []byte
	var wg sync.WaitGroup
	var stdoutErr, stderrErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutBuf, stdoutErr = ReadPipe(stdout)
		CloseHandle(stdout)
	}()
	go func() {
		defer wg.Done()
		stderrBuf, stderrErr = ReadPipe(stderr)
		CloseHandle(stderr)
	}()

	// 等待进程退出
	exitCode, waitErr := WaitForProcessV2(system, process, timeout)

	wg.Wait()

	elapsed := time.Since(startTime)

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

	err := TerminateComputeSystem(s.system)
	if err != nil {
		logrus.WithError(err).Warn("终止沙箱时出错")
	}

	CloseHandle(s.system)
	s.system = 0
	s.running = false

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
	jsonStr, err := s.config.ToHCSJSON()
	if err != nil {
		return err
	}
	fmt.Println(jsonStr)
	return nil
}

// CheckPrerequisites 检查运行前提条件
func CheckPrerequisites() error {
	if err := CheckHCSAPI(); err != nil {
		return fmt.Errorf("HCS API 不可用: %w", err)
	}
	return nil
}
