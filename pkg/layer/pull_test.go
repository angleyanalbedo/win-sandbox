package layer

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseImageName(t *testing.T) {
	tests := []struct {
		input    string
		registry string
		repo     string
		tag      string
	}{
		{
			input:    "mcr.microsoft.com/windows/nanoserver:ltsc2022",
			registry: "mcr.microsoft.com",
			repo:     "windows/nanoserver",
			tag:      "ltsc2022",
		},
		{
			input:    "docker.io/library/alpine:latest",
			registry: "docker.io",
			repo:     "library/alpine",
			tag:      "latest",
		},
		{
			input:    "alpine:latest",
			registry: "registry-1.docker.io",
			repo:     "library/alpine",
			tag:      "latest",
		},
		{
			input:    "nanoserver",
			registry: "registry-1.docker.io",
			repo:     "library/nanoserver",
			tag:      "latest",
		},
		{
			input:    "localhost:5000/myimage:v1",
			registry: "localhost:5000",
			repo:     "myimage",
			tag:      "v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			registry, repo, tag := parseImageName(tt.input)
			if registry != tt.registry {
				t.Errorf("registry: got %q, want %q", registry, tt.registry)
			}
			if repo != tt.repo {
				t.Errorf("repo: got %q, want %q", repo, tt.repo)
			}
			if tag != tt.tag {
				t.Errorf("tag: got %q, want %q", tag, tt.tag)
			}
		})
	}
}

func TestIndexManager(t *testing.T) {
	// 创建临时目录
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-index")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, err := NewIndexManager(tmpDir)
	if err != nil {
		t.Fatalf("创建索引管理器失败: %v", err)
	}

	// 测试添加层
	layer1 := &LayerInfo{
		DiffID: "sha256:aaa111",
		Path:   filepath.Join(tmpDir, "aaa111"),
		Parent: "",
		Size:   1024,
	}
	if err := mgr.AddLayer(layer1); err != nil {
		t.Fatalf("添加层失败: %v", err)
	}

	// 测试重复检查
	if !mgr.HasLayer("sha256:aaa111") {
		t.Error("HasLayer 应该返回 true")
	}
	if mgr.HasLayer("sha256:notexist") {
		t.Error("HasLayer 对不存在的层应该返回 false")
	}

	// 测试获取层
	got, ok := mgr.GetLayer("sha256:aaa111")
	if !ok {
		t.Fatal("GetLayer 应该成功")
	}
	if got.DiffID != layer1.DiffID {
		t.Errorf("DiffID: got %q, want %q", got.DiffID, layer1.DiffID)
	}

	// 测试添加镜像
	img := &ImageInfo{
		Name:   "nanoserver:ltsc2022",
		Layers: []string{"sha256:aaa111"},
	}
	if err := mgr.AddImage(img); err != nil {
		t.Fatalf("添加镜像失败: %v", err)
	}

	// 测试获取镜像
	gotImg, ok := mgr.GetImage("nanoserver:ltsc2022")
	if !ok {
		t.Fatal("GetImage 应该成功")
	}
	if gotImg.Name != img.Name {
		t.Errorf("Name: got %q, want %q", gotImg.Name, img.Name)
	}

	// 测试持久化（重新加载）
	mgr2, err := NewIndexManager(tmpDir)
	if err != nil {
		t.Fatalf("重新创建索引管理器失败: %v", err)
	}
	if !mgr2.HasLayer("sha256:aaa111") {
		t.Error("重新加载后层应该存在")
	}
	if _, ok := mgr2.GetImage("nanoserver:ltsc2022"); !ok {
		t.Error("重新加载后镜像应该存在")
	}

	// 测试删除
	if err := mgr.RemoveImage("nanoserver:ltsc2022"); err != nil {
		t.Fatalf("删除镜像失败: %v", err)
	}
	if _, ok := mgr.GetImage("nanoserver:ltsc2022"); ok {
		t.Error("删除后镜像不应该存在")
	}
}

func TestGetLayerChain(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-chain")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, _ := NewIndexManager(tmpDir)

	// 创建 3 层的链: base -> mid -> top
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:base", Path: "/layers/base", Parent: ""})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:mid", Path: "/layers/mid", Parent: "sha256:base"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:top", Path: "/layers/top", Parent: "sha256:mid"})

	chain, err := mgr.GetLayerChain("sha256:top")
	if err != nil {
		t.Fatalf("GetLayerChain 失败: %v", err)
	}

	expected := []string{"/layers/base", "/layers/mid", "/layers/top"}
	if len(chain) != len(expected) {
		t.Fatalf("chain 长度: got %d, want %d", len(chain), len(expected))
	}
	for i, p := range chain {
		if p != expected[i] {
			t.Errorf("chain[%d]: got %q, want %q", i, p, expected[i])
		}
	}
}

