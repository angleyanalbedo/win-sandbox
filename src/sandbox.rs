//! 沙箱生命周期管理
//!
//! 封装 HCS API 的完整工作流：
//! 创建 → 启动 → 执行进程 → 收集结果 → 销毁
//!
//! 这与 Windows Sandbox 内部的工作流程一致：
//! 1. HcsCreateComputeSystem  → 创建轻量 VM
//! 2. HcsStartComputeSystem   → 启动 VM
//! 3. HcsExecuteProcess       → 在 VM 内执行 exe
//! 4. HcsWaitForProcess       → 等待执行完成
//! 5. HcsTerminateComputeSystem → 销毁 VM（一次性）

use anyhow::Result;
use log::{debug, error, info, warn};
use uuid::Uuid;

use crate::config::{self, SandboxConfig, SandboxType};
use crate::hcs;

/// 沙箱实例
pub struct Sandbox {
    /// 唯一标识
    id: String,
    /// 配置
    config: SandboxConfig,
    /// HCS 系统句柄
    system: hcs::HcsSystem,
    /// 是否已启动
    running: bool,
}

/// 进程执行结果
#[derive(Debug)]
pub struct ExecutionResult {
    pub exit_code: i32,
    pub stdout: String,
    pub stderr: String,
    pub elapsed_ms: i64,
}

impl Sandbox {
    /// 创建一个新的沙箱实例
    pub fn new(config: SandboxConfig) -> Result<Self> {
        let id = format!("sandbox-{}", Uuid::new_v4());
        info!("创建沙箱: {} ({:?})", id, config.sandbox_type);

        // 确保工作目录存在
        std::fs::create_dir_all(&config.workspace_path)?;

        Ok(Self {
            id,
            config,
            system: std::ptr::null_mut(),
            running: false,
        })
    }

    /// 启动沙箱
    pub fn start(&mut self) -> Result<()> {
        info!("启动沙箱: {}", self.id);

        let hcs_json = self.config.to_hcs_json();
        debug!("HCS 配置:\n{}", hcs_json);

        let id_wide = hcs::to_wide(&self.id);
        let config_wide = hcs::to_wide(&hcs_json);

        // 1. 创建异步操作
        let op = hcs::create_operation()?;

        // 2. 创建计算系统
        let hr = unsafe {
            hcs::HcsCreateComputeSystem(
                id_wide.as_ptr(),
                config_wide.as_ptr(),
                op,
                std::ptr::null(),
                &mut self.system,
            )
        };

        if hr != 0 {
            unsafe { hcs::HcsCloseOperation(op) };
            anyhow::bail!("HcsCreateComputeSystem 失败: HRESULT 0x{:08X}", hr as u32);
        }

        // 3. 等待创建完成
        let wait_hr = hcs::wait_operation(op, 30_000)?;
        unsafe { hcs::HcsCloseOperation(op) };

        if wait_hr != 0 {
            anyhow::bail!(
                "等待系统创建完成失败: HRESULT 0x{:08X}",
                wait_hr as u32
            );
        }

        // 4. 启动系统
        let start_op = hcs::create_operation()?;
        let start_options = hcs::to_wide("{}");

        let hr = unsafe {
            hcs::HcsStartComputeSystem(
                self.system,
                start_op,
                start_options.as_ptr(),
            )
        };

        if hr != 0 {
            unsafe { hcs::HcsCloseOperation(start_op) };
            anyhow::bail!("HcsStartComputeSystem 失败: HRESULT 0x{:08X}", hr as u32);
        }

        // 5. 等待启动完成
        let wait_hr = hcs::wait_operation(start_op, 60_000)?;
        unsafe { hcs::HcsCloseOperation(start_op) };

        if wait_hr != 0 {
            anyhow::bail!("等待系统启动完成失败");
        }

        self.running = true;
        info!("沙箱启动成功: {}", self.id);
        Ok(())
    }

