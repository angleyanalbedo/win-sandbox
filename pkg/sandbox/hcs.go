package sandbox

import (
	"encoding/json"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

var (
	modVMCompute   = windows.NewLazyDLL("vmcompute.dll")
	modComputeCore = windows.NewLazyDLL("computecore.dll")

	// 必需函数
	procHcsCreateComputeSystem    = getProc("HcsCreateComputeSystem")
	procHcsStartComputeSystem     = getProc("HcsStartComputeSystem")
	procHcsTerminateComputeSystem = getProc("HcsTerminateComputeSystem")
	procHcsCreateProcess          = getProc("HcsCreateProcess")

	// 可选函数
	procHcsCreateOperation               = getProc("HcsCreateOperation")
	procHcsCloseOperation                = getProc("HcsCloseOperation")
	procHcsWaitForOperationResult        = getProc("HcsWaitForOperationResult")
	procHcsWaitForProcessInComputeSystem = getProc("HcsWaitForProcessInComputeSystem")
	procHcsGetProcessProperties          = getProc("HcsGetProcessProperties")

	hasOperationAPI bool
)

func init() {
	hasOperationAPI = procHcsCreateOperation != nil &&
		procHcsCloseOperation != nil &&
		procHcsWaitForOperationResult != nil
}

func getProc(name string) *windows.LazyProc {
	if p := modVMCompute.NewProc(name); p.Find() == nil {
		return p
	}
	if p := modComputeCore.NewProc(name); p.Find() == nil {
		return p
	}
	return nil
}

type HCSHandle uintptr

type hcsOperation struct {
	handle HCSHandle
}

func createOperation() *hcsOperation {
	if !hasOperationAPI {
		logrus.Debug("HcsCreateOperation 不可用，使用 NULL 操作")
		return &hcsOperation{handle: 0}
	}
	// HcsCreateOperation 直接返回句柄（不是通过输出参数）
	// HCS_OPERATION HcsCreateOperation(HCS_CALLBACK callback, void *context);
	handle, _, callErr := procHcsCreateOperation.Call(0, 0)
	logrus.WithFields(logrus.Fields{
		"handle":  fmt.Sprintf("0x%X", handle),
		"callErr": callErr,
	}).Debug("HcsCreateOperation 返回")
	return &hcsOperation{handle: HCSHandle(handle)}
}

func (op *hcsOperation) close() {
	if op.handle != 0 && procHcsCloseOperation != nil {
		procHcsCloseOperation.Call(uintptr(op.handle))
		op.handle = 0
	}
}

func (op *hcsOperation) waitForResult(timeoutSec int) (string, error) {
	if op.handle == 0 || !hasOperationAPI {
		return "", nil
	}
	var result *uint16
	timeout := uint32(timeoutSec * 1000)
	r1, _, err := procHcsWaitForOperationResult.Call(
		uintptr(op.handle),
		uintptr(timeout),
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 != 0 {
		return "", fmt.Errorf("HcsWaitForOperationResult 失败: %v", err)
	}
	if result != nil {
		defer windows.CoTaskMemFree(unsafe.Pointer(result))
		return windows.UTF16PtrToString(result), nil
	}
	return "", nil
}

func CreateComputeSystemV2(id string, configJSON string) (HCSHandle, error) {
	logrus.WithField("id", id).Debug("正在创建 compute system (v2)...")

	if procHcsCreateComputeSystem == nil {
		return 0, fmt.Errorf("HcsCreateComputeSystem 不可用")
	}

	op := createOperation()
	defer op.close()

	logrus.WithField("opHandle", op.handle).Debug("操作句柄")

	idPtr, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return 0, fmt.Errorf("转换 id 失败: %w", err)
	}
	configPtr, err := windows.UTF16PtrFromString(configJSON)
	if err != nil {
		return 0, fmt.Errorf("转换 config 失败: %w", err)
	}

	var system HCSHandle

	logrus.WithFields(logrus.Fields{
		"funcAddr":  fmt.Sprintf("0x%X", procHcsCreateComputeSystem.Addr()),
		"idPtr":     fmt.Sprintf("0x%X", uintptr(unsafe.Pointer(idPtr))),
		"configPtr": fmt.Sprintf("0x%X", uintptr(unsafe.Pointer(configPtr))),
		"opHandle":  fmt.Sprintf("0x%X", uintptr(op.handle)),
		"systemPtr": fmt.Sprintf("0x%X", uintptr(unsafe.Pointer(&system))),
	}).Debug("调用参数")

	// 使用 Syscall6 直接调用（与 hcsshim 一致）
	r1, _, callErr := syscall.Syscall6(
		procHcsCreateComputeSystem.Addr(),
		5,
		uintptr(unsafe.Pointer(idPtr)),
		uintptr(unsafe.Pointer(configPtr)),
		uintptr(op.handle),
		0,
		uintptr(unsafe.Pointer(&system)),
		0,
	)

	logrus.WithFields(logrus.Fields{
		"r1":       fmt.Sprintf("0x%X", r1),
		"callErr":  callErr,
		"system":   fmt.Sprintf("0x%X", system),
	}).Debug("HcsCreateComputeSystem 返回")

	if r1 != 0 {
		result, _ := op.waitForResult(5)
		return 0, fmt.Errorf("HcsCreateComputeSystem 失败 (HRESULT: 0x%X, %v) - %s", r1, callErr, result)
	}

	result, err := op.waitForResult(waitTimeout)
	if err != nil {
		return 0, fmt.Errorf("等待创建完成失败: %w", err)
	}
	if result != "" {
		logrus.WithField("result", result).Debug("HcsCreateComputeSystem 完成")
	}

	return system, nil
}

func StartComputeSystem(system HCSHandle) error {
	logrus.Debug("正在启动 compute system...")

	if procHcsStartComputeSystem == nil {
		return fmt.Errorf("HcsStartComputeSystem 不可用")
	}

	op := createOperation()
	defer op.close()

	r1, _, callErr := procHcsStartComputeSystem.Call(
		uintptr(system),
		uintptr(op.handle),
		0,
	)

	logrus.WithFields(logrus.Fields{
		"r1":      fmt.Sprintf("0x%X", r1),
		"callErr": callErr,
	}).Debug("HcsStartComputeSystem 返回")

	if r1 != 0 {
		result, _ := op.waitForResult(5)
		return fmt.Errorf("HcsStartComputeSystem 失败 (HRESULT: 0x%X, %v) - %s", r1, callErr, result)
	}

	result, err := op.waitForResult(waitTimeout)
	if err != nil {
		return fmt.Errorf("等待启动完成失败: %w", err)
	}
	if result != "" {
		logrus.WithField("result", result).Debug("HcsStartComputeSystem 完成")
	}

	return nil
}

func TerminateComputeSystem(system HCSHandle) error {
	logrus.Debug("正在终止 compute system...")

	if procHcsTerminateComputeSystem == nil {
		return fmt.Errorf("HcsTerminateComputeSystem 不可用")
	}

	op := createOperation()
	defer op.close()

	r1, _, callErr := procHcsTerminateComputeSystem.Call(
		uintptr(system),
		uintptr(op.handle),
		0,
	)
	if r1 != 0 {
		return fmt.Errorf("HcsTerminateComputeSystem 失败 (HRESULT: 0x%X, %v)", r1, callErr)
	}

	_, err := op.waitForResult(waitTimeout)
	return err
}

func CreateProcessV2(system HCSHandle, processConfigJSON string) (HCSHandle, HCSHandle, HCSHandle, HCSHandle, error) {
	logrus.Debug("正在创建进程...")

	if procHcsCreateProcess == nil {
		return 0, 0, 0, 0, fmt.Errorf("HcsCreateProcess 不可用")
	}

	op := createOperation()
	defer op.close()

	configPtr, err := windows.UTF16PtrFromString(processConfigJSON)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	var process, stdin, stdout, stderr HCSHandle
	r1, _, callErr := procHcsCreateProcess.Call(
		uintptr(system),
		uintptr(unsafe.Pointer(configPtr)),
		uintptr(op.handle),
		uintptr(unsafe.Pointer(&process)),
		uintptr(unsafe.Pointer(&stdin)),
		uintptr(unsafe.Pointer(&stdout)),
		uintptr(unsafe.Pointer(&stderr)),
	)

	logrus.WithFields(logrus.Fields{
		"r1":      fmt.Sprintf("0x%X", r1),
		"callErr": callErr,
		"process": process,
	}).Debug("HcsCreateProcess 返回")

	if r1 != 0 {
		result, _ := op.waitForResult(5)
		return 0, 0, 0, 0, fmt.Errorf("HcsCreateProcess 失败 (HRESULT: 0x%X, %v) - %s", r1, callErr, result)
	}

	_, err = op.waitForResult(waitTimeout)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("等待进程创建完成失败: %w", err)
	}

	return process, stdin, stdout, stderr, nil
}

