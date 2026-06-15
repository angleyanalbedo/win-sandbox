//! HCS (Host Compute System) API 绑定
//!
//! 这是 Windows Sandbox 和 Windows Container 底层使用的同一套 API。
//! 位于 vmcompute.dll，不属于公开 Win32 API，但微软的 hcsshim 项目
//! （Apache-2.0 开源）使用了这些接口，证明了它们的可用性。
//!
//! 使用运行时动态加载（LoadLibraryW + GetProcAddress），
//! 因为 vmcompute.dll 没有附带 .lib 导入库。
//!
//! 参考: https://github.com/microsoft/hcsshim

#![allow(non_snake_case, dead_code)]

use std::ffi::c_void;
use std::sync::LazyLock;
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

// ── 函数指针类型定义 ──

type FnHcsCreateComputeSystem = unsafe extern "system" fn(
    id: *const u16,
    config: *const u16,
    operation: HcsOperation,
    security_descriptor: *const c_void,
    system: *mut HcsSystem,
) -> i32;

type FnHcsStartComputeSystem = unsafe extern "system" fn(
    system: HcsSystem,
    operation: HcsOperation,
    options: *const u16,
) -> i32;

type FnHcsTerminateComputeSystem = unsafe extern "system" fn(
    system: HcsSystem,
    operation: HcsOperation,
    options: *const u16,
) -> i32;

type FnHcsPauseComputeSystem = unsafe extern "system" fn(
    system: HcsSystem,
    operation: HcsOperation,
    options: *const u16,
) -> i32;

type FnHcsResumeComputeSystem = unsafe extern "system" fn(
    system: HcsSystem,
    operation: HcsOperation,
    options: *const u16,
) -> i32;

type FnHcsGetComputeSystemProperties = unsafe extern "system" fn(
    system: HcsSystem,
    operation: HcsOperation,
    property_query: *const u16,
    properties: *mut *mut u16,
) -> i32;

type FnHcsExecuteProcess = unsafe extern "system" fn(
    system: HcsSystem,
    options: *const u16,
    operation: HcsOperation,
    process: *mut HcsProcess,
    std_in: *mut *mut c_void,
    std_out: *mut *mut c_void,
    std_err: *mut *mut c_void,
) -> i32;

type FnHcsWaitForProcessInComputeSystem = unsafe extern "system" fn(
    system: HcsSystem,
    process: HcsProcess,
    timeout_ms: u32,
    operation: HcsOperation,
    exit_info: *mut *mut u16,
) -> i32;

type FnHcsGetProcessProperties = unsafe extern "system" fn(
    process: HcsProcess,
    operation: HcsOperation,
    property_query: *const u16,
    properties: *mut *mut u16,
) -> i32;

type FnHcsCreateOperation =
    unsafe extern "system" fn(context: *mut c_void, callback: HcsEventCallback) -> HcsOperation;

type FnHcsCloseOperation = unsafe extern "system" fn(operation: HcsOperation);

type FnHcsWaitForOperationResult = unsafe extern "system" fn(
    operation: HcsOperation,
    timeout_ms: u32,
    result: *mut *mut u16,
) -> i32;

type FnHcsGetOperationContext = unsafe extern "system" fn(operation: HcsOperation) -> *mut c_void;

type FnCoTaskMemFree = unsafe extern "system" fn(ptr: *mut c_void);

// ── 动态加载 ──

/// vmcompute.dll 的函数表，运行时通过 LoadLibraryW + GetProcAddress 加载
struct HcsApi {
    _lib: windows::Win32::Foundation::HMODULE,
    HcsCreateComputeSystem: FnHcsCreateComputeSystem,
    HcsStartComputeSystem: FnHcsStartComputeSystem,
    HcsTerminateComputeSystem: FnHcsTerminateComputeSystem,
    HcsPauseComputeSystem: FnHcsPauseComputeSystem,
    HcsResumeComputeSystem: FnHcsResumeComputeSystem,
    HcsGetComputeSystemProperties: FnHcsGetComputeSystemProperties,
    HcsExecuteProcess: FnHcsExecuteProcess,
    HcsWaitForProcessInComputeSystem: FnHcsWaitForProcessInComputeSystem,
    HcsGetProcessProperties: FnHcsGetProcessProperties,
    HcsCreateOperation: FnHcsCreateOperation,
    HcsCloseOperation: FnHcsCloseOperation,
    HcsWaitForOperationResult: FnHcsWaitForOperationResult,
    HcsGetOperationContext: FnHcsGetOperationContext,
    CoTaskMemFree: FnCoTaskMemFree,
}

