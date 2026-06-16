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

/// HCS API 函数表，运行时从 vmcompute.dll + computecore.dll 动态加载
///
/// 不同 Windows 版本的 DLL 布局不同，部分函数可能不存在。
/// 可选函数用 Option 标记，必选函数加载失败则报错。
struct HcsApi {
    _lib: windows::Win32::Foundation::HMODULE,
    HcsCreateComputeSystem: FnHcsCreateComputeSystem,
    HcsStartComputeSystem: FnHcsStartComputeSystem,
    HcsTerminateComputeSystem: FnHcsTerminateComputeSystem,
    HcsPauseComputeSystem: Option<FnHcsPauseComputeSystem>,
    HcsResumeComputeSystem: Option<FnHcsResumeComputeSystem>,
    HcsGetComputeSystemProperties: FnHcsGetComputeSystemProperties,
    HcsExecuteProcess: FnHcsExecuteProcess,
    HcsWaitForProcessInComputeSystem: FnHcsWaitForProcessInComputeSystem,
    HcsGetProcessProperties: FnHcsGetProcessProperties,
    HcsCreateOperation: FnHcsCreateOperation,
    HcsCloseOperation: FnHcsCloseOperation,
    HcsWaitForOperationResult: FnHcsWaitForOperationResult,
    HcsGetOperationContext: Option<FnHcsGetOperationContext>,
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
        None => anyhow::bail!("缺少函数: {}", name),
    }
}

/// 从多个 DLL 中尝试获取函数指针
unsafe fn get_proc_from_any<F: Copy>(
    libs: &[windows::Win32::Foundation::HMODULE],
    name: &str,
) -> anyhow::Result<F> {
    for lib in libs {
        let name_c = std::ffi::CString::new(name)?;
        let addr = windows::Win32::System::LibraryLoader::GetProcAddress(
            *lib,
            windows::core::PCSTR(name_c.as_ptr() as _),
        );
        if let Some(f) = addr {
            return Ok(std::mem::transmute_copy(&f));
        }
    }
    anyhow::bail!("所有 DLL 中均缺少函数: {}", name)
}

