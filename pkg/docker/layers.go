package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Docker 数据目录
const (
	// ImageDB 镜像数据库路径
	ImageDB = `C:\ProgramData\Docker\image\windowsfilter`
	// LayerStore 实际层数据存储路径
	LayerStore = `C:\ProgramData\Docker\windowsfilter`
)

// ImageLayer 表示一个 Docker 镜像层
type ImageLayer struct {
	// DiffID 层的 diff ID（sha256:xxx 格式）
	DiffID string
	// CacheID 层在 windowsfilter 目录中的目录名
	CacheID string
	// Path 层在磁盘上的完整路径
	Path string
	// HasFiles 是否包含 Files 目录（容器文件系统）
	HasFiles bool
	// HasHives 是否包含 Hives 目录（注册表）
	HasHives bool
	// HasUtilityVM 是否包含 UtilityVM（Hyper-V 隔离用）
	HasUtilityVM bool
	// LayerChain 层链（父层列表）
	LayerChain []string
}

// repositories.json 的结构
type repositories struct {
	Repositories map[string]map[string]string `json:"Repositories"`
}

// image config 的结构（只需要 rootfs 部分）
type imageConfig struct {
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

// FindImageLayers 根据镜像名查找其所有层的路径
// 需要管理员权限访问 Docker 数据目录
func FindImageLayers(imageRef string) ([]ImageLayer, error) {
	// 1. 从 repositories.json 获取镜像 ID
	imageID, err := getImageID(imageRef)
	if err != nil {
		return nil, fmt.Errorf("获取镜像 ID 失败: %w", err)
	}

	// 2. 从镜像配置获取层的 diff ID 列表
	diffIDs, err := getImageDiffIDs(imageID)
	if err != nil {
		return nil, fmt.Errorf("获取镜像层 diff ID 失败: %w", err)
	}

	// 3. 通过 diff ID 查找 cache ID，构建层路径
	var layers []ImageLayer
	for _, diffID := range diffIDs {
		layer, err := resolveLayer(diffID)
		if err != nil {
			return nil, fmt.Errorf("解析层 %s 失败: %w", diffID, err)
		}
		layers = append(layers, layer)
	}

	if len(layers) == 0 {
		return nil, fmt.Errorf("镜像 %s 没有找到任何层", imageRef)
	}

	return layers, nil
}

// getImageID 从 repositories.json 获取镜像的 ID
func getImageID(imageRef string) (string, error) {
	repoFile := filepath.Join(ImageDB, "repositories.json")
	data, err := os.ReadFile(repoFile)
	if err != nil {
		return "", fmt.Errorf("读取 repositories.json 失败: %w", err)
	}

	var repos repositories
	if err := json.Unmarshal(data, &repos); err != nil {
		return "", fmt.Errorf("解析 repositories.json 失败: %w", err)
	}

	// 在所有仓库中查找镜像
	for _, tags := range repos.Repositories {
		for tag, id := range tags {
			// 精确匹配 tag（如 "mcr.microsoft.com/windows/nanoserver:ltsc2022"）
			if tag == imageRef {
				return id, nil
			}
		}
	}

	return "", fmt.Errorf("未找到镜像: %s", imageRef)
}

// getImageDiffIDs 从镜像配置中获取层的 diff ID 列表
func getImageDiffIDs(imageID string) ([]string, error) {
	// 镜像 ID 格式: sha256:2f14ee035891...
	// 配置文件路径: imagedb/content/sha256/2f14ee035891...
	hash := strings.TrimPrefix(imageID, "sha256:")
	configPath := filepath.Join(ImageDB, "imagedb", "content", "sha256", hash)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取镜像配置失败: %w", err)
	}

	var config imageConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析镜像配置失败: %w", err)
	}

	if len(config.RootFS.DiffIDs) == 0 {
		return nil, fmt.Errorf("镜像没有层信息")
	}

	return config.RootFS.DiffIDs, nil
}

// resolveLayer 通过 diff ID 查找层的 cache ID 和路径
func resolveLayer(diffID string) (ImageLayer, error) {
	// diff ID 格式: sha256:abc123...
	hash := strings.TrimPrefix(diffID, "sha256:")
	layerDBPath := filepath.Join(ImageDB, "layerdb", "sha256", hash)

	// 读取 cache-id 文件
	cacheIDBytes, err := os.ReadFile(filepath.Join(layerDBPath, "cache-id"))
	if err != nil {
		return ImageLayer{}, fmt.Errorf("读取 cache-id 失败: %w", err)
	}
	cacheID := strings.TrimSpace(string(cacheIDBytes))

	// 构建层的实际路径
	layerPath := filepath.Join(LayerStore, cacheID)

	// 检查层目录是否存在
	if _, err := os.Stat(layerPath); err != nil {
		return ImageLayer{}, fmt.Errorf("层目录不存在: %s", layerPath)
	}

	// 读取 layerchain.json
	var layerChain []string
	chainData, err := os.ReadFile(filepath.Join(layerPath, "layerchain.json"))
	if err == nil {
		// layerchain.json 可能是 null（基础层）或数组
		content := strings.TrimSpace(string(chainData))
		if content != "null" && content != "" {
			_ = json.Unmarshal(chainData, &layerChain)
		}
	}

	// 检查层的结构
	layer := ImageLayer{
		DiffID:     diffID,
		CacheID:    cacheID,
		Path:       layerPath,
		HasFiles:   dirExists(filepath.Join(layerPath, "Files")),
		HasHives:   dirExists(filepath.Join(layerPath, "Hives")),
		HasUtilityVM: dirExists(filepath.Join(layerPath, "UtilityVM")),
		LayerChain: layerChain,
	}

	return layer, nil
}

// dirExists 检查目录是否存在
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
