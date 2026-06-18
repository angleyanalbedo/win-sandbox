package layer

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Microsoft/hcsshim/pkg/ociwclayer"
)

// Store 层存储管理器
type Store struct {
	dir   string
	index *IndexManager
}

// NewStore 创建层存储
// baseDir 示例: C:\Users\<user>\.win-sandbox\layers
func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}

	index, err := NewIndexManager(baseDir)
	if err != nil {
		return nil, fmt.Errorf("创建索引管理器失败: %w", err)
	}

	return &Store{
		dir:   baseDir,
		index: index,
	}, nil
}

// Index 返回索引管理器
func (s *Store) Index() *IndexManager {
	return s.index
}

// ImportFromTar 从 tar 流导入层
// diffID: 层的内容哈希（sha256:xxx）
// parentDiffID: 父层的 DiffID（基础层为空字符串）
// r: tar 流（已解压 gzip）
func (s *Store) ImportFromTar(ctx context.Context, diffID string, parentDiffID string, r io.Reader) (*LayerInfo, error) {
	// 检查是否已存在
	if s.index.HasLayer(diffID) {
		layer, _ := s.index.GetLayer(diffID)
		return layer, nil
	}

	// 构建层目录路径
	dirName := DiffIDToDirName(diffID)
	layerPath := filepath.Join(s.dir, dirName)

	// 构建父层路径列表
	var parentPaths []string
	if parentDiffID != "" {
		parent, ok := s.index.GetLayer(parentDiffID)
		if !ok {
			return nil, fmt.Errorf("父层 %s 不存在，请先导入父层", parentDiffID)
		}
		parentPaths = []string{parent.Path}
	}

	// 调用 hcsshim 导入
	size, err := ociwclayer.ImportLayerFromTar(ctx, r, layerPath, parentPaths)
	if err != nil {
		// 清理失败的导入
		os.RemoveAll(layerPath)
		return nil, fmt.Errorf("导入层失败: %w", err)
	}

	// 记录到索引
	info := &LayerInfo{
		DiffID:    diffID,
		Path:      layerPath,
		Parent:    parentDiffID,
		Size:      size,
		CreatedAt: timeNow(),
	}
	if err := s.index.AddLayer(info); err != nil {
		return nil, fmt.Errorf("更新索引失败: %w", err)
	}

	return info, nil
}

// ImportFromGzipTar 从 gzip 压缩的 tar 流导入层
func (s *Store) ImportFromGzipTar(ctx context.Context, diffID string, parentDiffID string, r io.Reader) (*LayerInfo, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("创建 gzip reader 失败: %w", err)
	}
	defer gz.Close()

	return s.ImportFromTar(ctx, diffID, parentDiffID, gz)
}

// ImportFromFile 从 tar 文件导入层
func (s *Store) ImportFromFile(ctx context.Context, diffID string, parentDiffID string, tarPath string) (*LayerInfo, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	// 检测是否 gzip 压缩
	buf := make([]byte, 2)
	n, _ := f.Read(buf)
	f.Seek(0, 0)

	if n == 2 && buf[0] == 0x1f && buf[1] == 0x8b {
		// gzip 格式
		return s.ImportFromGzipTar(ctx, diffID, parentDiffID, f)
	}

	// 普通 tar
	return s.ImportFromTar(ctx, diffID, parentDiffID, f)
}

// ExportToTar 将层导出为 tar 流
func (s *Store) ExportToTar(ctx context.Context, diffID string, w io.Writer) error {
	layer, ok := s.index.GetLayer(diffID)
	if !ok {
		return fmt.Errorf("层 %s 不存在", diffID)
	}

	// 获取父层路径
	parentPaths, err := s.index.GetLayerChain(diffID)
	if err != nil {
		return err
	}
	// 去掉当前层自身，只保留父层
	if len(parentPaths) > 0 {
		parentPaths = parentPaths[:len(parentPaths)-1]
	}

	return ociwclayer.ExportLayerToTar(ctx, w, layer.Path, parentPaths)
}

// GetLayerPath 获取层的磁盘路径
func (s *Store) GetLayerPath(diffID string) (string, error) {
	layer, ok := s.index.GetLayer(diffID)
	if !ok {
		return "", fmt.Errorf("层 %s 不存在", diffID)
	}
	return layer.Path, nil
}

// GetParentPaths 获取层的父层路径列表（用于 PrepareLayer 等）
func (s *Store) GetParentPaths(diffID string) ([]string, error) {
	chain, err := s.index.GetLayerChain(diffID)
	if err != nil {
		return nil, err
	}
	// 去掉最后一层（自身），只保留父层
	if len(chain) > 0 {
		return chain[:len(chain)-1], nil
	}
	return nil, nil
}

// RemoveLayer 删除层（从磁盘和索引中移除）
func (s *Store) RemoveLayer(diffID string) error {
	layer, ok := s.index.GetLayer(diffID)
	if !ok {
		return nil // 不存在视为成功
	}

	// 检查是否有子层依赖
	for _, other := range s.index.ListLayers() {
		if other.Parent == diffID {
			return fmt.Errorf("层 %s 被层 %s 依赖，无法删除", diffID, other.DiffID)
		}
	}

	// 从磁盘删除
	if err := os.RemoveAll(layer.Path); err != nil {
		return fmt.Errorf("删除层目录失败: %w", err)
	}

	// 从索引删除
	return s.index.RemoveLayer(diffID)
}

// ImportImage 导入完整镜像（所有层 + 镜像记录）
func (s *Store) ImportImage(ctx context.Context, name string, layers []TarLayer) error {
	// layers 按从底到顶顺序排列
	for _, l := range layers {
		if _, err := s.ImportFromGzipTar(ctx, l.DiffID, l.ParentDiffID, l.Reader); err != nil {
			return fmt.Errorf("导入层 %s 失败: %w", l.DiffID, err)
		}
	}

	// 记录镜像
	diffIDs := make([]string, len(layers))
	for i, l := range layers {
		diffIDs[i] = l.DiffID
	}

	return s.index.AddImage(&ImageInfo{
		Name:     name,
		Layers:   diffIDs,
		CreatedAt: timeNow(),
	})
}

// TarLayer 表示一个待导入的 tar 层
type TarLayer struct {
	DiffID      string
	ParentDiffID string
	Reader       io.Reader
}

func timeNow() time.Time {
	return time.Now()
}
