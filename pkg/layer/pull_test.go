package layer

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var pullFlag = flag.Bool("pull", false, "运行需要网络的集成测试")

func TestParseImageName(t *testing.T) {
	tests := []struct {
		input    string
		registry string
		repo     string
		tag      string
	}{
		{"mcr.microsoft.com/windows/nanoserver:ltsc2022", "mcr.microsoft.com", "windows/nanoserver", "ltsc2022"},
		{"docker.io/library/alpine:latest", "docker.io", "library/alpine", "latest"},
		{"alpine:latest", "registry-1.docker.io", "library/alpine", "latest"},
		{"nanoserver", "registry-1.docker.io", "library/nanoserver", "latest"},
		{"localhost:5000/myimage:v1", "localhost:5000", "myimage", "v1"},
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
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-index")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, err := NewIndexManager(tmpDir)
	if err != nil {
		t.Fatalf("创建索引管理器失败: %v", err)
	}

	layer1 := &LayerInfo{DiffID: "sha256:aaa111", Path: filepath.Join(tmpDir, "aaa111"), Size: 1024}
	if err := mgr.AddLayer(layer1); err != nil {
		t.Fatalf("添加层失败: %v", err)
	}

	if !mgr.HasLayer("sha256:aaa111") {
		t.Error("HasLayer 应该返回 true")
	}
	if mgr.HasLayer("sha256:notexist") {
		t.Error("HasLayer 对不存在的层应该返回 false")
	}

	got, ok := mgr.GetLayer("sha256:aaa111")
	if !ok || got.DiffID != layer1.DiffID {
		t.Error("GetLayer 失败")
	}

	img := &ImageInfo{Name: "nanoserver:ltsc2022", Layers: []string{"sha256:aaa111"}}
	mgr.AddImage(img)
	if _, ok := mgr.GetImage("nanoserver:ltsc2022"); !ok {
		t.Error("GetImage 失败")
	}

	// 测试持久化
	mgr2, _ := NewIndexManager(tmpDir)
	if !mgr2.HasLayer("sha256:aaa111") {
		t.Error("持久化后层丢失")
	}
}

func TestGetLayerChain(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-chain")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, _ := NewIndexManager(tmpDir)
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:base", Path: "/layers/base"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:mid", Path: "/layers/mid", Parent: "sha256:base"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:top", Path: "/layers/top", Parent: "sha256:mid"})

	chain, err := mgr.GetLayerChain("sha256:top")
	if err != nil {
		t.Fatalf("GetLayerChain 失败: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("chain 长度: got %d, want 3", len(chain))
	}
}

func TestGetUnusedLayers(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "win-sandbox-test-unused")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	mgr, _ := NewIndexManager(tmpDir)
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:a", Path: "/a"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:b", Path: "/b", Parent: "sha256:a"})
	mgr.AddLayer(&LayerInfo{DiffID: "sha256:c", Path: "/c"})
	mgr.AddImage(&ImageInfo{Name: "test:latest", Layers: []string{"sha256:a", "sha256:b"}})

	unused := mgr.GetUnusedLayers()
	if len(unused) != 1 || unused[0].DiffID != "sha256:c" {
		t.Errorf("unused 层结果错误: %v", unused)
	}
}

// 集成测试：从 MCR 拉取 Windows 镜像
func TestPullMCRManifest(t *testing.T) {
	if !*pullFlag {
		t.Skip("跳过网络测试，使用 -pull 标志启用")
	}

	client := NewRegistryClient()
	ctx := context.Background()

	// MCR 上的 Windows nanoserver 镜像
	imageName := "mcr.microsoft.com/windows/nanoserver:ltsc2022"
	registry, repo, tag := parseImageName(imageName)
	fmt.Printf("镜像: %s\n", imageName)
	fmt.Printf("Registry: %s, Repo: %s, Tag: %s\n", registry, repo, tag)

	manifest, err := client.fetchManifest(ctx, registry, repo, tag)
	if err != nil {
		t.Fatalf("获取 manifest 失败: %v", err)
	}

	fmt.Printf("MediaType: %s\n", manifest.MediaType)
	fmt.Printf("Layers: %d\n", len(manifest.Layers))
	fmt.Printf("Manifests: %d\n", len(manifest.Manifests))

	if len(manifest.Manifests) > 0 {
		fmt.Println("\n是 manifest list，获取具体 manifest...")
		resolved, err := client.resolveManifestList(ctx, registry, repo, manifest)
		if err != nil {
			t.Fatalf("解析 manifest list 失败: %v", err)
		}
		fmt.Printf("Resolved Layers: %d\n", len(resolved.Layers))
		fmt.Printf("Config: %s\n", resolved.Config.Digest)
	}
}
