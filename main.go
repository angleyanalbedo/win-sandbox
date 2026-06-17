package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Microsoft/hcsshim"
	"github.com/angleyanalbedo/win-sandbox/pkg/docker"
	"github.com/google/uuid"
)

func main() {
	// 1. 从 Docker 存储中查找 nanoserver 镜像的层
	imageRef := "mcr.microsoft.com/windows/nanoserver:ltsc2022"
	fmt.Println("正在查找镜像层:", imageRef)

	layers, err := docker.FindImageLayers(imageRef)
	if err != nil {
		fmt.Println("查找镜像层失败:", err)
		fmt.Println("请确保已拉取镜像: docker pull", imageRef)
		os.Exit(1)
	}

	// 使用最后一层（基础层）
	baseLayer := layers[len(layers)-1]
	fmt.Println("基础层路径:", baseLayer.Path)
	fmt.Println("  HasFiles:", baseLayer.HasFiles)
	fmt.Println("  HasHives:", baseLayer.HasHives)
	fmt.Println("  HasUtilityVM:", baseLayer.HasUtilityVM)

	if !baseLayer.HasFiles {
		fmt.Println("错误: 基础层缺少 Files 目录")
		os.Exit(1)
	}

	// 2. 配置 filter driver（指向 Docker 的 windowsfilter 目录）
	info := hcsshim.DriverInfo{
		Flavour: 1,
		HomeDir: docker.LayerStore,
	}

	// 3. 创建 scratch 目录
	scratchID := "sandbox-" + uuid.New().String()[:8]
	scratchPath := filepath.Join(docker.LayerStore, scratchID)
	if err := os.MkdirAll(scratchPath, 0755); err != nil {
		fmt.Println("创建 scratch 目录失败:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(scratchPath)

	// 4. 创建 scratch 层（sandbox.vhdx）
	fmt.Println("正在创建 scratch 层...")
	if err := hcsshim.CreateScratchLayer(info, scratchID, "", []string{baseLayer.Path}); err != nil {
		fmt.Println("创建 scratch 层失败:", err)
		os.Exit(1)
	}

	// 5. 激活 scratch 层
	fmt.Println("正在激活 scratch 层...")
	if err := hcsshim.ActivateLayer(info, scratchID); err != nil {
		fmt.Println("激活 scratch 层失败:", err)
		os.Exit(1)
	}
	defer hcsshim.DeactivateLayer(info, scratchID)

	// 6. 准备 scratch 层（传入基础层作为父层）
	if err := hcsshim.PrepareLayer(info, scratchID, []string{baseLayer.Path}); err != nil {
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
			{ID: filepath.Base(baseLayer.Path), Path: baseLayer.Path},
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
	fmt.Printf("=== exit code: %d ===\n", exitCode)

	// 13. 清理
	container.Shutdown()
	container.Terminate()
}
