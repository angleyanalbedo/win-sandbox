package layer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// OCI Manifest 结构
type Manifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	MediaType     string           `json:"mediaType"`
	Config        Descriptor       `json:"config"`
	Layers        []Descriptor     `json:"layers"`
	// ManifestList 多平台镜像的 manifest list
	Manifests     []Descriptor     `json:"manifests,omitempty"`
}

// Platform 平台信息
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// ManifestListEntry manifest list 中的条目
type ManifestListEntry struct {
	Descriptor
	Platform Platform `json:"platform"`
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
	client  *http.Client
	tokens  map[string]string // registry -> token 缓存
}

// NewRegistryClient 创建 Registry 客户端
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		client: &http.Client{},
		tokens: make(map[string]string),
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

	// 如果是 manifest list（多平台），需要获取特定平台的 manifest
	if manifest.MediaType == "application/vnd.oci.image.index.v1+json" ||
		manifest.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		// 查找 windows/amd64 平台
		manifest, err = c.resolveManifestList(ctx, registry, repository, manifest)
		if err != nil {
			return nil, fmt.Errorf("解析 manifest list 失败: %w", err)
		}
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

// doRequest 执行 HTTP 请求，自动处理认证
func (c *RegistryClient) doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	// 如果有缓存的 token，先加上
	if token, ok := c.tokens[req.URL.Host]; ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	// 如果返回 401，尝试获取 token 并重试
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()

		token, err := c.fetchToken(ctx, resp, req.URL.String())
		if err != nil {
			return nil, fmt.Errorf("获取认证 token 失败: %w", err)
		}
		c.tokens[req.URL.Host] = token

		// 重试请求
		req.Header.Set("Authorization", "Bearer "+token)
		return c.client.Do(req)
	}

	return resp, nil
}

// fetchToken 从 Www-Authenticate 头获取 token
func (c *RegistryClient) fetchToken(ctx context.Context, resp *http.Response, originalURL string) (string, error) {
	// 解析 Www-Authenticate 头
	// 格式: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/hello-world:pull"
	authHeader := resp.Header.Get("Www-Authenticate")
	if authHeader == "" {
		return "", fmt.Errorf("响应中没有 Www-Authenticate 头")
	}

	realm := ""
	service := ""
	scope := ""

	re := regexp.MustCompile(`realm="([^"]+)"`)
	if matches := re.FindStringSubmatch(authHeader); len(matches) > 1 {
		realm = matches[1]
	}

	re = regexp.MustCompile(`service="([^"]+)"`)
	if matches := re.FindStringSubmatch(authHeader); len(matches) > 1 {
		service = matches[1]
	}

	re = regexp.MustCompile(`scope="([^"]+)"`)
	if matches := re.FindStringSubmatch(authHeader); len(matches) > 1 {
		scope = matches[1]
	}

	if realm == "" {
		return "", fmt.Errorf("无法解析 realm")
	}

	// 构建 token 请求
	tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return "", err
	}

	tokenResp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token 请求失败: HTTP %d", tokenResp.StatusCode)
	}

	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return "", err
	}

	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}

	return token, nil
}

// fetchManifest 获取镜像 manifest
func (c *RegistryClient) fetchManifest(ctx context.Context, registry, repository, tag string) (*Manifest, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json")

	resp, err := c.doRequest(ctx, req)
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

	resp, err := c.doRequest(ctx, req)
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

	resp, err := c.doRequest(ctx, req)
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
	// 先找第一个 / 分离 registry 和 repository
	slashIdx := strings.Index(imageName, "/")

	if slashIdx == -1 {
		// 没有 /，是 Docker Hub 官方镜像（可能带 tag）
		name, t := splitTag(imageName)
		registry = "registry-1.docker.io"
		repository = "library/" + name
		tag = t
		return
	}

	potentialRegistry := imageName[:slashIdx]
	rest := imageName[slashIdx+1:]

	// 判断 potentialRegistry 是否是 registry 地址
	if isRegistry(potentialRegistry) {
		registry = potentialRegistry
		repository, tag = splitTag(rest)
	} else {
		// 不是 registry，整体是 Docker Hub 的 repository
		registry = "registry-1.docker.io"
		repository, tag = splitTag(imageName)
	}

	return
}

// splitTag 分离镜像名和 tag
// nanoserver:ltsc2022 → nanoserver, ltsc2022
// nanoserver → nanoserver, latest
func splitTag(name string) (string, string) {
	// 从右边找 :（避免误匹配 host:port 中的冒号）
	// 但 tag 的 : 后面不应该有 /
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == ':' {
			potentialTag := name[i+1:]
			if !strings.Contains(potentialTag, "/") {
				return name[:i], potentialTag
			}
		}
	}
	return name, "latest"
}

// isRegistry 判断是否是 registry 地址（包含 . 或 : 或是 localhost）
func isRegistry(s string) bool {
	return strings.Contains(s, ".") || strings.Contains(s, ":") || s == "localhost"
}
