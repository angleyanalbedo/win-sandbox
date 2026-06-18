package sandbox

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/Microsoft/hcsshim"
	"github.com/angleyanalbedo/win-sandbox/pkg/state"
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
	store     state.Store
	record    *state.SandboxRecord
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

// NewWithStore 创建带状态存储的沙箱实例
func NewWithStore(store state.Store, opts ...Option) *Sandbox {
	s := New(opts...)
	s.store = store
	return s
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
		SystemType:                  "Container",
		Name:                        s.id,
		HvPartition:                 false,
		VolumePath:                  mount.VolumePath,
		LayerFolderPath:             mount.ScratchPath,
		ProcessorCount:              uint32(s.config.CPUs),
		MemoryMaximumInMB:           int64(s.config.MemoryMB),
		TerminateOnLastHandleClosed: false,
		Layers:                      hcsLayers,
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

	// 持久化状态
	if s.store != nil {
		s.record = &state.SandboxRecord{
			ID:          s.id,
			ContainerID: s.id,
			ImageRef:    s.config.ImageRef,
			Status:      "created",
			CreatedAt:   time.Now(),
			ScratchID:   mount.ScratchID,
			ScratchPath: mount.ScratchPath,
			LayerPath:   baseLayer.Path,
			MemoryMB:    s.config.MemoryMB,
			CPUs:        s.config.CPUs,
			Network:     s.config.Network,
		}
		if err := s.store.Save(s.record); err != nil {
			// 状态保存失败不阻塞创建，但记录警告
			fmt.Printf("警告: 保存沙箱状态失败: %v\n", err)
		}
	}

	return nil
}

// Attach 重新连接到已存在的沙箱
// 通过 HCS API 打开已有的容器句柄
func Attach(id string, store state.Store) (*Sandbox, error) {
	record, err := store.Load(id)
	if err != nil {
		return nil, fmt.Errorf("加载沙箱状态失败: %w", err)
	}

	// 通过 HCS API 重新打开容器
	container, err := hcsshim.OpenContainer(record.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("打开容器 %s 失败: %w", record.ContainerID, err)
	}

	return &Sandbox{
		id: record.ID,
		config: &Config{
			ImageRef: record.ImageRef,
			MemoryMB: record.MemoryMB,
			CPUs:     record.CPUs,
			Network:  record.Network,
		},
		layerMgr:  NewLayerManager(),
		container: container,
		running:   true, // 假设已运行，因为 OpenContainer 成功说明容器存在
		store:     store,
		record:    record,
	}, nil
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

	// 更新状态
	if s.store != nil && s.record != nil {
		s.record.Status = "running"
		_ = s.store.Save(s.record)
	}

	return nil
}

// Execute 在沙箱中执行命令（一次性）
func (s *Sandbox) Execute(command string) (*ExecutionResult, error) {
	if s.container == nil {
		return nil, fmt.Errorf("沙箱未创建或未连接")
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

// Close 关闭沙箱句柄（不销毁容器）
// 对应 runhcs 的 container.Close()
func (s *Sandbox) Close() error {
	if s.container != nil {
		err := s.container.Close()
		s.container = nil
		return err
	}
	return nil
}

// Destroy 销毁沙箱（终止容器、清理层、删除状态）
// 对应 runhcs 的 container.Remove()
func (s *Sandbox) Destroy() error {
	var lastErr error

	// 终止容器
	if s.container != nil {
		s.container.Shutdown()
		s.container.Terminate()
		s.container.Wait()
		s.container.Close()
		s.container = nil
	}

	// 清理层
	if s.mount != nil {
		s.layerMgr.UnmountLayers(s.mount)
		s.mount = nil
	}

	// 删除状态
	if s.store != nil {
		if err := s.store.Delete(s.id); err != nil {
			lastErr = fmt.Errorf("删除沙箱状态失败: %w", err)
		}
	}

	s.running = false
	return lastErr
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