/// 加载 HCS API 函数指针（兼容新旧版本 DLL 布局）
///
/// Windows 10/11 不同版本的 HCS 函数分布在不同 DLL 中：
/// - vmcompute.dll: 核心 VM/容器管理（旧版也有全部函数）
/// - computecore.dll: 新版拆分出的操作和进程函数
///
/// 这里从两个 DLL 中搜索，找到就用，兼容新旧系统。
fn load_hcs_api() -> anyhow::Result<HcsApi> {
    use windows::Win32::Foundation::GetLastError;
    use windows::Win32::System::LibraryLoader::LoadLibraryW;

    let lib1 = unsafe { LoadLibraryW(windows::core::w!("C:\\Windows\\System32\\vmcompute.dll")) }
        .map_err(|e| {
            let err_code = unsafe { GetLastError() };
            anyhow::anyhow!(
                "无法加载 vmcompute.dll: {} (错误码: 0x{:08X})\n  请确认 Hyper-V 已启用且以管理员权限运行",
                e, err_code.0
            )
        })?;

    // computecore.dll 可能不存在于旧系统，允许失败
    let lib2 = unsafe { LoadLibraryW(windows::core::w!("C:\\Windows\\System32\\computecore.dll")) };
    let _has_lib2 = lib2.is_ok();

    // ole32.dll 提供 CoTaskMemFree
    let lib_ole32 = unsafe { LoadLibraryW(windows::core::w!("ole32.dll")) };

    // 收集所有可用的 DLL 句柄用于搜索函数
    let mut libs = vec![lib1];
    if let Ok(l) = lib2 {
        libs.push(l);
    }
    if let Ok(l) = lib_ole32 {
        libs.push(l);
    }

    unsafe {
        Ok(HcsApi {
            _lib: lib1,
            HcsCreateComputeSystem: get_proc_from_any(&libs, "HcsCreateComputeSystem")?,
            HcsStartComputeSystem: get_proc_from_any(&libs, "HcsStartComputeSystem")?,
            HcsTerminateComputeSystem: get_proc_from_any(&libs, "HcsTerminateComputeSystem")?,
            HcsPauseComputeSystem: get_proc_from_any(&libs, "HcsPauseComputeSystem").ok(),
            HcsResumeComputeSystem: get_proc_from_any(&libs, "HcsResumeComputeSystem").ok(),
            HcsGetComputeSystemProperties: get_proc_from_any(&libs, "HcsGetComputeSystemProperties")?,
            // 新版 API: HcsCreateProcess 替代 HcsExecuteProcess
            HcsExecuteProcess: get_proc_from_any(&libs, "HcsExecuteProcess")
                .or_else(|_| get_proc_from_any(&libs, "HcsCreateProcess"))
                .map_err(|_| anyhow::anyhow!("找不到 HcsExecuteProcess 或 HcsCreateProcess"))?,
            // 新版 API: HcsOpenProcess 替代 HcsWaitForProcessInComputeSystem
            HcsWaitForProcessInComputeSystem: get_proc_from_any(&libs, "HcsWaitForProcessInComputeSystem")
                .or_else(|_| get_proc_from_any(&libs, "HcsOpenProcess"))
                .map_err(|_| anyhow::anyhow!("找不到 HcsWaitForProcessInComputeSystem 或 HcsOpenProcess"))?,
            HcsGetProcessProperties: get_proc_from_any(&libs, "HcsGetProcessProperties")?,
            HcsCreateOperation: get_proc_from_any(&libs, "HcsCreateOperation")?,
            HcsCloseOperation: get_proc_from_any(&libs, "HcsCloseOperation")?,
            HcsWaitForOperationResult: get_proc_from_any(&libs, "HcsWaitForOperationResult")?,
            HcsGetOperationContext: get_proc_from_any(&libs, "HcsGetOperationContext").ok(),
            CoTaskMemFree: get_proc_from_any(&libs, "CoTaskMemFree")?,
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

/// 获取详细的加载错误信息（用于诊断）
pub fn load_error_detail() -> String {
    match api() {
        Ok(_) => String::new(),
        Err(e) => {
            let vmcompute = std::path::Path::new(r"C:\Windows\System32\vmcompute.dll");
            let computecore = std::path::Path::new(r"C:\Windows\System32\computecore.dll");
            format!(
                "{}\n  vmcompute.dll 存在: {} ({} bytes)\n  computecore.dll 存在: {}\n  进程架构: {}bit",
                e,
                vmcompute.exists(),
                if vmcompute.exists() { std::fs::metadata(vmcompute).map(|m| m.len()).unwrap_or(0) } else { 0 },
                computecore.exists(),
                std::mem::size_of::<usize>() * 8
            )
        }
    }
}

// ── Windows 管道 I/O ──

/// 从管道句柄读取所有数据（阻塞直到管道关闭）
///
/// # Safety
/// `handle` 必须是有效的 Windows 管道句柄
pub unsafe fn read_pipe_to_string(handle: *mut c_void) -> String {
    use windows::Win32::Foundation::{CloseHandle, HANDLE};
    use windows::Win32::Storage::FileSystem::ReadFile;

    if handle.is_null() {
        return String::new();
    }

    let h = HANDLE(handle);

    let mut output = Vec::new();
    let mut buf = [0u8; 4096];
    let mut bytes_read: u32 = 0;

    loop {
        let ok = ReadFile(h, Some(&mut buf), Some(&mut bytes_read), None);

        match ok {
            Ok(()) if bytes_read == 0 => break, // EOF
            Ok(()) => output.extend_from_slice(&buf[..bytes_read as usize]),
            Err(_) => break,
        }
    }

    let _ = CloseHandle(h);

    String::from_utf8_lossy(&output).to_string()
}
