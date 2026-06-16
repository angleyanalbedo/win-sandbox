package sandbox

import (
	"encoding/json"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

var (
	modVMCompute = syscall.NewLazyDLL("vmcompute.dll")
	modOle32     = syscall.NewLazyDLL("ole32.dll")

	procHcsCreateComputeSystem            = modVMCompute.NewProc("HcsCreateComputeSystem")
	procHcsStartComputeSystem             = modVMCompute.NewProc("HcsStartComputeSystem")
	procHcsTerminateComputeSystem         = modVMCompute.NewProc("HcsTerminateComputeSystem")
	procHcsCreateOperation                = modVMCompute.NewProc("HcsCreateOperation")
	procHcsCloseOperation                 = modVMCompute.NewProc("HcsCloseOperation")
	procHcsWaitForOperationResult         = modVMCompute.NewProc("HcsWaitForOperationResult")
	procHcsCreateProcess                  = modVMCompute.NewProc("HcsCreateProcess")
	procHcsWaitForProcessInComputeSystem  = modVMCompute.NewProc("HcsWaitForProcessInComputeSystem")
	procHcsGetProcessProperties           = modVMCompute.NewProc("HcsGetProcessProperties")
	procCoTaskMemFree                     = modOle32.NewProc("CoTaskMemFree")

	// 尝试 computecore.dll（新版 Windows 拆分了部分函数到这里）
	modComputeCore = syscall.NewLazyDLL("computecore.dll")
)

const (
	hcsEventOperationCompleted = 3
	hcsProcessStatistics       = 0
	waitTimeout                = 30 // 秒
)

// getProc 从多个 DLL 中查找函数
func getProc(name string) *syscall.LazyProc {
	if p := modVMCompute.NewProc(name); p.Find() == nil {
		return p
	}
	if p := modComputeCore.NewProc(name); p.Find() == nil {
		return p
	}
	return nil
}

// HCSHandle HCS 句柄类型
type HCSHandle uintptr

// hcsOperation 结构
type hcsOperation struct {
	handle HCSHandle
}

// createOperation 创建 HCS 操作
func createOperation() (*hcsOperation, error) {
	var handle HCSHandle
	r1, _, err := procHcsCreateOperation.Call(0, 0, uintptr(unsafe.Pointer(&handle)))
	if r1 != 0 {
		return nil, fmt.Errorf("HcsCreateOperation 失败: %v", err)
	}
	return &hcsOperation{handle: handle}, nil
}

// close 关闭操作
func (op *hcsOperation) close() {
	if op.handle != 0 {
		procHcsCloseOperation.Call(uintptr(op.handle))
		op.handle = 0
	}
}

// waitForResult 等待操作结果
func (op *hcsOperation) waitForResult(timeoutSec int) (string, error) {
	var result *uint16
	timeout := uint32(timeoutSec * 1000) // 转换为毫秒
	r1, _, err := procHcsWaitForOperationResult.Call(
		uintptr(op.handle),
		uintptr(timeout),
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 != 0 {
		return "", fmt.Errorf("HcsWaitForOperationResult 失败: %v", err)
	}
	if result != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(result)))
		return UTF16PtrToString(result), nil
	}
	return "", nil
}

// CreateComputeSystemV2 使用 HCS v2 JSON 创建 compute system
func CreateComputeSystemV2(id string, configJSON string) (HCSHandle, error) {
	logrus.WithField("id", id).Debug("正在创建 compute system (v2)...")

	op, err := createOperation()
	if err != nil {
		return 0, err
	}
	defer op.close()

	idPtr, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return 0, err
	}
	configPtr, err := syscall.UTF16PtrFromString(configJSON)
	if err != nil {
		return 0, err
	}

	var system HCSHandle
	r1, _, err := procHcsCreateComputeSystem.Call(
		uintptr(unsafe.Pointer(idPtr)),
		uintptr(unsafe.Pointer(configPtr)),
		uintptr(op.handle),
		0, // securityDescriptor
		uintptr(unsafe.Pointer(&system)),
	)
	if r1 != 0 {
		// 尝试获取详细错误信息
		result, _ := op.waitForResult(5)
		return 0, fmt.Errorf("HcsCreateComputeSystem 失败 (HRESULT: 0x%X, %v) - %s", r1, err, result)
	}

	// 等待操作完成
	result, err := op.waitForResult(waitTimeout)
	if err != nil {
		return 0, fmt.Errorf("等待创建完成失败: %w", err)
	}
	if result != "" {
		logrus.WithField("result", result).Debug("HcsCreateComputeSystem 完成")
	}

	return system, nil
}

// StartComputeSystem 启动 compute system
func StartComputeSystem(system HCSHandle) error {
	logrus.Debug("正在启动 compute system...")

	op, err := createOperation()
	if err != nil {
		return err
	}
	defer op.close()

	r1, _, err := procHcsStartComputeSystem.Call(
		uintptr(system),
		uintptr(op.handle),
		0, // options
	)
	if r1 != 0 {
		result, _ := op.waitForResult(5)
		return fmt.Errorf("HcsStartComputeSystem 失败 (HRESULT: 0x%X, %v) - %s", r1, err, result)
	}

	// 等待操作完成
	result, err := op.waitForResult(waitTimeout)
	if err != nil {
		return fmt.Errorf("等待启动完成失败: %w", err)
	}
	if result != "" {
		logrus.WithField("result", result).Debug("HcsStartComputeSystem 完成")
	}

	return nil
}

