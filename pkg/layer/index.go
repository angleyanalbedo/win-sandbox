package layer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LayerInfo 层信息
type LayerInfo struct {
	// DiffID 层的内容哈希（sha256:xxx）
	DiffID string `json:"diff_id"`
	// Path 层在磁盘上的路径
	Path string `json:"path"`
	// Parent 父层的 DiffID（基础层为空）
	Parent string `json:"parent,omitempty"`
	// Size 层的大小（字节）
	Size int64 `json:"size"`
	// CreatedAt 导入时间
	CreatedAt time.Time `json:"created_at"`
}

// ImageInfo 镜像信息
type ImageInfo struct {
	// Name 镜像名（如 nanoserver:ltsc2022）
	Name string `json:"name"`
	// Layers 层的 DiffID 列表（从底到顶）
	Layers []string `json:"layers"`
	// ConfigDigest 配置文件的 digest
	ConfigDigest string `json:"config_digest"`
	// CreatedAt 拉取时间
	CreatedAt time.Time `json:"created_at"`
}

// Index 层索引
type Index struct {
	// Layers 所有已导入的层，key 为 DiffID
	Layers map[string]*LayerInfo `json:"layers"`
	// Images 所有已导入的镜像，key 为镜像名
	Images map[string]*ImageInfo `json:"images"`
}

// IndexManager 索引管理器
type IndexManager struct {
	dir   string
	index *Index
}

// NewIndexManager 创建索引管理器
func NewIndexManager(baseDir string) (*IndexManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	m := &IndexManager{
		dir: baseDir,
	}
	if err := m.load(); err != nil {
		// 索引文件不存在则创建空索引
		m.index = &Index{
			Layers: make(map[string]*LayerInfo),
			Images: make(map[string]*ImageInfo),
		}
	}

	return m, nil
}

// HasLayer 检查层是否已存在
func (m *IndexManager) HasLayer(diffID string) bool {
	_, ok := m.index.Layers[diffID]
	return ok
}

// GetLayer 获取层信息
func (m *IndexManager) GetLayer(diffID string) (*LayerInfo, bool) {
	layer, ok := m.index.Layers[diffID]
	return layer, ok
}

// AddLayer 添加层到索引
func (m *IndexManager) AddLayer(info *LayerInfo) error {
	if info.DiffID == "" {
		return fmt.Errorf("层的 DiffID 不能为空")
	}
	m.index.Layers[info.DiffID] = info
	return m.save()
}

// RemoveLayer 从索引删除层
func (m *IndexManager) RemoveLayer(diffID string) error {
	delete(m.index.Layers, diffID)
	return m.save()
}

// AddImage 添加镜像到索引
func (m *IndexManager) AddImage(info *ImageInfo) error {
	if info.Name == "" {
		return fmt.Errorf("镜像名不能为空")
	}
	m.index.Images[info.Name] = info
	return m.save()
}

// GetImage 获取镜像信息
func (m *IndexManager) GetImage(name string) (*ImageInfo, bool) {
	img, ok := m.index.Images[name]
	return img, ok
}

// RemoveImage 从索引删除镜像
func (m *IndexManager) RemoveImage(name string) error {
	delete(m.index.Images, name)
	return m.save()
}

// ListImages 列出所有镜像
func (m *IndexManager) ListImages() []*ImageInfo {
	var images []*ImageInfo
	for _, img := range m.index.Images {
		images = append(images, img)
	}
	return images
}

// ListLayers 列出所有层
func (m *IndexManager) ListLayers() []*LayerInfo {
	var layers []*LayerInfo
	for _, layer := range m.index.Layers {
		layers = append(layers, layer)
	}
	return layers
}

// GetLayerChain 获取层的完整父层链（从底到顶）
func (m *IndexManager) GetLayerChain(diffID string) ([]string, error) {
	var chain []string
	current := diffID

	for current != "" {
		layer, ok := m.index.Layers[current]
		if !ok {
			return nil, fmt.Errorf("层 %s 不存在", current)
		}
		chain = append([]string{layer.Path}, chain...) // 前插
		current = layer.Parent
	}

	return chain, nil
}

// GetUnusedLayers 获取未被任何镜像引用的层
func (m *IndexManager) GetUnusedLayers() []*LayerInfo {
	// 收集所有镜像引用的层
	used := make(map[string]bool)
	for _, img := range m.index.Images {
		for _, layerID := range img.Layers {
			used[layerID] = true
		}
	}

	var unused []*LayerInfo
	for diffID, layer := range m.index.Layers {
		if !used[diffID] {
			unused = append(unused, layer)
		}
	}
	return unused
}

// DiffIDToDirName 将 DiffID 转换为目录名
// sha256:abc123... → abc123...
func DiffIDToDirName(diffID string) string {
	if len(diffID) > 7 && diffID[:7] == "sha256:" {
		return diffID[7:]
	}
	return diffID
}

func (m *IndexManager) indexPath() string {
	return filepath.Join(m.dir, "index.json")
}

func (m *IndexManager) load() error {
	data, err := os.ReadFile(m.indexPath())
	if err != nil {
		return err
	}

	m.index = &Index{}
	return json.Unmarshal(data, m.index)
}

func (m *IndexManager) save() error {
	data, err := json.MarshalIndent(m.index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.indexPath(), data, 0644)
}
