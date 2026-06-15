//! HCS (Host Compute System) API 绑定
//!
//! 这是 Windows Sandbox 和 Windows Container 底层使用的同一套 API。
//! 位于 vmcompute.dll，不属于公开 Win32 API，但微软的 hcsshim 项目
//! （Apache-2.0 开源）使用了这些接口，证明了它们的可用性。
//!
//! 参考: https://github.com/microsoft/hcsshim

#![allow(non_snake_case, dead_code)]

use std::ffi::c_void;
use widestring::U16CString;

// ── 类型定义 ──

/// HCS 系统句柄（代表一个 VM 或容器）
pub type HcsSystem = *mut c_void;

/// HCS 进程句柄（代表 VM 内的一个进程）
pub type HcsProcess = *mut c_void;

/// 操作句柄（异步操作）
pub type HcsOperation = *mut c_void;

/// 回调函数类型
pub type HcsEventCallback =
    Option<unsafe extern "system" fn(event_type: u32, context: *mut c_void, context2: *mut c_void)>;

// ── 常量 ──

/// 操作完成事件
pub const HCS_EVENT_OPERATION_COMPLETED: u32 = 3;

/// 进程信息类型
pub const HCS_PROCESS_STATISTICS: u32 = 0;

// ── vmcompute.dll 函数绑定 ──

#[link(name = "vmcompute")]
extern "system" {

    // ═══════════════════════════════════════════
    //  系统（VM/容器）管理
    // ═══════════════════════════════════════════

    /// 创建一个新的计算系统（VM 或容器）
    ///
    /// # 参数
    /// - id: 系统唯一标识 (GUID 字符串)
    /// - config: JSON 格式的配置（定义 VM/容器的属性）
    /// - operation: 异步操作句柄
    /// - system: 输出 - 创建的系统句柄
    pub fn HcsCreateComputeSystem(
        id: *const u16,
        config: *const u16,
        operation: HcsOperation,
        security_descriptor: *const c_void,
        system: *mut HcsSystem,
    ) -> i32; // HRESULT

    /// 启动一个已创建的计算系统
    pub fn HcsStartComputeSystem(
        system: HcsSystem,
        operation: HcsOperation,
        options: *const u16, // JSON 启动选项
    ) -> i32;

    /// 关闭/销毁一个计算系统
    pub fn HcsTerminateComputeSystem(
        system: HcsSystem,
        operation: HcsOperation,
        options: *const u16,
    ) -> i32;

    /// 暂停计算系统
    pub fn HcsPauseComputeSystem(
        system: HcsSystem,
        operation: HcsOperation,
        options: *const u16,
    ) -> i32;

    /// 恢复暂停的计算系统
    pub fn HcsResumeComputeSystem(
        system: HcsSystem,
        operation: HcsOperation,
        options: *const u16,
    ) -> i32;

    /// 获取系统属性（内存使用、CPU 时间等）
    pub fn HcsGetComputeSystemProperties(
        system: HcsSystem,
        operation: HcsOperation,
        property_query: *const u16, // JSON 查询
        properties: *mut *mut u16,  // JSON 输出
    ) -> i32;

    // ═══════════════════════════════════════════
    //  进程管理（在 VM/容器内执行程序）
    // ═══════════════════════════════════════════

    /// 在计算系统内执行一个进程
    ///
    /// # 参数
    /// - system: 系统句柄
    /// - options: JSON 格式的进程配置（命令行、环境变量、工作目录）
    /// - operation: 异步操作句柄
    /// - process: 输出 - 进程句柄
    /// - std_in/std_out/std_err: 标准 I/O 管道句柄
    pub fn HcsExecuteProcess(
        system: HcsSystem,
        options: *const u16,
        operation: HcsOperation,
        process: *mut HcsProcess,
        std_in: *mut *mut c_void,
        std_out: *mut *mut c_void,
        std_err: *mut *mut c_void,
    ) -> i32;

    /// 等待进程结束
    pub fn HcsWaitForProcessInComputeSystem(
        system: HcsSystem,
        process: HcsProcess,
        timeout_ms: u32,
        operation: HcsOperation,
        exit_info: *mut *mut u16, // JSON 输出
    ) -> i32;

    /// 获取进程属性
    pub fn HcsGetProcessProperties(
        process: HcsProcess,
        operation: HcsOperation,
        property_query: *const u16,
        properties: *mut *mut u16,
    ) -> i32;

    // ═══════════════════════════════════════════
    //  操作管理（异步操作控制）
    // ═══════════════════════════════════════════

    /// 创建一个异步操作
    pub fn HcsCreateOperation(
        context: *mut c_void,
        callback: HcsEventCallback,
    ) -> HcsOperation;

    /// 关闭一个操作
    pub fn HcsCloseOperation(operation: HcsOperation);

    /// 等待操作完成
    pub fn HcsWaitForOperationResult(
        operation: HcsOperation,
        timeout_ms: u32,
        result: *mut *mut u16, // JSON 错误信息
    ) -> i32;

    /// 获取操作的上下文
    pub fn HcsGetOperationContext(operation: HcsOperation) -> *mut c_void;
}

// ── 安全封装 ──

/// 创建操作句柄（安全封装）
pub fn create_operation() -> anyhow::Result<HcsOperation> {
    let op = unsafe { HcsCreateOperation(std::ptr::null_mut(), None) };
    if op.is_null() {
        anyhow::bail!("HcsCreateOperation 失败");
    }
    Ok(op)
}

/// 等待操作完成并返回 HRESULT
pub fn wait_operation(op: HcsOperation, timeout_ms: u32) -> anyhow::Result<i32> {
    let mut result_ptr: *mut u16 = std::ptr::null_mut();
    let hr = unsafe { HcsWaitForOperationResult(op, timeout_ms, &mut result_ptr) };
    if !result_ptr.is_null() {
        // 有错误信息
        let result_str = unsafe { u16_ptr_to_string(result_ptr) };
        log::debug!("操作结果: {}", result_str);
        // 释放 HCS 分配的字符串
        unsafe { CoTaskMemFree(result_ptr as *mut c_void) };
    }
    Ok(hr)
}

/// 将 UTF-16 指针转为 Rust String
pub unsafe fn u16_ptr_to_string(ptr: *const u16) -> String {
    if ptr.is_null() {
        return String::new();
    }
    let mut len = 0;
    while *ptr.add(len) != 0 {
        len += 1;
    }
    let slice = std::slice::from_raw_parts(ptr, len);
    String::from_utf16_lossy(slice)
}

/// Rust String → UTF-16 C 字符串
pub fn to_wide(s: &str) -> U16CString {
    U16CString::from_str(s).expect("UTF-16 转换失败")
}

extern "system" {
    fn CoTaskMemFree(ptr: *mut c_void);
}
