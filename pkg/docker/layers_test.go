package docker

import (
	"testing"
)

// 测试获取镜像 ID
func TestGetImageID(t *testing.T) {
	// 测试存在的镜像
	imageID, err := getImageID("mcr.microsoft.com/windows/nanoserver:ltsc2022")
	if err != nil {
		t.Fatalf("getImageID 失败: %v", err)
	}

	if imageID == "" {
		t.Fatal("返回的镜像 ID 为空")
	}

	t.Logf("镜像 ID: %s", imageID)

	// 测试不存在的镜像
	_, err = getImageID("not-exist/image:latest")
	if err == nil {
		t.Fatal("期望返回错误，但没有")
	}
}

// 测试获取镜像层的 diff ID
func TestGetImageDiffIDs(t *testing.T) {
	// 先获取镜像 ID
	imageID, err := getImageID("mcr.microsoft.com/windows/nanoserver:ltsc2022")
	if err != nil {
		t.Fatalf("getImageID 失败: %v", err)
	}

	// 获取 diff ID 列表
	diffIDs, err := getImageDiffIDs(imageID)
	if err != nil {
		t.Fatalf("getImageDiffIDs 失败: %v", err)
	}

	if len(diffIDs) == 0 {
		t.Fatal("没有找到任何层")
	}

	for i, id := range diffIDs {
		t.Logf("层 %d: %s", i, id)
	}
}

// 测试解析单个层
func TestResolveLayer(t *testing.T) {
	// 先获取镜像 ID 和 diff ID
	imageID, err := getImageID("mcr.microsoft.com/windows/nanoserver:ltsc2022")
	if err != nil {
		t.Fatalf("getImageID 失败: %v", err)
	}

	diffIDs, err := getImageDiffIDs(imageID)
	if err != nil {
		t.Fatalf("getImageDiffIDs 失败: %v", err)
	}

	// 解析第一个层（基础层）
	layer, err := resolveLayer(diffIDs[0])
	if err != nil {
		t.Fatalf("resolveLayer 失败: %v", err)
	}

	// 验证层的基本信息
	t.Logf("DiffID: %s", layer.DiffID)
	t.Logf("CacheID: %s", layer.CacheID)
	t.Logf("Path: %s", layer.Path)
	t.Logf("HasFiles: %v", layer.HasFiles)
	t.Logf("HasHives: %v", layer.HasHives)
	t.Logf("HasUtilityVM: %v", layer.HasUtilityVM)
	t.Logf("LayerChain: %v", layer.LayerChain)

	// 验证层必须包含 Files 目录
	if !layer.HasFiles {
		t.Error("层缺少 Files 目录")
	}

	// 验证层必须包含 Hives 目录
	if !layer.HasHives {
		t.Error("层缺少 Hives 目录")
	}

	// 验证层路径存在
	if layer.Path == "" {
		t.Error("层路径为空")
	}
}

// 测试完整的 FindImageLayers 函数
func TestFindImageLayers(t *testing.T) {
	// 测试查找 nanoserver 镜像
	layers, err := FindImageLayers("mcr.microsoft.com/windows/nanoserver:ltsc2022")
	if err != nil {
		t.Fatalf("FindImageLayers 失败: %v", err)
	}

	if len(layers) == 0 {
		t.Fatal("没有找到任何层")
	}

	t.Logf("找到 %d 个层", len(layers))
	for i, layer := range layers {
		t.Logf("层 %d:", i)
		t.Logf("  Path: %s", layer.Path)
		t.Logf("  HasFiles: %v", layer.HasFiles)
		t.Logf("  HasHives: %v", layer.HasHives)
		t.Logf("  HasUtilityVM: %v", layer.HasUtilityVM)
	}

	// 验证最后一层（基础层）必须有 Files 和 Hives
	baseLayer := layers[len(layers)-1]
	if !baseLayer.HasFiles {
		t.Error("基础层缺少 Files 目录")
	}
	if !baseLayer.HasHives {
		t.Error("基础层缺少 Hives 目录")
	}

	// 测试不存在的镜像
	_, err = FindImageLayers("not-exist/image:latest")
	if err == nil {
		t.Fatal("期望返回错误，但没有")
	}
}
