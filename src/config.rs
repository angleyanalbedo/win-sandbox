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
        // 共享目录作为挂载层
        let mount_layers: Vec<serde_json::Value> = self
            .shared_dirs
            .iter()
            .map(|d| {
                serde_json::json!({
                    "Path": d.host_path,
                    "ReadOnly": d.read_only
                })
            })
            .collect();

        // 自动检测容器基础镜像层
        let base_layers = detect_container_layers();
        let mut all_layers: Vec<serde_json::Value> = base_layers
            .iter()
            .map(|p| {
                serde_json::json!({
                    "Path": p,
                    "ReadOnly": true
                })
            })
            .collect();
        all_layers.extend(mount_layers);

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
                    "Layers": all_layers
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
        // 自动检测 WSL2 内核路径
        let kernel_path = detect_wsl_kernel();

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
                        "KernelFilePath": kernel_path,
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

    // 2. 搜索 ContainerStorages 下的 sandbox.vhdx（Windows Sandbox 功能安装后生成）
    //    优先选最大的文件（完整的基础镜像通常 500MB+）
    let container_storages = r"C:\ProgramData\Microsoft\Windows\Containers\ContainerStorages";
    let mut best_vhdx: Option<(String, u64)> = None;
    if let Ok(entries) = std::fs::read_dir(container_storages) {
        for entry in entries.flatten() {
            let vhdx = entry.path().join("sandbox.vhdx");
            if vhdx.exists() {
                let size = std::fs::metadata(&vhdx).map(|m| m.len()).unwrap_or(0);
                if best_vhdx.as_ref().is_none_or(|(_, s)| size > *s) {
                    best_vhdx = Some((vhdx.to_string_lossy().to_string(), size));
                }
            }
        }
    }
    if let Some((path, _)) = best_vhdx {
        return path;
    }

    // 3. 检查 Hyper-V 默认路径下是否有基础镜像
    let hyperv_base =
        r"C:\Users\Public\Documents\Hyper-V\Virtual Hard Disks\BaseImage.vhdx".to_string();
    if std::path::Path::new(&hyperv_base).exists() {
        return hyperv_base;
    }

    // 3. 检查 Hyper-V 默认目录下是否有任何 .vhdx 文件
    let hyperv_dir = r"C:\Users\Public\Documents\Hyper-V\Virtual Hard Disks";
    if let Ok(entries) = std::fs::read_dir(hyperv_dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().is_some_and(|e| e == "vhdx") {
                return path.to_string_lossy().to_string();
            }
        }
    }

    String::new()
}

/// 自动检测 Windows 容器基础镜像层路径
///
/// 安装 Windows Containers 功能后，系统会自带基础镜像层。
/// 这些镜像层存储在 ProgramData\Microsoft\Windows\Containers\BaseImages 下。
pub fn detect_container_layers() -> Vec<String> {
    let mut layers = Vec::new();

    // 1. 检查标准容器基础镜像路径
    let base_paths = [
        r"C:\ProgramData\Microsoft\Windows\Containers\BaseImages",
        r"C:\ProgramData\Microsoft\Windows\Containers\BaseImages\WCOS",
    ];

    for base_dir in &base_paths {
        if let Ok(entries) = std::fs::read_dir(base_dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                // 查找 .vhdx 文件或以 Layers 命名的目录
                if path.extension().is_some_and(|e| e == "vhdx") {
                    layers.push(path.to_string_lossy().to_string());
                }
                // 检查子目录中的 Layers
                if path.is_dir() {
                    let layers_dir = path.join("Layers");
                    if layers_dir.exists() {
                        if let Ok(layer_entries) = std::fs::read_dir(&layers_dir) {
                            for layer_entry in layer_entries.flatten() {
                                let layer_path = layer_entry.path();
                                if layer_path.extension().is_some_and(|e| e == "vhdx") {
                                    layers.push(layer_path.to_string_lossy().to_string());
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // 2. 检查 Docker Desktop 的 Windows 容器镜像
    let docker_paths = [
        r"C:\ProgramData\Docker\windowsfilter",
        r"C:\ProgramData\DockerDesktop\vm-data",
    ];
    for docker_dir in &docker_paths {
        if let Ok(entries) = std::fs::read_dir(docker_dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.is_dir() {
                    // Docker 的镜像层目录里通常有 Layer.vhdx
                    let layer_vhdx = path.join("Layer.vhdx");
                    if layer_vhdx.exists() {
                        layers.push(layer_vhdx.to_string_lossy().to_string());
                    }
                }
            }
        }
    }

    layers
}

/// 自动检测 WSL2 内核路径
pub fn detect_wsl_kernel() -> String {
    // 1. Windows 内置的 WSL2 内核
    let builtin_paths = [
        r"C:\Windows\System32\lxss\tools\kernel",
        r"C:\Windows\System32\lxss\tools\kernel.efi",
    ];
    for p in &builtin_paths {
        if std::path::Path::new(p).exists() {
            return p.to_string();
        }
    }

    // 2. Microsoft Store 版 WSL2 的内核
    let store_paths = [
        r"C:\Program Files\WSL\microsoft.kernel",
        r"C:\Program Files\WSL\kernel",
    ];
    for p in &store_paths {
        if std::path::Path::new(p).exists() {
            return p.to_string();
        }
    }

    // 3. 搜索 WSL 安装目录下的 kernel 文件
    let wsl_dir = r"C:\Program Files\WSL";
    if let Ok(entries) = std::fs::read_dir(wsl_dir) {
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().to_lowercase();
            if name.contains("kernel") {
                return entry.path().to_string_lossy().to_string();
            }
        }
    }

    String::new()
}

/// 检查各种组件的可用性，返回描述信息
pub fn check_components() -> Vec<ComponentStatus> {
    let mut statuses = Vec::new();

    // Hyper-V VM 基础镜像
    let base = detect_base_image();
    statuses.push(ComponentStatus {
        name: "Hyper-V VM 基础镜像".to_string(),
        available: !base.is_empty(),
        path: base,
        mode: "hyperv".to_string(),
    });

    // 容器基础镜像
    let container_layers = detect_container_layers();
    statuses.push(ComponentStatus {
        name: "容器基础镜像".to_string(),
        available: !container_layers.is_empty(),
        path: container_layers.first().cloned().unwrap_or_default(),
        mode: "container".to_string(),
    });

    // WSL2 内核
    let kernel = detect_wsl_kernel();
    statuses.push(ComponentStatus {
        name: "WSL2 内核".to_string(),
        available: !kernel.is_empty(),
        path: kernel,
        mode: "linux".to_string(),
    });

    statuses
}

/// 组件状态
pub struct ComponentStatus {
    pub name: String,
    pub available: bool,
    pub path: String,
    pub mode: String,
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
