package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Microsoft/hcsshim"
	"github.com/angleyanalbedo/win-sandbox/pkg/docker"
	"github.com/google/uuid"
)

// LayerInfo 层信息
type LayerInfo struct {
	// DiffID 层的 diff ID
	DiffID string
	// CacheID 层在 windowsfilter 目录中的目录名
	CacheID string
	// Path 层在磁盘上的完整路径
	Path string
	// HasFiles 是否包含 Files 目录
	HasFiles bool
	// HasHives 是否包含 Hives 目录
	HasHives bool
}

// LayerManager 层管理器
type LayerManager struct {
	driverInfo hcsshim.DriverInfo
}

// NewLayerManager 创建层管理器
func NewLayerManager() *LayerManager {
	return &LayerManager{
		driverInfo: hcsshim.DriverInfo{
			Flavour: 1,
			HomeDir: docker.LayerStore,
		},
	}
}

// FindBaseLayer 查找镜像的基础层
func (m *LayerManager) FindBaseLayer(imageRef string) (*LayerInfo, error) {
	layers, err := docker.FindImageLayers(imageRef)
	if err != nil {
		return nil, fmt.Errorf("查找镜像层失败: %w", err)
	}

	if len(layers) == 0 {
		return nil, fmt.Errorf("镜像 %s 没有找到任何层", imageRef)
	}

	// 使用最后一层（基础层）
	base := layers[len(layers)-1]
	return &LayerInfo{
		DiffID:  base.DiffID,
		CacheID: base.CacheID,
		Path:    base.Path,
		HasFiles: base.HasFiles,
		HasHives: base.HasHives,
	}, nil
}

// MountResult 层挂载结果
type MountResult struct {
	// VolumePath volume GUID path
	VolumePath string
	// ScratchPath scratch 层路径
	ScratchPath string
	// ScratchID scratch 层 ID
	ScratchID string
}

// MountLayers 挂载层（创建 scratch、激活、准备、获取 volume path）
func (m *LayerManager) MountLayers(baseLayer *LayerInfo) (*MountResult, error) {
	if !baseLayer.HasFiles {
		return nil, fmt.Errorf("基础层缺少 Files 目录")
	}

	// 创建 scratch 目录
	scratchID := "sandbox-" + uuid.New().String()[:8]
	scratchPath := filepath.Join(docker.LayerStore, scratchID)
	if err := os.MkdirAll(scratchPath, 0755); err != nil {
		return nil, fmt.Errorf("创建 scratch 目录失败: %w", err)
	}

	// 创建 scratch 层
	if err := hcsshim.CreateScratchLayer(m.driverInfo, scratchID, "", []string{baseLayer.Path}); err != nil {
		os.RemoveAll(scratchPath)
		return nil, fmt.Errorf("创建 scratch 层失败: %w", err)
	}

	// 激活 scratch 层
	if err := hcsshim.ActivateLayer(m.driverInfo, scratchID); err != nil {
		os.RemoveAll(scratchPath)
		return nil, fmt.Errorf("激活 scratch 层失败: %w", err)
	}

	// 准备 scratch 层
	if err := hcsshim.PrepareLayer(m.driverInfo, scratchID, []string{baseLayer.Path}); err != nil {
		hcsshim.DeactivateLayer(m.driverInfo, scratchID)
		os.RemoveAll(scratchPath)
		return nil, fmt.Errorf("准备 scratch 层失败: %w", err)
	}

	// 获取 volume path
	volumePath, err := hcsshim.GetLayerMountPath(m.driverInfo, scratchID)
	if err != nil {
		hcsshim.UnprepareLayer(m.driverInfo, scratchID)
		hcsshim.DeactivateLayer(m.driverInfo, scratchID)
		os.RemoveAll(scratchPath)
		return nil, fmt.Errorf("获取挂载路径失败: %w", err)
	}

	return &MountResult{
		VolumePath:  volumePath,
		ScratchPath: scratchPath,
		ScratchID:   scratchID,
	}, nil
}

// UnmountLayers 卸载层
func (m *LayerManager) UnmountLayers(result *MountResult) {
	if result == nil {
		return
	}
	hcsshim.UnprepareLayer(m.driverInfo, result.ScratchID)
	hcsshim.DeactivateLayer(m.driverInfo, result.ScratchID)
	os.RemoveAll(result.ScratchPath)
}

// BuildHCSLayers 构建 HCS Layer 列表
func (m *LayerManager) BuildHCSLayers(baseLayer *LayerInfo) ([]hcsshim.Layer, error) {
	dirName := filepath.Base(baseLayer.Path)
	guid := hcsshim.NewGUID(dirName)
	return []hcsshim.Layer{
		{ID: guid.ToString(), Path: baseLayer.Path},
	}, nil
}
