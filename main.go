package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	// 1. 检查 Docker 是否可用
	if err := exec.Command("docker", "info").Run(); err != nil {
		fmt.Println("Docker 不可用:", err)
		fmt.Println("请确保 Docker Desktop 已启动")
		return
	}

	// 2. 检查 Docker 模式（Windows/Linux）
	mode, err := getDockerMode()
	if err != nil {
		fmt.Println("获取 Docker 模式失败:", err)
		return
	}
	fmt.Println("Docker 模式:", mode)
	if mode != "windows" {
		fmt.Println("需要切换到 Windows 容器模式")
		fmt.Println("Docker Desktop -> Settings -> Use Windows containers")
		return
	}

	// 3. 检查 nanoserver 镜像
	if !hasImage("mcr.microsoft.com/windows/nanoserver:ltsc2022") {
		fmt.Println("正在拉取 nanoserver 镜像...")
		if err := run("docker", "pull", "mcr.microsoft.com/windows/nanoserver:ltsc2022"); err != nil {
			fmt.Println("拉取镜像失败:", err)
			return
		}
	}

	// 4. 创建容器
	fmt.Println("正在创建容器...")
	containerName := "win-sandbox"
	_ = run("docker", "rm", "-f", containerName) // 清理旧容器

	if err := run("docker", "create", "--name", containerName,
		"--memory", "1g",
		"--cpus", "2",
		"--network", "none",
		"mcr.microsoft.com/windows/nanoserver:ltsc2022",
		"cmd", "/c", "ping", "-t", "localhost",
	); err != nil {
		fmt.Println("创建容器失败:", err)
		return
	}

	// 5. 启动容器
	fmt.Println("正在启动容器...")
	if err := run("docker", "start", containerName); err != nil {
		fmt.Println("启动容器失败:", err)
		return
	}
	defer func() {
		fmt.Println("正在清理容器...")
		exec.Command("docker", "stop", "-t", "0", containerName).Run()
		exec.Command("docker", "rm", "-f", containerName).Run()
	}()

	// 6. 在容器中执行命令
	fmt.Println("正在执行命令...")
	result, err := execInContainer(containerName, "cmd /c echo hello from sandbox && hostname")
	if err != nil {
		fmt.Println("执行命令失败:", err)
		return
	}

	fmt.Println("=== stdout ===")
	fmt.Print(result.Stdout)
	fmt.Println("=== stderr ===")
	fmt.Print(result.Stderr)
	fmt.Printf("=== 退出码: %d ===\n", result.ExitCode)
}

// ExecResult 命令执行结果
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// getDockerMode 获取 Docker 模式（windows/linux）
func getDockerMode() (string, error) {
	out, err := exec.Command("docker", "info", "--format", "{{.OSType}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// hasImage 检查镜像是否存在
func hasImage(image string) bool {
	out, err := exec.Command("docker", "images", "-q", image).Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// execInContainer 在容器中执行命令
func execInContainer(container string, cmd string) (*ExecResult, error) {
	args := append([]string{"exec", container}, strings.Fields(cmd)...)
	execCmd := exec.Command("docker", args...)

	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// run 执行命令并显示输出
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = exec.Discard
	cmd.Stderr = exec.Discard
	return cmd.Run()
}