    /// 在沙箱内执行一个进程
    pub fn execute(
        &mut self,
        command: &str,
        args: &[&str],
        timeout_ms: u32,
    ) -> Result<ExecutionResult> {
        if !self.running {
            anyhow::bail!("沙箱未启动");
        }

        let start = std::time::Instant::now();
        info!("执行进程: {} {:?}", command, args);

        // 1. 构造进程配置 JSON
        let proc_json = config::process_config(command, args, None, &[]);
        let proc_wide = hcs::to_wide(&proc_json);

        // 2. 创建操作
        let op = hcs::create_operation()?;

        // 3. 执行进程
        let mut process: hcs::HcsProcess = std::ptr::null_mut();
        let mut std_in: *mut std::ffi::c_void = std::ptr::null_mut();
        let mut std_out: *mut std::ffi::c_void = std::ptr::null_mut();
        let mut std_err: *mut std::ffi::c_void = std::ptr::null_mut();

        let hr = unsafe {
            hcs::HcsExecuteProcess(
                self.system,
                proc_wide.as_ptr(),
                op,
                &mut process,
                &mut std_in,
                &mut std_out,
                &mut std_err,
            )
        };

        if hr != 0 {
            unsafe { hcs::HcsCloseOperation(op) };
            anyhow::bail!("HcsExecuteProcess 失败: 0x{:08X}", hr as u32);
        }

        // 4. 等待进程执行启动完成
        let wait_hr = hcs::wait_operation(op, 10_000)?;
        unsafe { hcs::HcsCloseOperation(op) };

        if wait_hr != 0 {
            anyhow::bail!("等待进程启动失败");
        }

        // 5. 等待进程结束
        let wait_op = hcs::create_operation()?;
        let mut exit_info_ptr: *mut u16 = std::ptr::null_mut();

        let hr = unsafe {
            hcs::HcsWaitForProcessInComputeSystem(
                self.system,
                process,
                timeout_ms,
                wait_op,
                &mut exit_info_ptr,
            )
        };

        // 6. 解析退出信息
        let exit_code = if !exit_info_ptr.is_null() {
            let exit_json = unsafe { hcs::u16_ptr_to_string(exit_info_ptr) };
            debug!("退出信息: {}", exit_json);

            // 解析 JSON 获取退出码
            if let Ok(v) = serde_json::from_str::<serde_json::Value>(&exit_json) {
                v["ProcessStatus"]["ExitCode"]
                    .as_i64()
                    .unwrap_or(-1) as i32
            } else {
                -1
            }
        } else {
            -1
        };

        unsafe {
            hcs::HcsCloseOperation(wait_op);
            if !exit_info_ptr.is_null() {
                crate::hcs::CoTaskMemFree(exit_info_ptr as *mut std::ffi::c_void);
            }
        }

        let elapsed = start.elapsed();

        info!(
            "进程执行完成: exit_code={}, elapsed={}ms",
            exit_code,
            elapsed.as_millis()
        );

        Ok(ExecutionResult {
            exit_code,
            stdout: String::new(), // TODO: 从 std_out 管道读取
            stderr: String::new(), // TODO: 从 std_err 管道读取
            elapsed_ms: elapsed.as_millis() as i64,
        })
    }

    /// 终止沙箱（一次性销毁，不留痕迹）
    pub fn terminate(&mut self) -> Result<()> {
        if !self.running {
            return Ok(());
        }

        info!("销毁沙箱: {}", self.id);

        let op = hcs::create_operation()?;
        let options = hcs::to_wide("{}");

        let hr = unsafe {
            hcs::HcsTerminateComputeSystem(
                self.system,
                op,
                options.as_ptr(),
            )
        };

        if hr != 0 {
            warn!("HcsTerminateComputeSystem 返回: 0x{:08X}", hr as u32);
        }

        // 等待销毁完成
        let _ = hcs::wait_operation(op, 30_000);
        unsafe { hcs::HcsCloseOperation(op) };

        self.running = false;
        self.system = std::ptr::null_mut();

        // 清理工作目录中的临时文件（差分磁盘等）
        if let Err(e) = std::fs::remove_dir_all(&self.config.workspace_path) {
            warn!("清理工作目录失败: {}", e);
        }

        info!("沙箱已销毁: {}", self.id);
        Ok(())
    }
}

impl Drop for Sandbox {
    fn drop(&mut self) {
        if self.running {
            let _ = self.terminate();
        }
    }
}
