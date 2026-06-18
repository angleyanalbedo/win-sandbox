package layer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OCI Manifest 结构
type Manifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	MediaType     string           `json:"mediaType"`
	Config        Descriptor       `json:"config"`
	Layers        []Descriptor     `json:"layers"`
}

// Descriptor 描述一个 blob
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// ImageConfig 镜像配置
type ImageConfig struct {
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

// RegistryClient OCI Registry 客户端
type RegistryClient struct {
	client *http.Client
}

// NewRegistryClient 创建 Registry 客户端
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		client: &http.Client{},
	}
}

// PullResult 拉取结果
type PullResult struct {
	ImageName string
	Layers    []string // DiffID 列表
}

// Pull 从 OCI Registry 拉取镜像
// imageName 示例: mcr.microsoft.com/windows/nanoserver:ltsc2022
func (c *RegistryClient) Pull(ctx context.Context, store *Store, imageName string) (*PullResult, error) {
	// 检查是否已拉取
	if img, ok := store.Index().GetImage(imageName); ok {
		return &PullResult{
			ImageName: imageName,
			Layers:    img.Layers,
		}, nil
	}

	// 解析镜像名
	registry, repository, tag := parseImageName(imageName)

	// 1. 获取 manifest
	manifest, err := c.fetchManifest(ctx, registry, repository, tag)
	if err != nil {
		return nil, fmt.Errorf("获取 manifest 失败: %w", err)
	}

	// 2. 获取镜像配置（获取 diff_id 列表）
	config, err := c.fetchConfig(ctx, registry, repository, manifest.Config.Digest)
	if err != nil {
		return nil, fmt.Errorf("获取镜像配置失败: %w", err)
	}

	diffIDs := config.RootFS.DiffIDs
	if len(diffIDs) != len(manifest.Layers) {
		return nil, fmt.Errorf("层数不匹配: manifest=%d, config=%d", len(manifest.Layers), len(diffIDs))
	}

	// 3. 逐层下载并导入
	var parentDiffID string
	for i, layer := range manifest.Layers {
		diffID := diffIDs[i]

		// 跳过已存在的层
		if store.Index().HasLayer(diffID) {
			parentDiffID = diffID
			continue
		}

		// 下载并导入
		fmt.Printf("  拉取层 %d/%d: %s\n", i+1, len(diffIDs), diffID[:19]+"...")

		reader, err := c.fetchBlob(ctx, registry, repository, layer.Digest)
		if err != nil {
			return nil, fmt.Errorf("下载层 %s 失败: %w", diffID, err)
		}

		_, err = store.ImportFromGzipTar(ctx, diffID, parentDiffID, reader)
		reader.Close()
		if err != nil {
			return nil, fmt.Errorf("导入层 %s 失败: %w", diffID, err)
		}

		parentDiffID = diffID
	}

	// 4. 记录镜像
	err = store.Index().AddImage(&ImageInfo{
		Name:     imageName,
		Layers:   diffIDs,
		CreatedAt: timeNow(),
	})
	if err != nil {
		return nil, fmt.Errorf("记录镜像失败: %w", err)
	}

	return &PullResult{
		ImageName: imageName,
		Layers:    diffIDs,
	}, nil
}

// fetchManifest 获取镜像 manifest
func (c *RegistryClient) fetchManifest(ctx context.Context, registry, repository, tag string) (*Manifest, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// fetchConfig 获取镜像配置
func (c *RegistryClient) fetchConfig(ctx context.Context, registry, repository, digest string) (*ImageConfig, error) {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, digest)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	var config ImageConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// fetchBlob 获取 blob（层数据），返回 gzip 压缩的 tar 流
func (c *RegistryClient) fetchBlob(ctx context.Context, registry, repository, digest string) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, digest)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return resp.Body, nil
}

// parseImageName 解析镜像名
// 输入: mcr.microsoft.com/windows/nanoserver:ltsc2022
// 输出: registry=mcr.microsoft.com, repository=windows/nanoserver, tag=ltsc2022
func parseImageName(imageName string) (registry, repository, tag string) {
	// 分离 tag
	parts := strings.SplitN(imageName, ":", 2)
	if len(parts) == 2 {
		tag = parts[1]
	} else {
		tag = "latest"
	}
	name := parts[0]

	// 分离 registry 和 repository
	slashIdx := strings.Index(name, "/")
	if slashIdx == -1 {
		// 没有 /，是 Docker Hub 官方镜像
		registry = "registry-1.docker.io"
		repository = "library/" + name
		return
	}

	potentialRegistry := name[:slashIdx]
	if strings.Contains(potentialRegistry, ".") || strings.Contains(potentialRegistry, ":") || potentialRegistry == "localhost" {
		// 有 . 或 : 或是 localhost，是 registry 地址
		registry = potentialRegistry
		repository = name[slashIdx+1:]
	} else {
		// 没有 .，是 Docker Hub 用户镜像
		registry = "registry-1.docker.io"
		repository = name
	}

	return
}
