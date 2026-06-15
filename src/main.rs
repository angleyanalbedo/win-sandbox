//! win-sandbox — Windows Sandbox 的开源等价实现
//!
//! 使用 Windows HCS (Host Compute System) API 创建轻量 VM，
//! 在 VM 内安全执行 .exe，完成后一次性销毁。
//!
//! 这就是 Windows Sandbox 内部使用的同一套 API。
//!
//! 前提条件:
//!   - Windows 10/11 Pro 或 Enterprise
//!   - Hyper-V 功能已启用
//!   - 以管理员权限运行

#![cfg_attr(not(windows), allow(unused))]

mod config;
mod hcs;
mod sandbox;

use anyhow::Result;
use clap::{Parser, Subcommand};
use std::path::PathBuf;

#[derive(Parser)]
#[command(
    name = "wsandbox-vm",
    version = "0.1.0",
    about = "Windows Sandbox 开源实现 — 基于 Hyper-V HCS API"
)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// 在轻量 VM 中执行一个 .exe 文件
    Run {
        /// .exe 文件路径
        exe: PathBuf,

        /// 传递给 exe 的参数
        #[arg(trailing_var_arg = true)]
        args: Vec<String>,

        /// VM 内存大小 (MB)
        #[arg(short, long, default_value = "512")]
        memory: u64,

        /// CPU 核心数
        #[arg(short, long, default_value = "2")]
        cpus: u32,

        /// 执行超时（秒）
        #[arg(short, long, default_value = "60")]
        timeout: u64,

        /// 启用网络
        #[arg(long)]
        network: bool,

        /// 允许访问的域名（可多次指定，仅 --network 时生效）
        #[arg(long = "allow-domain")]
        allowed_domains: Vec<String>,

        /// 共享目录 (host_path=guest_path，只读用 host_path=guest_path=ro)
        #[arg(short, long)]
        share: Vec<String>,

        /// 沙箱类型
        #[arg(long, default_value = "hyperv", value_parser = ["hyperv", "container", "linux"])]
        sandbox_type: String,

        /// 启用差分磁盘（共享 Host 基础镜像，只存改动）
        #[arg(long)]
        diff_disk: bool,

        /// 差分磁盘基础镜像路径（为空则自动检测）
        #[arg(long)]
        diff_disk_base: Option<String>,

        /// 最大内存限制 (MB)，超出进程会被 kill，0 = 不限制
        #[arg(long)]
        max_memory: Option<u64>,

        /// 最大 CPU 时间百分比 (1-100)，0 = 不限制
        #[arg(long)]
        max_cpu: Option<u32>,

        /// 最大子进程数，0 = 不限制
        #[arg(long)]
        max_processes: Option<u32>,

        /// 输出 JSON
        #[arg(long)]
        json: bool,

        /// 详细输出
        #[arg(short, long)]
        verbose: bool,
    },

    /// 显示 HCS 配置（不执行，用于调试）
    ShowConfig {
        /// 沙箱类型
        #[arg(long, default_value = "hyperv", value_parser = ["hyperv", "container", "linux"])]
        sandbox_type: String,

        /// 内存 (MB)
        #[arg(short, long, default_value = "512")]
        memory: u64,

        /// CPU 核心数
        #[arg(long, default_value = "2")]
        cpus: u32,

        /// 启用网络
        #[arg(long)]
        network: bool,

        /// 启用差分磁盘
        #[arg(long)]
        diff_disk: bool,

        /// 共享目录 (host_path=guest_path)
        #[arg(short, long)]
        share: Vec<String>,
    },

    /// 检查系统环境是否满足要求
    Check,
}

fn main() -> Result<()> {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    let cli = Cli::parse();

    match cli.command {
        Commands::Run {
            exe,
            args,
            memory,
            cpus,
            timeout,
            network,
            allowed_domains,
            share,
            sandbox_type,
            diff_disk,
            diff_disk_base,
            max_memory,
            max_cpu,
            max_processes,
            json,
            verbose,
        } => cmd_run(RunArgs {
            exe,
            args,
            memory,
            cpus,
            timeout,
            network,
            allowed_domains,
            share,
            sandbox_type,
            diff_disk,
            diff_disk_base: diff_disk_base.unwrap_or_default(),
            max_memory: max_memory.unwrap_or(0),
            max_cpu: max_cpu.unwrap_or(0),
            max_processes: max_processes.unwrap_or(0),
            json,
            _verbose: verbose,
        }),

        Commands::ShowConfig {
            sandbox_type,
            memory,
            cpus,
            network,
            diff_disk,
            share,
        } => cmd_show_config(&sandbox_type, memory, cpus, network, diff_disk, &share),

        Commands::Check => cmd_check(),
    }
}

struct RunArgs {
    exe: PathBuf,
    args: Vec<String>,
    memory: u64,
    cpus: u32,
    timeout: u64,
    network: bool,
    allowed_domains: Vec<String>,
    share: Vec<String>,
    sandbox_type: String,
    diff_disk: bool,
    diff_disk_base: String,
    max_memory: u64,
    max_cpu: u32,
    max_processes: u32,
    json: bool,
    _verbose: bool,
}