// TerminateComputeSystem 终止 compute system
func TerminateComputeSystem(system HCSHandle) error {
	logrus.Debug("正在终止 compute system...")

	op, err := createOperation()
	if err != nil {
		return err
	}
	defer op.close()

	r1, _, err := procHcsTerminateComputeSystem.Call(
		uintptr(system),
		uintptr(op.handle),
		0, // options
	)
	if r1 != 0 {
		return fmt.Errorf("HcsTerminateComputeSystem 失败 (HRESULT: 0x%X, %v)", r1, err)
	}

	// 等待操作完成
	_, err = op.waitForResult(waitTimeout)
	return err
}

// CreateProcessV2 使用 v2 API 在 compute system 中创建进程
func CreateProcessV2(system HCSHandle, processConfigJSON string) (HCSHandle, HCSHandle, HCSHandle, HCSHandle, error) {
	logrus.Debug("正在创建进程...")

	op, err := createOperation()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer op.close()

	configPtr, err := syscall.UTF16PtrFromString(processConfigJSON)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	var process HCSHandle
	var stdin, stdout, stderr HCSHandle
	r1, _, err := procHcsCreateProcess.Call(
		uintptr(system),
		uintptr(unsafe.Pointer(configPtr)),
		uintptr(op.handle),
		uintptr(unsafe.Pointer(&process)),
		uintptr(unsafe.Pointer(&stdin)),
		uintptr(unsafe.Pointer(&stdout)),
		uintptr(unsafe.Pointer(&stderr)),
	)
	if r1 != 0 {
		result, _ := op.waitForResult(5)
		return 0, 0, 0, 0, fmt.Errorf("HcsCreateProcess 失败 (HRESULT: 0x%X, %v) - %s", r1, err, result)
	}

	// 等待操作完成
	_, err = op.waitForResult(waitTimeout)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("等待进程创建完成失败: %w", err)
	}

	return process, stdin, stdout, stderr, nil
}

// WaitForProcessV2 等待进程退出
func WaitForProcessV2(system HCSHandle, process HCSHandle, timeout time.Duration) (int, error) {
	logrus.Debug("正在等待进程退出...")

	op, err := createOperation()
	if err != nil {
		return -1, err
	}
	defer op.close()

	timeoutMs := uint32(timeout.Milliseconds())
	var result *uint16
	r1, _, err := procHcsWaitForProcessInComputeSystem.Call(
		uintptr(system),
		uintptr(process),
		uintptr(timeoutMs),
		uintptr(op.handle),
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 != 0 {
		return -1, fmt.Errorf("HcsWaitForProcessInComputeSystem 失败 (HRESULT: 0x%X, %v)", r1, err)
	}

	// 等待操作完成
	resultStr, err := op.waitForResult(int(timeout.Seconds()) + 10)
	if err != nil {
		return -1, fmt.Errorf("等待进程退出失败: %w", err)
	}

	// 解析退出码
	exitCode := -1
	if resultStr != "" {
		var parsed struct {
			ProcessStatus struct {
				ExitCode int `json:"ExitCode"`
			} `json:"ProcessStatus"`
		}
		if jsonErr := json.Unmarshal([]byte(resultStr), &parsed); jsonErr == nil {
			exitCode = parsed.ProcessStatus.ExitCode
		}
	}

	if result != nil {
		procCoTaskMemFree.Call(uintptr(unsafe.Pointer(result)))
	}

	return exitCode, nil
}

// ReadPipe 从管道句柄读取数据
func ReadPipe(handle HCSHandle) ([]byte, error) {
	if handle == 0 {
		return nil, nil
	}

	var data []byte
	buf := make([]byte, 4096)

	for {
		var bytesRead uint32
		err := syscall.ReadFile(
			syscall.Handle(handle),
			buf,
			&bytesRead,
			nil, // overlapped
		)
		if err != nil {
			// ERROR_BROKEN_PIPE (109) 表示管道已关闭
			if err == syscall.ERROR_BROKEN_PIPE {
				break
			}
			return data, fmt.Errorf("读取管道失败: %v", err)
		}
		if bytesRead == 0 {
			break
		}
		data = append(data, buf[:bytesRead]...)
	}

	return data, nil
}

// CloseHandle 关闭句柄
func CloseHandle(handle HCSHandle) {
	if handle != 0 {
		syscall.CloseHandle(syscall.Handle(handle))
	}
}

// UTF16PtrToString 将 UTF-16 指针转换为 Go 字符串
func UTF16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// 找到字符串结尾
	end := unsafe.Pointer(p)
	n := 0
	for *(*uint16)(end) != 0 {
		end = unsafe.Pointer(uintptr(end) + 2)
		n++
	}
	return syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(p))[:n:n])
}

// CheckHCSAPI 检查 HCS API 是否可用
func CheckHCSAPI() error {
	if err := procHcsCreateComputeSystem.Find(); err != nil {
		return fmt.Errorf("HcsCreateComputeSystem 不可用: %v", err)
	}
	if err := procHcsStartComputeSystem.Find(); err != nil {
		return fmt.Errorf("HcsStartComputeSystem 不可用: %v", err)
	}
	if err := procHcsTerminateComputeSystem.Find(); err != nil {
		return fmt.Errorf("HcsTerminateComputeSystem 不可用: %v", err)
	}
	if err := procHcsCreateProcess.Find(); err != nil {
		return fmt.Errorf("HcsCreateProcess 不可用: %v", err)
	}
	return nil
}
