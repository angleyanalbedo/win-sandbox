package sandbox

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modShell32        = syscall.NewLazyDLL("shell32.dll")
	modKernel32       = syscall.NewLazyDLL("kernel32.dll")
	procShellExecuteW = modShell32.NewProc("ShellExecuteW")
)

const (
	// seeMSgBox = 0
	// swHide    = 0
)

// IsAdmin 检查当前进程是否以管理员权限运行
func IsAdmin() bool {
	var sid *windows.SID
	// 使用 SECURITY_BUILTIN_ADMIN_RID 构建管理员 SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

// ElevateSelf 以管理员权限重新启动当前进程（触发 UAC 弹窗）
// 会阻塞直到提权后的进程退出，然后以相同退出码退出当前进程
func ElevateSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 保留原始参数
	args := os.Args[1:]

	// 使用 ShellExecuteW 以 runas 动词启动
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(joinArgs(args))
	dir, _ := syscall.UTF16PtrFromString("")

	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(1), // SW_SHOWNORMAL
	)

	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW 失败，返回值: %d", ret)
	}

	// 提权后的进程已启动，当前进程退出
	os.Exit(0)
	return nil
}

// EnsureAdmin 确保以管理员权限运行，如果不是则自动触发 UAC 提权
func EnsureAdmin() error {
	if IsAdmin() {
		return nil
	}
	return ElevateSelf()
}

// joinArgs 将参数数组拼接为命令行字符串，正确处理带空格的参数
func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		// 如果参数包含空格，用双引号包裹
		if needsQuoting(arg) {
			result += `"` + arg + `"`
		} else {
			result += arg
		}
	}
	return result
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '"' {
			return true
		}
	}
	return false
}