fn cmd_run(args: RunArgs) -> Result<()> {
    use colored::*;
    use config::{SandboxConfig, SandboxType, SharedDir};

    // 1. 检查前提条件
    check_prerequisites()?;

    // 2. 解析沙箱类型
    let sandbox_type = match args.sandbox_type.as_str() {
        "hyperv" => SandboxType::HyperVVM,
        "container" => SandboxType::WindowsContainer,
        "linux" => SandboxType::LinuxContainer,
        _ => anyhow::bail!("未知沙箱类型: {}", args.sandbox_type),
    };

    // 3. 解析共享目录（用 = 分隔，因为 Windows 路径包含 :）
    // 格式: host_path=guest_path 或 host_path=guest_path=ro
    let shared_dirs: Vec<SharedDir> = args
        .share
        .iter()
        .map(|s| {
            let parts: Vec<&str> = s.splitn(3, '=').collect();
            SharedDir {
                host_path: parts[0].to_string(),
                guest_path: parts.get(1).unwrap_or(&"C:\\shared").to_string(),
                read_only: parts.get(2).is_some_and(|v| *v == "ro"),
            }
        })
        .collect();

    // 4. 创建工作目录
    let workspace = tempfile::tempdir()?;
    let workspace_path = workspace.path().to_string_lossy().to_string();

    // 5. 构造配置
    let config = SandboxConfig {
        name: "wsandbox".to_string(),
        sandbox_type,
        memory_mb: args.memory,
        cpu_count: args.cpus,
        enable_network: args.network,
        network_allowed_domains: args.allowed_domains.clone(),
        shared_dirs,
        workspace_path,
        enable_diff_disk: args.diff_disk,
        diff_disk_base: args.diff_disk_base.clone(),
    };

    // 6. 构造资源限制
    let limits = config::ResourceLimits {
        max_memory_mb: args.max_memory,
        max_cpu_percent: args.max_cpu,
        max_processes: args.max_processes,
    };

    // 7. 显示配置摘要
    eprintln!(
        "{} 创建 {:?} 沙箱 (内存: {}MB, CPU: {} 核)",
        "●".blue(),
        args.sandbox_type,
        args.memory,
        args.cpus
    );
    if args.diff_disk {
        eprintln!("  差分磁盘: 启用");
    }
    if args.network {
        eprintln!(
            "  网络: 启用{}",
            if args.allowed_domains.is_empty() {
                String::new()
            } else {
                format!(" (白名单: {})", args.allowed_domains.join(", "))
            }
        );
    }
    if !config.shared_dirs.is_empty() {
        eprintln!("  共享目录: {} 个", config.shared_dirs.len());
    }
    if limits.max_memory_mb > 0 || limits.max_cpu_percent > 0 || limits.max_processes > 0 {
        eprint!("  资源限制:");
        if limits.max_memory_mb > 0 {
            eprint!(" 内存≤{}MB", limits.max_memory_mb);
        }
        if limits.max_cpu_percent > 0 {
            eprint!(" CPU≤{}%", limits.max_cpu_percent);
        }
        if limits.max_processes > 0 {
            eprint!(" 进程≤{}", limits.max_processes);
        }
        eprintln!();
    }

    // 8. 创建并启动沙箱
    let mut sandbox = sandbox::Sandbox::new(config)?;
    sandbox.start()?;

    eprintln!("{} 沙箱已启动", "●".green());

    // 9. 执行 exe
    let exe_str = args.exe.to_string_lossy().to_string();
    let arg_refs: Vec<&str> = args.args.iter().map(|s| s.as_str()).collect();

    let result = sandbox.execute(&exe_str, &arg_refs, (args.timeout * 1000) as u32, &limits)?;

    // 10. 输出结果
    if args.json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "exit_code": result.exit_code,
                "stdout": result.stdout,
                "stderr": result.stderr,
                "elapsed_ms": result.elapsed_ms,
            }))?
        );
    } else {
        if !result.stdout.is_empty() {
            print!("{}", result.stdout);
        }
        if !result.stderr.is_empty() {
            eprint!("{}", result.stderr);
        }
        eprintln!();
        let status = if result.exit_code == 0 {
            "✓ 成功".green()
        } else {
            format!("✗ 退出码 {}", result.exit_code).red()
        };
        eprintln!("{} | 耗时 {}ms", status, result.elapsed_ms);
    }

    // 11. 销毁沙箱（Drop 会自动调用 terminate）
    sandbox.terminate()?;
    eprintln!("{} 沙箱已销毁，临时文件已清理", "●".dimmed());

    if result.exit_code != 0 {
        std::process::exit(result.exit_code);
    }

    Ok(())
}

