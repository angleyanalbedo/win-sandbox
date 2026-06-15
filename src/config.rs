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

    /// 共享目录：Host 路径 → VM 内路径
    pub shared_dirs: Vec<SharedDir>,

    /// 沙箱临时文件存放位置（差分磁盘等）
    pub workspace_path: String,
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
        // 这个配置对应一个轻量 Hyper-V VM
        // 使用差分磁盘，基础镜像共享 Host 的系统文件
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
                "Devices": {
                    "Scsi": {
                        "0": {}  // SCSI 控制器
                    }
                },
                "GuestConnection": {
                    "UseConnectedSuspend": true,
                    "UseConnectedBridge": true
                }
            },

            // ── 存储（差分磁盘）──
            "Storage": {
                "Layers": [
                    // 基础层：Host 系统文件（只读，共享）
                    // 这个路径需要根据实际系统调整
                ],
                "Path": format!("{}\\sandbox.vhdx", self.workspace_path),
                "AutoAttachVirtualHardDisks": true
            },

            // ── 网络 ──
            "GuestNetwork": if self.enable_network {
                serde_json::json!({
                    "AdapterName": "SandboxNIC",
                    "NatName": "SandboxNAT"
                })
            } else {
                serde_json::json!({})
            }
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
