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
	// 1. 找到基础镜像层 ID
	baseLayerID, err := findBaseLayerID()
	if err != nil {
		fmt.Println("找不到镜像层:", err)
		os.Exit(1)
	}
	fmt.Println("使用基础层:", baseLayerID)

	// 2. 配置 filter driver
	info := hcsshim.DriverInfo{
		Flavour: 1, // Windows filter driver
		HomeDir: layersHomeDir,
	}

	// 3. 激活基础层
	fmt.Println("正在激活基础层...")
	if err := hcsshim.ActivateLayer(info, baseLayerID); err != nil {
		fmt.Println("激活基础层失败:", err)
		os.Exit(1)
	}
	defer hcsshim.DeactivateLayer(info, baseLayerID)

	// 4. 准备基础层（无父层）
	if err := hcsshim.PrepareLayer(info, baseLayerID, nil); err != nil {
		fmt.Println("准备基础层失败:", err)
		os.Exit(1)
	}

	// 5. 创建 scratch 层（可写层）
	scratchID := "scratch-" + uuid.New().String()[:8]
	scratchPath := filepath.Join(layersHomeDir, scratchID)
	baseLayerPath := filepath.Join(layersHomeDir, baseLayerID)

	fmt.Println("正在创建 scratch 层...")
	if err := hcsshim.CreateScratchLayer(info, scratchID, baseLayerID, []string{baseLayerPath}); err != nil {
		fmt.Println("创建 scratch 层失败:", err)
		os.Exit(1)
	}
	defer func() {
		hcsshim.DeactivateLayer(info, scratchID)
		hcsshim.DestroyLayer(info, scratchID)
	}()

	// 6. 激活并准备 scratch 层
	if err := hcsshim.ActivateLayer(info, scratchID); err != nil {
		fmt.Println("激活 scratch 层失败:", err)
		os.Exit(1)
	}

	if err := hcsshim.PrepareLayer(info, scratchID, []string{baseLayerPath}); err != nil {
		fmt.Println("准备 scratch 层失败:", err)
		os.Exit(1)
	}

	// 7. 获取 scratch 的 volume GUID path（容器的可写根目录）
	volumePath, err := hcsshim.GetLayerMountPath(info, scratchID)
	if err != nil {
		fmt.Println("获取挂载路径失败:", err)
		os.Exit(1)
	}
	fmt.Println("VolumePath:", volumePath)

	// 8. 构建容器配置
	layerID := "sandbox-layer-" + uuid.New().String()[:8]
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
			{ID: baseLayerID, Path: baseLayerPath},
		},
	}

	// 9. 创建容器
	fmt.Println("正在创建容器...")
	container, err := hcsshim.CreateContainer(layerID, containerCfg)
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

// findBaseLayerID 在系统层目录中找到第一个有效层的 ID
func findBaseLayerID() (string, error) {
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
			return entry.Name(), nil
		}
	}

	return "", fmt.Errorf("没有找到有效层")
}
