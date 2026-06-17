package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Microsoft/hcsshim"
	"github.com/google/uuid"
)

// 层存储目录
const layersHomeDir = `C:\ProgramData\Microsoft\Windows\Containers\Layers`

func main() {
	// 1. 找到基础镜像层
	baseLayerPath, err := findBaseLayer()
	if err != nil {
		fmt.Println("找不到镜像层:", err)
		os.Exit(1)
	}
	fmt.Println("使用基础层:", baseLayerPath)

	// 2. 配置 filter driver
	info := hcsshim.DriverInfo{
		Flavour: 1, // Windows filter driver
		HomeDir: layersHomeDir,
	}

	// 3. 创建 scratch 目录
	scratchID := "sandbox-" + uuid.New().String()[:8]
	scratchPath := filepath.Join(layersHomeDir, scratchID)
	if err := os.MkdirAll(scratchPath, 0755); err != nil {
		fmt.Println("创建 scratch 目录失败:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(scratchPath)

	// 4. 创建 scratch 层（sandbox.vhdx）
	// 从 hcsoci 源码学到的：CreateScratchLayer 需要父层路径列表
	fmt.Println("正在创建 scratch 层...")
	if err := hcsshim.CreateScratchLayer(info, scratchID, "", []string{baseLayerPath}); err != nil {
		fmt.Println("创建 scratch 层失败:", err)
		os.Exit(1)
	}

	// 5. 激活 scratch 层（不是基础层！）
	// 从 hcsoci/mountProcessIsolatedWCIFSLayers 学到的：激活 scratch
	fmt.Println("正在激活 scratch 层...")
	if err := hcsshim.ActivateLayer(info, scratchID); err != nil {
		fmt.Println("激活 scratch 层失败:", err)
		os.Exit(1)
	}
	defer hcsshim.DeactivateLayer(info, scratchID)

	// 6. 准备 scratch 层（传入父层路径）
	if err := hcsshim.PrepareLayer(info, scratchID, []string{baseLayerPath}); err != nil {
		fmt.Println("准备 scratch 层失败:", err)
		os.Exit(1)
	}
	defer hcsshim.UnprepareLayer(info, scratchID)

	// 7. 获取 volume GUID path
	volumePath, err := hcsshim.GetLayerMountPath(info, scratchID)
	if err != nil {
		fmt.Println("获取挂载路径失败:", err)
		os.Exit(1)
	}
	fmt.Println("VolumePath:", volumePath)

	// 8. 构建容器配置
	containerID := "sandbox-" + uuid.New().String()[:8]
	containerCfg := &hcsshim.ContainerConfig{
		SystemType:              "Container",
		Name:                    "win-sandbox",
		HvPartition:             false,
		VolumePath:              volumePath,
		LayerFolderPath:         scratchPath,
		ProcessorCount:          2,
		MemoryMaximumInMB:       1024,
		TerminateOnLastHandleClosed: true,
		Layers: []hcsshim.Layer{
			{ID: filepath.Base(baseLayerPath), Path: baseLayerPath},
		},
	}

	// 9. 创建容器
	fmt.Println("正在创建容器...")
	container, err := hcsshim.CreateContainer(containerID, containerCfg)
	if err != nil {
		fmt.Println("创建容器失败:", err)
		os.Exit(1)
	}
	defer container.Close()

	// 10. 启动容器
	fmt.Println("正在启动容器...")
	if err := container.Start(); err != nil {
		fmt.Println("启动容器失败:", err)
		os.Exit(1)
	}

	// 11. 执行命令
	fmt.Println("正在执行命令...")
	proc, err := container.CreateProcess(&hcsshim.ProcessConfig{
		CommandLine:      "cmd /c echo hello from sandbox && hostname",
		CreateStdOutPipe: true,
		CreateStdErrPipe: true,
	})
	if err != nil {
		fmt.Println("执行命令失败:", err)
		os.Exit(1)
	}
	defer proc.Close()

	// 12. 读取输出
	_, stdout, stderr, _ := proc.Stdio()
	var outBuf, errBuf bytes.Buffer
	go outBuf.ReadFrom(stdout)
	go errBuf.ReadFrom(stderr)

	proc.Wait()
	exitCode, _ := proc.ExitCode()

	fmt.Println("=== stdout ===")
	fmt.Print(outBuf.String())
	fmt.Println("=== stderr ===")
	fmt.Print(errBuf.String())
	fmt.Printf("=== 退出码: %d ===\n", exitCode)

	// 13. 清理
	container.Shutdown()
	container.Terminate()
}

// findBaseLayer 在系统层目录中找到第一个有效层的完整路径
func findBaseLayer() (string, error) {
	entries, err := os.ReadDir(layersHomeDir)
	if err != nil {
		return "", fmt.Errorf("读取层目录失败: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		layerPath := filepath.Join(layersHomeDir, entry.Name())
		if _, err := os.Stat(filepath.Join(layerPath, "Files")); err == nil {
			return layerPath, nil
		}
	}

	return "", fmt.Errorf("在 %s 下没有找到有效层", layersHomeDir)
}
