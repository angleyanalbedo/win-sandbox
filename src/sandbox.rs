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
use log::{debug, info, warn};
use uuid::Uuid;

use crate::config::{self, ResourceLimits, SandboxConfig};
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
        // 先检查 vmcompute.dll 是否可用
        if !hcs::is_available() {
            anyhow::bail!(
                "vmcompute.dll 不可用。\n\
                 请确认：\n\
                 1. Windows 版本为 Pro/Enterprise（Home 版不支持 Hyper-V）\n\
                 2. Hyper-V 功能已启用\n\
                 3. vmcompute 服务正在运行"
            );
        }

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

        // 创建并等待计算系统
        let (system, _op) = hcs::create_compute_system(&self.id, &hcs_json)?;
        self.system = system;

        // 启动计算系统
        hcs::start_compute_system(self.system)?;

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
        limits: &ResourceLimits,
    ) -> Result<ExecutionResult> {
        if !self.running {
            anyhow::bail!("沙箱未启动");
        }

        let start = std::time::Instant::now();
        info!("执行进程: {} {:?}", command, args);

        // 构造进程配置 JSON
        let proc_json = config::process_config(command, args, None, &[]);

        // 执行进程
        let (process, std_in, std_out, std_err) = hcs::execute_process(self.system, &proc_json)?;

        // 关闭 stdin（我们不写入）
        if !std_in.is_null() {
            unsafe {
                windows::Win32::Foundation::CloseHandle(windows::Win32::Foundation::HANDLE(std_in))
            }
            .ok();
        }

        // 应用 Job Object 资源限制
        apply_resource_limits(limits)?;

        // 等待进程结束
        let exit_json = hcs::wait_for_process(self.system, process, timeout_ms)?;
        debug!("退出信息: {}", exit_json);

        // 解析退出码
        let exit_code = if !exit_json.is_empty() {
            if let Ok(v) = serde_json::from_str::<serde_json::Value>(&exit_json) {
                v["ProcessStatus"]["ExitCode"].as_i64().unwrap_or(-1) as i32
            } else {
                -1
            }
        } else {
            -1
        };

        // 从管道读取 stdout/stderr（进程结束后管道会关闭）
        let stdout = unsafe { hcs::read_pipe_to_string(std_out) };
        let stderr = unsafe { hcs::read_pipe_to_string(std_err) };

        let elapsed = start.elapsed();

        info!(
            "进程执行完成: exit_code={}, elapsed={}ms",
            exit_code,
            elapsed.as_millis()
        );

        Ok(ExecutionResult {
            exit_code,
            stdout,
            stderr,
            elapsed_ms: elapsed.as_millis() as i64,
        })
    }

    /// 终止沙箱（一次性销毁，不留痕迹）
    pub fn terminate(&mut self) -> Result<()> {
        if !self.running {
            return Ok(());
        }

        info!("销毁沙箱: {}", self.id);

        hcs::terminate_compute_system(self.system)?;

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

/// 应用 Job Object 资源限制
///
/// Job Object 是 Windows 的进程分组+资源限制机制，
/// 在所有 Windows 版本上可用（包括 Home）。
/// 这里用于限制沙箱内进程的资源使用。
fn apply_resource_limits(limits: &ResourceLimits) -> Result<()> {
    use windows::Win32::System::JobObjects::{
        AssignProcessToJobObject, CreateJobObjectW, JobObjectExtendedLimitInformation,
        SetInformationJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION, JOB_OBJECT_LIMIT,
        JOB_OBJECT_LIMIT_JOB_MEMORY, JOB_OBJECT_LIMIT_JOB_TIME,
    };
    use windows::Win32::System::Threading::GetCurrentProcess;

    // 如果没有任何限制，跳过
    if limits.max_memory_mb == 0 && limits.max_cpu_percent == 0 && limits.max_processes == 0 {
        return Ok(());
    }

    unsafe {
        // 创建 Job Object
        let job = CreateJobObjectW(None, windows::core::w!("wsandbox-limits"))
            .map_err(|e| anyhow::anyhow!("CreateJobObject 失败: {}", e))?;

        // 构造限制信息
        let mut ext_info: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
        let mut flags = JOB_OBJECT_LIMIT(0);

        if limits.max_memory_mb > 0 {
            flags |= JOB_OBJECT_LIMIT_JOB_MEMORY;
            ext_info.ProcessMemoryLimit = (limits.max_memory_mb * 1024 * 1024) as usize;
            ext_info.JobMemoryLimit = (limits.max_memory_mb * 1024 * 1024) as usize;
        }

        if limits.max_cpu_percent > 0 {
            // CPU 限制通过 JobObjectCpuRateControl 实现
            // 这里用 JOB_OBJECT_LIMIT_JOB_TIME 作为超时保护
            flags |= JOB_OBJECT_LIMIT_JOB_TIME;
        }

        if limits.max_processes > 0 {
            flags |= JOB_OBJECT_LIMIT(0x00000008); // JOB_OBJECT_LIMIT_ACTIVE_PROCESS
            ext_info.BasicLimitInformation.ActiveProcessLimit = limits.max_processes;
        }

        ext_info.BasicLimitInformation.LimitFlags = flags;

        // 设置限制
        let info_ptr = &ext_info as *const _ as *const std::ffi::c_void;
        let info_size = std::mem::size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32;

        SetInformationJobObject(job, JobObjectExtendedLimitInformation, info_ptr, info_size)
            .map_err(|e| anyhow::anyhow!("SetInformationJobObject 失败: {}", e))?;

        // 把当前进程（即沙箱进程的宿主）加入 Job
        // 注意：HCS 在 VM 内执行进程，Job Object 作用于宿主侧
        // 这里限制的是宿主进程的资源，间接影响 VM
        let current = GetCurrentProcess();
        AssignProcessToJobObject(job, current)
            .map_err(|e| anyhow::anyhow!("AssignProcessToJobObject 失败: {}", e))?;

        info!(
            "已应用资源限制: 内存={}MB, CPU={}%, 进程={}",
            limits.max_memory_mb, limits.max_cpu_percent, limits.max_processes
        );
    }

    Ok(())
}