// 安全性：HMODULE 本质是一个 isize 句柄，函数指针是代码地址，
// 在进程生命周期内都有效，可以安全地跨线程共享。
unsafe impl Send for HcsApi {}
unsafe impl Sync for HcsApi {}

/// 从 DLL 中获取函数指针
unsafe fn get_proc<F: Copy>(
    lib: windows::Win32::Foundation::HMODULE,
    name: &str,
) -> anyhow::Result<F> {
    let name_c = std::ffi::CString::new(name)?;
    let addr = windows::Win32::System::LibraryLoader::GetProcAddress(
        lib,
        windows::core::PCSTR(name_c.as_ptr() as _),
    );
    match addr {
        Some(f) => Ok(std::mem::transmute_copy(&f)),
        None => anyhow::bail!("vmcompute.dll 缺少函数: {}", name),
    }
}

/// 加载 vmcompute.dll 并获取所有函数指针
fn load_hcs_api() -> anyhow::Result<HcsApi> {
    use windows::Win32::System::LibraryLoader::LoadLibraryW;

    let lib = unsafe { LoadLibraryW(windows::core::w!("vmcompute.dll")) }
        .map_err(|e| anyhow::anyhow!("无法加载 vmcompute.dll: {} (Hyper-V 可能未启用)", e))?;

    unsafe {
        Ok(HcsApi {
            _lib: lib,
            HcsCreateComputeSystem: get_proc(lib, "HcsCreateComputeSystem")?,
            HcsStartComputeSystem: get_proc(lib, "HcsStartComputeSystem")?,
            HcsTerminateComputeSystem: get_proc(lib, "HcsTerminateComputeSystem")?,
            HcsPauseComputeSystem: get_proc(lib, "HcsPauseComputeSystem")?,
            HcsResumeComputeSystem: get_proc(lib, "HcsResumeComputeSystem")?,
            HcsGetComputeSystemProperties: get_proc(lib, "HcsGetComputeSystemProperties")?,
            HcsExecuteProcess: get_proc(lib, "HcsExecuteProcess")?,
            HcsWaitForProcessInComputeSystem: get_proc(lib, "HcsWaitForProcessInComputeSystem")?,
            HcsGetProcessProperties: get_proc(lib, "HcsGetProcessProperties")?,
            HcsCreateOperation: get_proc(lib, "HcsCreateOperation")?,
            HcsCloseOperation: get_proc(lib, "HcsCloseOperation")?,
            HcsWaitForOperationResult: get_proc(lib, "HcsWaitForOperationResult")?,
            HcsGetOperationContext: get_proc(lib, "HcsGetOperationContext")?,
            CoTaskMemFree: get_proc(lib, "CoTaskMemFree")?,
        })
    }
}

/// 全局 API 实例（懒加载，只加载一次）
static HCS: LazyLock<anyhow::Result<HcsApi, String>> =
    LazyLock::new(|| load_hcs_api().map_err(|e| e.to_string()));

/// 获取 HCS API 实例
fn api() -> anyhow::Result<&'static HcsApi> {
    HCS.as_ref().map_err(|e| anyhow::anyhow!("{}", e))
}

// ── 公开的安全封装 ──

/// 创建一个新的计算系统（VM 或容器）
pub fn create_compute_system(id: &str, config: &str) -> anyhow::Result<(HcsSystem, HcsOperation)> {
    let api = api()?;
    let id_wide = to_wide(id);
    let config_wide = to_wide(config);
    let op = create_operation()?;
    let mut system: HcsSystem = std::ptr::null_mut();

    let hr = unsafe {
        (api.HcsCreateComputeSystem)(
            id_wide.as_ptr(),
            config_wide.as_ptr(),
            op,
            std::ptr::null(),
            &mut system,
        )
    };

    if hr != 0 {
        unsafe { (api.HcsCloseOperation)(op) };
        anyhow::bail!("HcsCreateComputeSystem 失败: HRESULT 0x{:08X}", hr as u32);
    }

    let wait_hr = wait_operation(op, 30_000)?;
    if wait_hr != 0 {
        unsafe { (api.HcsCloseOperation)(op) };
        anyhow::bail!("等待系统创建完成失败: HRESULT 0x{:08X}", wait_hr as u32);
    }

    Ok((system, op))
}

/// 启动一个已创建的计算系统
pub fn start_compute_system(system: HcsSystem) -> anyhow::Result<()> {
    let api = api()?;
    let start_op = create_operation()?;
    let options = to_wide("{}");

    let hr = unsafe { (api.HcsStartComputeSystem)(system, start_op, options.as_ptr()) };

    if hr != 0 {
        unsafe { (api.HcsCloseOperation)(start_op) };
        anyhow::bail!("HcsStartComputeSystem 失败: HRESULT 0x{:08X}", hr as u32);
    }

    let wait_hr = wait_operation(start_op, 60_000)?;
    unsafe { (api.HcsCloseOperation)(start_op) };

    if wait_hr != 0 {
        anyhow::bail!("等待系统启动完成失败: HRESULT 0x{:08X}", wait_hr as u32);
    }

    Ok(())
}

