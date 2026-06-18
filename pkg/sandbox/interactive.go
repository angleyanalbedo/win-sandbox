package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/Microsoft/hcsshim"
	"golang.org/x/sys/windows"
)

// ExecInteractive 在沙箱中执行命令（交互式模式）
// 接管当前终端的 stdin/stdout/stderr，支持全双工交互
func (s *Sandbox) ExecInteractive(command string) (int, error) {
	if s.container == nil {
		return -1, fmt.Errorf("沙箱未创建或未连接")
	}

	// 创建进程（带 stdin/stdout/stderr 管道）
	proc, err := s.container.CreateProcess(&hcsshim.ProcessConfig{
		CommandLine:      command,
		CreateStdInPipe:  true,
		CreateStdOutPipe: true,
		CreateStdErrPipe: true,
		WorkingDirectory: s.config.WorkDir,
		Environment:      envToMap(s.config.Env),
	})
	if err != nil {
		return -1, fmt.Errorf("创建进程失败: %w", err)
	}
	defer proc.Close()

	// 获取管道
	stdinP, stdoutP, stderrP, _ := proc.Stdio()

	// 设置终端为 raw mode
	oldMode, err := setConsoleRaw()
	if err != nil {
		return -1, fmt.Errorf("设置终端 raw mode 失败: %w", err)
	}
	defer restoreConsole(oldMode)

	// 处理 Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	// 转发 stdin → 容器进程
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		io.Copy(stdinP, os.Stdin)
	}()

	// 转发容器进程 stdout/stderr → 终端
	go func() {
		io.Copy(os.Stdout, stdoutP)
	}()
	go func() {
		io.Copy(os.Stderr, stderrP)
	}()

	// 等待进程退出或中断
	go func() {
		<-sigCh
		proc.Close()
	}()

	proc.Wait()
	exitCode, _ := proc.ExitCode()

	return exitCode, nil
}

// setConsoleRaw 设置控制台为 raw mode
// 返回旧的控制台模式用于恢复
func setConsoleRaw() (uint32, error) {
	handle := windows.Handle(os.Stdin.Fd())

	var oldMode uint32
	if err := windows.GetConsoleMode(handle, &oldMode); err != nil {
		return 0, err
	}

	// raw mode: 禁用行缓冲、回显、信号处理
	newMode := oldMode
	newMode &^= windows.ENABLE_LINE_INPUT
	newMode &^= windows.ENABLE_ECHO_INPUT
	newMode &^= windows.ENABLE_PROCESSED_INPUT
	newMode |= windows.ENABLE_WINDOW_INPUT

	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return 0, err
	}

	return oldMode, nil
}

// restoreConsole 恢复控制台模式
func restoreConsole(oldMode uint32) {
	handle := windows.Handle(os.Stdin.Fd())
	windows.SetConsoleMode(handle, oldMode)
}
