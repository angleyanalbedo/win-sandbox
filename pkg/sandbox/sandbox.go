package sandbox

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/Microsoft/hcsshim"
	"github.com/google/uuid"
)

// ExecutionResult 执行结果
type ExecutionResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Elapsed  time.Duration
}

// Sandbox 沙箱实例
type Sandbox struct {
	id        string
	config    *Config
	layerMgr  *LayerManager
	baseLayer *LayerInfo
	mount     *MountResult
	container hcsshim.Container
	running   bool
}

// New 创建新的沙箱实例
func New(opts ...Option) *Sandbox {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return &Sandbox{
		id:       "sandbox-" + uuid.New().String()[:8],
		config:   cfg,
		layerMgr: NewLayerManager(),
	}
}

// ID 返回沙箱 ID
func (s *Sandbox) ID() string {
	return s.id
}

// Create 创建沙箱（查找层、挂载层、创建容器）
func (s *Sandbox) Create() error {
	// 查找基础层
	baseLayer, err := s.layerMgr.FindBaseLayer(s.config.ImageRef)
	if err != nil {
		return fmt.Errorf("查找基础层失败: %w", err)
	}
	s.baseLayer = baseLayer

	// 挂载层
	mount, err := s.layerMgr.MountLayers(baseLayer)
	if err != nil {
		return fmt.Errorf("挂载层失败: %w", err)
	}
	s.mount = mount

	// 构建 HCS 层
	hcsLayers, err := s.layerMgr.BuildHCSLayers(baseLayer)
	if err != nil {
		s.layerMgr.UnmountLayers(mount)
		return fmt.Errorf("构建 HCS 层失败: %w", err)
	}

	// 构建容器配置
	containerCfg := &hcsshim.ContainerConfig{
		SystemType:      "Container",
		Name:            s.id,
		HvPartition:     false,
		VolumePath:      mount.VolumePath,
		LayerFolderPath: mount.ScratchPath,
		ProcessorCount:  uint32(s.config.CPUs),
		MemoryMaximumInMB: int64(s.config.MemoryMB),
		TerminateOnLastHandleClosed: true,
		Layers: hcsLayers,
	}

	// 网络配置
	if s.config.Network == "none" {
		containerCfg.EndpointList = []string{}
	}

	// 创建容器
	container, err := hcsshim.CreateContainer(s.id, containerCfg)
	if err != nil {
		s.layerMgr.UnmountLayers(mount)
		return fmt.Errorf("创建容器失败: %w", err)
	}
	s.container = container

	return nil
}

// Start 启动沙箱
func (s *Sandbox) Start() error {
	if s.container == nil {
		return fmt.Errorf("沙箱未创建，请先调用 Create()")
	}

	if err := s.container.Start(); err != nil {
		return fmt.Errorf("启动容器失败: %w", err)
	}

	s.running = true
	return nil
}

// Execute 在沙箱中执行命令
func (s *Sandbox) Execute(command string) (*ExecutionResult, error) {
	if !s.running {
		return nil, fmt.Errorf("沙箱未运行")
	}

	startTime := time.Now()

	// 创建进程
	proc, err := s.container.CreateProcess(&hcsshim.ProcessConfig{
		CommandLine:      command,
		CreateStdOutPipe: true,
		CreateStdErrPipe: true,
		WorkingDirectory: s.config.WorkDir,
		Environment:      envToMap(s.config.Env),
	})
	if err != nil {
		return nil, fmt.Errorf("创建进程失败: %w", err)
	}
	defer proc.Close()

	// 读取输出
	_, stdout, stderr, _ := proc.Stdio()
	var outBuf, errBuf bytes.Buffer
	go io.Copy(&outBuf, stdout)
	go io.Copy(&errBuf, stderr)

	// 等待退出
	proc.Wait()
	exitCode, _ := proc.ExitCode()

	return &ExecutionResult{
		ExitCode: exitCode,
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		Elapsed:  time.Since(startTime),
	}, nil
}

// Close 关闭沙箱（清理资源）
func (s *Sandbox) Close() error {
	if s.container != nil {
		s.container.Shutdown()
		s.container.Terminate()
		s.container.Close()
		s.container = nil
	}
	if s.mount != nil {
		s.layerMgr.UnmountLayers(s.mount)
		s.mount = nil
	}
	s.running = false
	return nil
}

// IsRunning 沙箱是否正在运行
func (s *Sandbox) IsRunning() bool {
	return s.running
}

// envToMap 将环境变量切片转换为 map
func envToMap(env []string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	m := make(map[string]string, len(env))
	for _, e := range env {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				m[e[:i]] = e[i+1:]
				break
			}
		}
	}
	return m
}