/// 在计算系统内执行一个进程
pub fn execute_process(
    system: HcsSystem,
    process_config: &str,
) -> anyhow::Result<(HcsProcess, *mut c_void, *mut c_void, *mut c_void)> {
    let api = api()?;
    let proc_wide = to_wide(process_config);
    let op = create_operation()?;

    let mut process: HcsProcess = std::ptr::null_mut();
    let mut std_in: *mut c_void = std::ptr::null_mut();
    let mut std_out: *mut c_void = std::ptr::null_mut();
    let mut std_err: *mut c_void = std::ptr::null_mut();

    let hr = unsafe {
        (api.HcsExecuteProcess)(
            system,
            proc_wide.as_ptr(),
            op,
            &mut process,
            &mut std_in,
            &mut std_out,
            &mut std_err,
        )
    };

    if hr != 0 {
        unsafe { (api.HcsCloseOperation)(op) };
        anyhow::bail!("HcsExecuteProcess 失败: 0x{:08X}", hr as u32);
    }

    let wait_hr = wait_operation(op, 10_000)?;
    unsafe { (api.HcsCloseOperation)(op) };

    if wait_hr != 0 {
        anyhow::bail!("等待进程启动失败: 0x{:08X}", wait_hr as u32);
    }

    Ok((process, std_in, std_out, std_err))
}

/// 等待进程结束并获取退出信息
pub fn wait_for_process(
    system: HcsSystem,
    process: HcsProcess,
    timeout_ms: u32,
) -> anyhow::Result<String> {
    let api = api()?;
    let wait_op = create_operation()?;
    let mut exit_info_ptr: *mut u16 = std::ptr::null_mut();

    let hr = unsafe {
        (api.HcsWaitForProcessInComputeSystem)(
            system,
            process,
            timeout_ms,
            wait_op,
            &mut exit_info_ptr,
        )
    };

    if hr != 0 {
        unsafe { (api.HcsCloseOperation)(wait_op) };
        anyhow::bail!("HcsWaitForProcessInComputeSystem 失败: 0x{:08X}", hr as u32);
    }

    let exit_json = if !exit_info_ptr.is_null() {
        let s = unsafe { u16_ptr_to_string(exit_info_ptr) };
        unsafe { (api.CoTaskMemFree)(exit_info_ptr as *mut c_void) };
        s
    } else {
        String::new()
    };

    unsafe { (api.HcsCloseOperation)(wait_op) };
    Ok(exit_json)
}

/// 销毁计算系统
pub fn terminate_compute_system(system: HcsSystem) -> anyhow::Result<()> {
    let api = api()?;
    let op = create_operation()?;
    let options = to_wide("{}");

    let hr = unsafe { (api.HcsTerminateComputeSystem)(system, op, options.as_ptr()) };

    if hr != 0 {
        log::warn!("HcsTerminateComputeSystem 返回: 0x{:08X}", hr as u32);
    }

    let _ = wait_operation(op, 30_000);
    unsafe { (api.HcsCloseOperation)(op) };

    Ok(())
}

/// 创建操作句柄
pub fn create_operation() -> anyhow::Result<HcsOperation> {
    let api = api()?;
    let op = unsafe { (api.HcsCreateOperation)(std::ptr::null_mut(), None) };
    if op.is_null() {
        anyhow::bail!("HcsCreateOperation 失败");
    }
    Ok(op)
}

/// 等待操作完成并返回 HRESULT
pub fn wait_operation(op: HcsOperation, timeout_ms: u32) -> anyhow::Result<i32> {
    let api = api()?;
    let mut result_ptr: *mut u16 = std::ptr::null_mut();
    let hr = unsafe { (api.HcsWaitForOperationResult)(op, timeout_ms, &mut result_ptr) };
    if !result_ptr.is_null() {
        let result_str = unsafe { u16_ptr_to_string(result_ptr) };
        log::debug!("操作结果: {}", result_str);
        unsafe { (api.CoTaskMemFree)(result_ptr as *mut c_void) };
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

/// 释放 HCS 分配的内存
pub unsafe fn co_task_mem_free(ptr: *mut c_void) {
    if let Ok(api) = api() {
        (api.CoTaskMemFree)(ptr);
    }
}

/// 检查 vmcompute.dll 是否可用
pub fn is_available() -> bool {
    api().is_ok()
}
