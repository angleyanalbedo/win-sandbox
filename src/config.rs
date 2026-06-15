//! HCS 系统配置生成
//!
//! HCS API 使用 JSON 配置来定义 VM/容器的属性。
//! 这里提供 Windows Sandbox 等价物的配置模板。
//!
//! 配置 Schema 参考:
//! https://learn.microsoft.com/en-us/virtualization/api/hcs/resourceschemaversion2

/// 沙箱配置
#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct SandboxConfig {
    /// 沙箱名称（用于标识）
    pub name: String,

    /// 沙箱类型
    pub sandbox_type: SandboxType,

    /// 分配给 VM 的内存 (MB)
    pub memory_mb: u64,

    /// CPU 核心数限制
    pub cpu_count: u32,

    /// 是否启用网络
    pub enable_network: bool,

    /// 网络域名白名单（为空则不限制，仅 allow_network=true 时生效）
    pub network_allowed_domains: Vec<String>,

    /// 共享目录：Host 路径 → VM 内路径
    pub shared_dirs: Vec<SharedDir>,

    /// 沙箱临时文件存放位置（差分磁盘等）
    pub workspace_path: String,

    /// 是否使用差分磁盘（共享 Host 基础镜像，只存改动）
    pub enable_diff_disk: bool,

    /// 差分磁盘基础镜像路径（为空则自动检测）
    pub diff_disk_base: String,
}

#[derive(Debug, Clone)]
pub enum SandboxType {
    /// Windows 容器（需要 nanoserver/servercore 基础镜像）
    WindowsContainer,
    /// Hyper-V 轻量 VM（最接近 Windows Sandbox）
    HyperVVM,
    /// Linux 容器（通过 WSL2）
    LinuxContainer,
}

#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct SharedDir {
    pub host_path: String,
    pub guest_path: String,
    pub read_only: bool,
}

impl SandboxConfig {
    /// 生成 HCS JSON 配置
    pub fn to_hcs_json(&self) -> String {
        match self.sandbox_type {
            SandboxType::HyperVVM => self.hyperv_vm_config(),
            SandboxType::WindowsContainer => self.windows_container_config(),
            SandboxType::LinuxContainer => self.linux_container_config(),
        }
    }

    /// Hyper-V 轻量 VM 配置（最接近 Windows Sandbox）
    fn hyperv_vm_config(&self) -> String {
        // ── 共享目录（MappedDirectories）──
        let mapped_dirs: Vec<serde_json::Value> = self
            .shared_dirs
            .iter()
            .map(|d| {
                serde_json::json!({
                    "HostPath": d.host_path,
                    "ContainerPath": d.guest_path,
                    "ReadOnly": d.read_only,
                    "Lun": 0
                })
            })
            .collect();

        // ── 差分磁盘层 ──
        let layers = if self.enable_diff_disk {
            let base = if self.diff_disk_base.is_empty() {
                // 自动检测 Windows 基础镜像路径
                detect_base_image()
            } else {
                self.diff_disk_base.clone()
            };
            if base.is_empty() {
                vec![]
            } else {
                vec![serde_json::json!({
                    "Id": "os",
                    "Path": base,
                    "ReadOnly": true,
                    "Type": "Dynamic"
                })]
            }
        } else {
            vec![]
        };

        // ── 网络配置 ──
        let network = if self.enable_network {
            let mut net = serde_json::json!({
                "AdapterName": "SandboxNIC",
                "NatName": "SandboxNAT"
            });
            // 域名白名单
            if !self.network_allowed_domains.is_empty() {
                net["DnsSearchList"] = serde_json::Value::Array(
                    self.network_allowed_domains
                        .iter()
                        .map(|d| serde_json::Value::String(d.clone()))
                        .collect(),
                );
            }
            net
        } else {
            serde_json::json!({})
        };

        // ── 设备配置 ──
        let mut devices = serde_json::json!({
            "Scsi": {
                "0": {}
            }
        });

        // 如果有共享目录，加入 MappedDirectories
        if !mapped_dirs.is_empty() {
            devices["MappedDirectories"] = serde_json::Value::Array(mapped_dirs);
        }

        let config = serde_json::json!({
            "Owner": "win-sandbox",
            "SchemaVersion": {
                "Major": 2,
                "Minor": 1
            },
            "ShouldTerminateOnLastHandleClosed": true,

            // ── VM 属性 ──
            "VirtualMachine": {
                "StopOnReset": true,
                "Chipset": {
                    "LinuxKernelDirect": {}
                },
                "ComputeTopology": {
                    "Memory": {
                        "SizeInMB": self.memory_mb,
                        "AllowOvercommit": true
                    },
                    "Processor": {
                        "Count": self.cpu_count
                    }
                },
                "Devices": devices,
                "GuestConnection": {
                    "UseConnectedSuspend": true,
                    "UseConnectedBridge": true
                }
            },

            // ── 存储（差分磁盘）──
            "Storage": {
                "Layers": layers,
                "Path": format!("{}\\sandbox.vhdx", self.workspace_path),
                "AutoAttachVirtualHardDisks": true
            },

            // ── 网络 ──
            "GuestNetwork": network
        });

        serde_json::to_string_pretty(&config).unwrap()
    }

