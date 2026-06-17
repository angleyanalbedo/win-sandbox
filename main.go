package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim"
)

func main() {
	// 1. 找到容器镜像层
	layers, err := findLayers()
	if err != nil {
		fmt.Println("找不到镜像层:", err)
		os.Exit(1)
	}
	fmt.Println("找到镜像层:", layers)

	// 2. 构建容器配置
	cfg := &hcsshim.ContainerConfig{
		SystemType:              "Container",
		Name:                    "win-sandbox",
		HvPartition:             false, // 先用进程隔离，简单
		ProcessorCount:          2,
		MemoryMaximumInMB:       1024,
		TerminateOnLastHandleClosed: true,
		Layers:                  layers,
	}

	// 3. 创建容器
	fmt.Println("正在创建容器...")
	container, err := hcsshim.CreateContainer("win-sandbox", cfg)
	if err != nil {
		fmt.Println("创建容器失败:", err)
		os.Exit(1)
	}
	defer container.Close()

	// 4. 启动容器
	fmt.Println("正在启动容器...")
	if err := container.Start(); err != nil {
		fmt.Println("启动容器失败:", err)
		os.Exit(1)
	}

	// 5. 在容器里执行命令
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

	// 6. 读取输出
	_, stdout, stderr, _ := proc.Stdio()
	var outBuf, errBuf bytes.Buffer
	go outBuf.ReadFrom(stdout)
	go errBuf.ReadFrom(stderr)

	// 7. 等待退出
	proc.Wait()
	exitCode, _ := proc.ExitCode()

	fmt.Println("=== stdout ===")
	fmt.Print(outBuf.String())
	fmt.Println("=== stderr ===")
	fmt.Print(errBuf.String())
	fmt.Printf("=== 退出码: %d ===\n", exitCode)

	// 8. 关闭容器
	container.Shutdown()
	container.Terminate()
}

// findLayers 在系统上查找容器镜像层
func findLayers() ([]hcsshim.Layer, error) {
	// Windows 容器层的标准存储位置
	basePath := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Containers", "Layers")

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, fmt.Errorf("读取层目录失败: %w", err)
	}

	var layers []hcsshim.Layer
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		layerPath := filepath.Join(basePath, entry.Name())
		// 检查是否是有效的层（包含 Files 目录）
		if _, err := os.Stat(filepath.Join(layerPath, "Files")); err == nil {
			layers = append(layers, hcsshim.Layer{
				ID:   entry.Name(),
				Path: layerPath,
			})
		}
	}

	if len(layers) == 0 {
		return nil, fmt.Errorf("在 %s 下没有找到有效层", basePath)
	}

	// HCS 需要层按从底到顶排序，基础层在前
	// 简单处理：只有一个层时直接返回
	return layers, nil
}

// findLayersFromDocker 从 Docker 的存储目录找层（如果 Docker 已安装）
func findLayersFromDocker() ([]hcsshim.Layer, error) {
	dockerPath := filepath.Join(os.Getenv("ProgramData"), "Docker", "windowsfilter")

	entries, err := os.ReadDir(dockerPath)
	if err != nil {
		return nil, fmt.Errorf("Docker 层目录不存在: %w", err)
	}

	var layers []hcsshim.Layer
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		layerPath := filepath.Join(dockerPath, entry.Name())
		if _, err := os.Stat(filepath.Join(layerPath, "Files")); err == nil {
			layers = append(layers, hcsshim.Layer{
				ID:   entry.Name(),
				Path: layerPath,
			})
		}
	}

	if len(layers) == 0 {
		return nil, fmt.Errorf("Docker 目录下没有找到层")
	}

	return layers, nil
}