fn cmd_show_config(
    sandbox_type: &str,
    memory: u64,
    cpus: u32,
    network: bool,
    diff_disk: bool,
    share: &[String],
) -> Result<()> {
    use config::{SandboxConfig, SandboxType, SharedDir};

    let st = match sandbox_type {
        "hyperv" => SandboxType::HyperVVM,
        "container" => SandboxType::WindowsContainer,
        "linux" => SandboxType::LinuxContainer,
        _ => anyhow::bail!("未知类型"),
    };

    let shared_dirs: Vec<SharedDir> = share
        .iter()
        .map(|s| {
            let parts: Vec<&str> = s.splitn(3, '=').collect();
            SharedDir {
                host_path: parts[0].to_string(),
                guest_path: parts.get(1).unwrap_or(&"C:\\shared").to_string(),
                read_only: parts.get(2).is_some_and(|v| *v == "ro"),
            }
        })
        .collect();

    let config = SandboxConfig {
        name: "demo".to_string(),
        sandbox_type: st,
        memory_mb: memory,
        cpu_count: cpus,
        enable_network: network,
        network_allowed_domains: vec![],
        shared_dirs,
        workspace_path: "C:\\temp\\sandbox".to_string(),
        enable_diff_disk: diff_disk,
        diff_disk_base: String::new(),
    };

    println!("{}", config.to_hcs_json());
    Ok(())
}

fn cmd_check() -> Result<()> {
    use colored::*;

    eprintln!("{}", "── 环境检查 ──".bold());
    eprintln!();

    // 1. Windows 版本
    let output = std::process::Command::new("cmd")
        .args(["/c", "ver"])
        .output();
    match output {
        Ok(o) => {
            let ver = String::from_utf8_lossy(&o.stdout);
            eprintln!("{} Windows: {}", "●".blue(), ver.trim());
        }
        Err(e) => eprintln!("{} 无法检测 Windows 版本: {}", "✗".red(), e),
    }

    // 2. Hyper-V 状态
    let output = std::process::Command::new("powershell")
        .args([
            "-Command",
            "(Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V).State",
        ])
        .output();
    match output {
        Ok(o) => {
            let state = String::from_utf8_lossy(&o.stdout).trim().to_string();
            if state.contains("Enabled") {
                eprintln!("{} Hyper-V: 已启用", "✓".green());
            } else {
                eprintln!("{} Hyper-V: 未启用（状态: {}）", "✗".red(), state);
                eprintln!(
                    "  → 启用方法: {} (管理员)",
                    "dism /online /enable-feature /featurename:Microsoft-Hyper-V /all".dimmed()
                );
            }
        }
        Err(_) => {
            eprintln!("{} Hyper-V: 无法检测（可能需要管理员权限）", "!".yellow());
        }
    }

    // 3. vmcompute.dll
    let output = std::process::Command::new("powershell")
        .args([
            "-Command",
            "Get-Service vmcompute | Select-Object -ExpandProperty Status",
        ])
        .output();
    match output {
        Ok(o) => {
            let status = String::from_utf8_lossy(&o.stdout).trim().to_string();
            if status.contains("Running") {
                eprintln!("{} vmcompute 服务: 运行中", "✓".green());
            } else {
                eprintln!("{} vmcompute 服务: {}", "✗".red(), status);
            }
        }
        Err(_) => eprintln!("{} vmcompute: 无法检测", "!".yellow()),
    }

    // 4. 管理员权限
    let output = std::process::Command::new("powershell")
        .args([
            "-Command",
            "([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)",
        ])
        .output();
    match output {
        Ok(o) => {
            let is_admin = String::from_utf8_lossy(&o.stdout).trim() == "True";
            if is_admin {
                eprintln!("{} 管理员权限: 是", "✓".green());
            } else {
                eprintln!("{} 管理员权限: 否（需要管理员权限）", "✗".red());
            }
        }
        Err(_) => eprintln!("{} 权限检测: 失败", "!".yellow()),
    }

    // 5. vmcompute.dll 动态加载
    if hcs::is_available() {
        eprintln!("{} vmcompute.dll: 可加载", "✓".green());
    } else {
        eprintln!("{} vmcompute.dll: 无法加载", "✗".red());
    }

    // 6. 差分磁盘基础镜像
    let base = config::detect_base_image();
    if base.is_empty() {
        eprintln!("{} 差分磁盘基础镜像: 未找到", "!".yellow());
    } else {
        eprintln!("{} 差分磁盘基础镜像: {}", "✓".green(), base);
    }

    eprintln!();
    eprintln!("── 如果检查全部通过，即可使用 wsandbox-vm run 命令 ──");

    Ok(())
}

/// 检查前提条件
fn check_prerequisites() -> Result<()> {
    // 检查是否以管理员运行
    let output = std::process::Command::new("powershell")
        .args([
            "-Command",
            "([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)",
        ])
        .output()?;

    let is_admin = String::from_utf8_lossy(&output.stdout).trim() == "True";
    if !is_admin {
        anyhow::bail!(
            "需要管理员权限运行。\n\
             请右键以管理员身份运行命令提示符，然后重新执行。"
        );
    }

    Ok(())
}