func WaitForProcessV2(system HCSHandle, process HCSHandle, timeout time.Duration) (int, error) {
	logrus.Debug("正在等待进程退出...")

	if procHcsWaitForProcessInComputeSystem != nil {
		return waitForProcessWithAPI(system, process, timeout)
	}
	return waitForProcessPolling(system, process, timeout)
}

func waitForProcessWithAPI(system HCSHandle, process HCSHandle, timeout time.Duration) (int, error) {
	op := createOperation()
	defer op.close()

	timeoutMs := uint32(timeout.Milliseconds())
	var result *uint16
	r1, _, callErr := procHcsWaitForProcessInComputeSystem.Call(
		uintptr(system),
		uintptr(process),
		uintptr(timeoutMs),
		uintptr(op.handle),
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 != 0 {
		return -1, fmt.Errorf("HcsWaitForProcessInComputeSystem 失败 (HRESULT: 0x%X, %v)", r1, callErr)
	}

	resultStr, err := op.waitForResult(int(timeout.Seconds()) + 10)
	if err != nil {
		return -1, fmt.Errorf("等待进程退出失败: %w", err)
	}

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
		windows.CoTaskMemFree(unsafe.Pointer(result))
	}

	return exitCode, nil
}

func waitForProcessPolling(system HCSHandle, process HCSHandle, timeout time.Duration) (int, error) {
	if procHcsGetProcessProperties == nil {
		logrus.Warn("HcsWaitForProcessInComputeSystem 和 HcsGetProcessProperties 都不可用，等待固定时间")
		time.Sleep(timeout)
		return -1, nil
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var result *uint16
		r1, _, _ := procHcsGetProcessProperties.Call(
			uintptr(system),
			uintptr(process),
			uintptr(unsafe.Pointer(&result)),
		)

		if r1 == 0 && result != nil {
			resultStr := windows.UTF16PtrToString(result)
			windows.CoTaskMemFree(unsafe.Pointer(result))

			var parsed struct {
				ProcessStatus struct {
					ExitCode int    `json:"ExitCode"`
					State    string `json:"State"`
				} `json:"ProcessStatus"`
			}
			if jsonErr := json.Unmarshal([]byte(resultStr), &parsed); jsonErr == nil {
				if parsed.ProcessStatus.State == "Exited" || parsed.ProcessStatus.State == "Terminated" {
					return parsed.ProcessStatus.ExitCode, nil
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return -1, fmt.Errorf("等待进程退出超时")
}

func ReadPipe(handle HCSHandle) ([]byte, error) {
	if handle == 0 {
		return nil, nil
	}

	var data []byte
	buf := make([]byte, 4096)

	for {
		var bytesRead uint32
		err := windows.ReadFile(
			windows.Handle(handle),
			buf,
			&bytesRead,
			nil,
		)
		if err != nil {
			if err == windows.ERROR_BROKEN_PIPE {
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

func CloseHandle(handle HCSHandle) {
	if handle != 0 {
		windows.CloseHandle(windows.Handle(handle))
	}
}

func CheckHCSAPI() error {
	if procHcsCreateComputeSystem == nil {
		return fmt.Errorf("HcsCreateComputeSystem 不可用，请确保 vmcompute.dll 已加载")
	}
	if procHcsStartComputeSystem == nil {
		return fmt.Errorf("HcsStartComputeSystem 不可用")
	}
	if procHcsTerminateComputeSystem == nil {
		return fmt.Errorf("HcsTerminateComputeSystem 不可用")
	}
	if procHcsCreateProcess == nil {
		return fmt.Errorf("HcsCreateProcess 不可用")
	}
	return nil
}

func HCSAPIStatus() map[string]bool {
	return map[string]bool{
		"HcsCreateComputeSystem":           procHcsCreateComputeSystem != nil,
		"HcsStartComputeSystem":            procHcsStartComputeSystem != nil,
		"HcsTerminateComputeSystem":        procHcsTerminateComputeSystem != nil,
		"HcsCreateProcess":                 procHcsCreateProcess != nil,
		"HcsCreateOperation":               procHcsCreateOperation != nil,
		"HcsCloseOperation":                procHcsCloseOperation != nil,
		"HcsWaitForOperationResult":        procHcsWaitForOperationResult != nil,
		"HcsWaitForProcessInComputeSystem": procHcsWaitForProcessInComputeSystem != nil,
		"HcsGetProcessProperties":          procHcsGetProcessProperties != nil,
	}
}

const waitTimeout = 30