func TestGetUnusedLayers(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-unused")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, _ := NewIndexManager(tmpDir)

	// 添加 3 层
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:a", Path: "/a", Parent: ""})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:b", Path: "/b", Parent: "sha256:a"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:c", Path: "/c", Parent: ""})

	// 只有镜像引用了 a 和 b
	mgr.AddImage(&ImageInfo{Name: "test:latest", Layers: []string{"sha256:a", "sha256:b"}})

	unused := mgr.GetUnusedLayers()
	if len(unused) != 1 {
		t.Fatalf("unused 层数: got %d, want 1", len(unused))
	}
	if unused[0].DiffID != "sha256:c" {
		t.Errorf("unused 层: got %q, want sha256:c", unused[0].DiffID)
	}
}

func TestRegistryClientParseImageName(t *testing.T) {
	// 测试 MCR 镜像
	registry, repo, tag := parseImageName("mcr.microsoft.com/windows/nanoserver:ltsc2022")
	fmt.Printf("MCR: registry=%s, repo=%s, tag=%s\n", registry, repo, tag)

	// 测试 Docker Hub 镜像
	registry, repo, tag = parseImageName("library/alpine:latest")
	fmt.Printf("Docker Hub: registry=%s, repo=%s, tag=%s\n", registry, repo, tag)

	// 测试简写
	registry, repo, tag = parseImageName("alpine")
	fmt.Printf("Short: registry=%s, repo=%s, tag=%s\n", registry, repo, tag)
}

// 集成测试：实际从 Registry 拉取镜像
// 需要网络连接，设置 -pull 标志运行: go test -v -run TestPull -pull
var pullFlag = flag.Bool("pull", false, "运行需要网络的集成测试")

func TestPullManifest(t *testing.T) {
	if !*pullFlag {
		t.Skip("跳过网络测试，使用 -pull 标志启用")
	}

	client := NewRegistryClient()
	ctx := context.Background()

	// 测试获取公开镜像的 manifest（使用 Docker Hub 的 hello-world，体积小）
	registry, repo, tag := parseImageName("library/hello-world:latest")
	fmt.Printf("正在获取 manifest: %s/%s:%s\n", registry, repo, tag)

	manifest, err := client.fetchManifest(ctx, registry, repo, tag)
	if err != nil {
		t.Fatalf("获取 manifest 失败: %v", err)
	}

	fmt.Printf("Manifest 版本: %d\n", manifest.SchemaVersion)
	fmt.Printf("MediaType: %s\n", manifest.MediaType)
	fmt.Printf("Config digest: %s\n", manifest.Config.Digest)
	fmt.Printf("层数: %d\n", len(manifest.Layers))

	for i, layer := range manifest.Layers {
		fmt.Printf("  层 %d: %s (size: %d)\n", i, layer.Digest[:20]+"...", layer.Size)
	}
}

func TestPullImage(t *testing.T) {
	if !*pullFlag {
		t.Skip("跳过网络测试，使用 -pull 标志启用")
	}

	// 创建临时存储目录
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-pull")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}

	client := NewRegistryClient()
	ctx := context.Background()

	// 拉取一个小镜像用于测试
	// 注意: Windows 容器镜像较大，测试时可能需要较长时间
	imageName := "mcr.microsoft.com/windows/nanoserver:ltsc2022"
	fmt.Printf("正在拉取镜像: %s\n", imageName)
	fmt.Println("这可能需要几分钟...")

	result, err := client.Pull(ctx, store, imageName)
	if err != nil {
		t.Fatalf("拉取镜像失败: %v", err)
	}

	fmt.Printf("拉取完成!\n")
	fmt.Printf("镜像名: %s\n", result.ImageName)
	fmt.Printf("层数: %d\n", len(result.Layers))

	// 验证层是否正确导入
	for i, diffID := range result.Layers {
		layer, ok := store.Index().GetLayer(diffID)
		if !ok {
			t.Errorf("层 %s 未在索引中找到", diffID)
			continue
		}
		fmt.Printf("  层 %d: %s (path: %s, size: %d)\n", i, diffID[:20]+"...", layer.Path, layer.Size)

		// 检查层目录是否存在
		if _, err := os.Stat(layer.Path); os.IsNotExist(err) {
			t.Errorf("层目录不存在: %s", layer.Path)
		}
	}

	// 验证镜像记录
	img, ok := store.Index().GetImage(imageName)
	if !ok {
		t.Fatal("镜像记录不存在")
	}
	fmt.Printf("镜像记录: %s, %d 层\n", img.Name, len(img.Layers))

	// 测试重复拉取（应该跳过已存在的层）
	fmt.Println("\n再次拉取（测试去重）...")
	result2, err := client.Pull(ctx, store, imageName)
	if err != nil {
		t.Fatalf("重复拉取失败: %v", err)
	}
	if result2.ImageName != imageName {
		t.Errorf("镜像名不匹配: got %s, want %s", result2.ImageName, imageName)
	}
}