    /// Windows 容器配置
    fn windows_container_config(&self) -> String {
        let layers: Vec<serde_json::Value> = self
            .shared_dirs
            .iter()
            .map(|d| {
                serde_json::json!({
                    "Path": d.host_path,
                    "ReadOnly": d.read_only
                })
            })
            .collect();

        let config = serde_json::json!({
            "Owner": "win-sandbox",
            "SchemaVersion": {
                "Major": 2,
                "Minor": 1
            },
            "ShouldTerminateOnLastHandleClosed": true,

            // ── 容器属性 ──
            "Container": {
                "Networking": {
                    "AllowUnqualifiedDNSQueryDomainSuffixList": [],
                    "CompartmentNamespaceHandleIn": "",
                    "EnableHostNetworkAccess": self.enable_network
                },
                "Storage": {
                    "Layers": layers
                }
            },

            // ── 资源限制 ──
            "Resources": {
                "Memory": {
                    "SizeInMB": self.memory_mb,
                    "AllowOvercommit": true
                },
                "Processor": {
                    "Count": self.cpu_count
                }
            }
        });

        serde_json::to_string_pretty(&config).unwrap()
    }

    /// Linux 容器配置（通过 WSL2 的 Hyper-V 后端）
    fn linux_container_config(&self) -> String {
        let config = serde_json::json!({
            "Owner": "win-sandbox",
            "SchemaVersion": {
                "Major": 2,
                "Minor": 1
            },
            "ShouldTerminateOnLastHandleClosed": true,

            "Container": {
                "Storage": {
                    "Layers": [],
                    "Path": self.workspace_path
                }
            },

            "VirtualMachine": {
                "StopOnReset": true,
                "Chipset": {
                    "LinuxKernelDirect": {
                        "KernelFilePath": "",  // WSL2 内核路径
                        "InitRdPath": "",
                        "KernelBootOptions": "8250_core.nr_uarts=0"
                    }
                },
                "ComputeTopology": {
                    "Memory": {
                        "SizeInMB": self.memory_mb
                    },
                    "Processor": {
                        "Count": self.cpu_count
                    }
                }
            }
        });

        serde_json::to_string_pretty(&config).unwrap()
    }
}

/// 自动检测 Windows 基础镜像路径
///
/// Windows Sandbox 使用 WCOS (Windows Core OS) 基础镜像，
/// 普通 Hyper-V VM 可以用 Windows 安装盘中的 install.wim/esd。
/// 这里尝试找到一个可用的 Windows 基础 VHDX。
pub fn detect_base_image() -> String {
    // 1. 检查 Windows Sandbox 的基础镜像（如果安装了 Windows Sandbox）
    let sandbox_paths = [
        r"C:\ProgramData\Microsoft\Windows\Containers\BaseImages\BaseImage.vhdx",
        r"C:\Windows\Containers\BaseImages\BaseImage.vhdx",
    ];
    for p in &sandbox_paths {
        if std::path::Path::new(p).exists() {
            return p.to_string();
        }
    }

    // 2. 检查 Hyper-V 默认路径下是否有基础镜像
    let hyperv_base =
        r"C:\Users\Public\Documents\Hyper-V\Virtual Hard Disks\BaseImage.vhdx".to_string();
    if std::path::Path::new(&hyperv_base).exists() {
        return hyperv_base;
    }

    String::new()
}

/// 生成进程执行配置的 JSON
pub fn process_config(
    command: &str,
    args: &[&str],
    work_dir: Option<&str>,
    env_vars: &[(String, String)],
) -> String {
    let command_line = if args.is_empty() {
        command.to_string()
    } else {
        format!("{} {}", command, args.join(" "))
    };

    let env: serde_json::Map<String, serde_json::Value> = env_vars
        .iter()
        .map(|(k, v)| (k.clone(), serde_json::Value::String(v.clone())))
        .collect();

    let config = serde_json::json!({
        "CommandLine": command_line,
        "CommandArgs": args,
        "WorkingDirectory": work_dir.unwrap_or("C:\\"),
        "Environment": env,
        "CreateStdInPipe": true,
        "CreateStdOutPipe": true,
        "CreateStdErrPipe": true,
        "ConsoleSize": [25, 80],  // rows, cols
        "EmulateConsole": false
    });

    serde_json::to_string(&config).unwrap()
}

/// 进程资源限制配置
#[derive(Debug, Clone, Default)]
pub struct ResourceLimits {
    /// 最大内存 (MB)，0 = 不限制
    pub max_memory_mb: u64,
    /// 最大 CPU 时间百分比 (1-100)，0 = 不限制
    pub max_cpu_percent: u32,
    /// 最大子进程数，0 = 不限制
    pub max_processes: u32,
}
