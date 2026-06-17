# Docker 镜像存储结构（Windows）

Docker 在 Windows 上如何存储镜像、层、容器的完整说明。

## 1. 存储根目录

```text
C:\ProgramData\Docker\
├── image\windowsfilter\    ← 镜像元数据库（索引、配置、层关系）
└── windowsfilter\          ← 实际层数据（Files\, Hives\ 等）
```

两个目录配合使用：

- `image\windowsfilter\` 存的是"谁在哪里"（索引）
- `windowsfilter\` 存的是"实际内容"（文件系统）

## 2. 镜像元数据库结构

```text
C:\ProgramData\Docker\image\windowsfilter\
├── repositories.json                    ← 镜像名 → 镜像 ID 的映射
├── imagedb\
│   └── content\sha256\                  ← 镜像 ID → 镜像配置
│       └── 2f14ee035891...              ← 镜像配置 JSON（包含层 diff ID 列表）
└── layerdb\
    └── sha256\                          ← 层 diff ID → cache ID 的映射
        └── b2c929f0a04e...\
            ├── cache-id                 ← 实际层目录名
            ├── diff                     ← diff ID
            ├── size                     ← 层大小
            └── tar-split.json.gz        ← tar 分割数据
```

## 3. 查找层的完整路径（三步）

以 `mcr.microsoft.com/windows/nanoserver:ltsc2022` 为例：

### 第一步：repositories.json → 镜像 ID

```json
{
  "Repositories": {
    "mcr.microsoft.com/windows/nanoserver": {
      "mcr.microsoft.com/windows/nanoserver:ltsc2022": "sha256:2f14ee035891..."
    }
  }
}
```

输入：镜像名 `mcr.microsoft.com/windows/nanoserver:ltsc2022`
输出：镜像 ID `sha256:2f14ee0358910e78990d182b4dd576c30d1785f8c45d6a419323ea0c4c19ab5e`

### 第二步：镜像配置 → 层 diff ID 列表

文件路径：`imagedb/content/sha256/2f14ee035891...`

```json
{
  "rootfs": {
    "type": "layers",
    "diff_ids": [
      "sha256:b2c929f0a04e37f232257591e53df7c9031b5b4e0764fce28630091e8c79ff92"
    ]
  }
}
nanoserver 只有一个层。

### 第三步：layerdb → cache ID → 实际路径

层数据库路径：`layerdb/sha256/b2c929f0a04e...`

读取 `cache-id` 文件内容：`d643449f9e57da1a0c840dc257019281c4e01d02f814b3fe4eea12087b492291`

实际层路径：`C:\ProgramData\Docker\windowsfilter\d643449f9e57da1a0c840dc257019281c4e01d02f814b3fe4eea12087b492291`

## 4. 实际层目录结构

```text
C:\ProgramData\Docker\windowsfilter\d643449f...\
├── Files\           ← 容器的 C:\ 盘（Windows 系统文件）
│   ├── Windows\
│   │   ├── System32\
│   │   └── ...
│   └── ...
├── Hives\           ← 注册表文件
│   ├── DEFAULT
│   ├── SAM
│   ├── SECURITY
│   ├── SOFTWARE
│   └── SYSTEM
├── UtilityVM\       ← Hyper-V 隔离用的 Utility VM 镜像
├── layerchain.json  ← 层链（父层列表，基础层为 null）
├── blank-base.vhdx  ← 基础 VHD
├── blank.vhdx       ← scratch VHD
└── layout           ← 层布局信息
```

### 各目录/文件的作用

| 名称 | 作用 |
| --- | --- |
| `Files\` | 容器的文件系统，合并后成为容器的 C:\ 盘 |
| `Hives\` | 注册表蜂巢文件，容器启动时加载 |
| `UtilityVM\` | Hyper-V 隔离容器的轻量 VM 镜像 |
| `layerchain.json` | 层依赖链，基础层为 `null`，子层包含父层 ID |
| `blank-base.vhdx` | 基础虚拟硬盘 |
| `blank.vhdx` | scratch 虚拟硬盘（可写层） |

## 5. 多层镜像的存储

如果镜像有多个层（如在基础镜像上安装了软件），结构如下：

```text
repositories.json → 镜像 ID
    ↓
imagedb/content/sha256/<镜像ID>
    ↓
rootfs.diff_ids = [层3, 层2, 层1]   ← 从顶到底
    ↓
layerdb/sha256/<层3>/cache-id → windowsfilter/aaa/
layerdb/sha256/<层2>/cache-id → windowsfilter/bbb/
layerdb/sha256/<层1>/cache-id → windowsfilter/ccc/
    ↓
层3 的 layerchain.json = [层2, 层1]
层2 的 layerchain.json = [层1]
层1 的 layerchain.json = null（基础层）
```

## 6. 为什么需要管理员权限

Docker 的 `windowsfilter\` 目录设置了严格的 ACL：

- 只有 SYSTEM 和 Docker 服务账户有完全访问权限
- 普通用户（即使是管理员组）需要"以管理员身份运行"才能访问
- 这是安全设计，防止容器文件系统被非授权访问

## 7. 验证数据（实际输出）

以下数据来自本机 Windows 11 Pro + Docker Desktop：

```text
镜像: mcr.microsoft.com/windows/nanoserver:ltsc2022
镜像 ID: sha256:2f14ee0358910e78990d182b4dd576c30d1785f8c45d6a419323ea0c4c19ab5e

层 diff ID: sha256:b2c929f0a04e37f232257591e53df7c9031b5b4e0764fce28630091e8c79ff92
层 cache ID: d643449f9e57da1a0c840dc257019281c4e01d02f814b3fe4eea12087b492291
层路径: C:\ProgramData\Docker\windowsfilter\d643449f9e57da1a0c840dc257019281c4e01d02f814b3fe4eea12087b492291

层目录内容:
├── Files\         ✓
├── Hives\         ✓
├── UtilityVM\     ✓
├── layerchain.json (内容: null，基础层)
├── blank-base.vhdx
├── blank.vhdx
└── layout
```

## 8. 代码实现

查找函数位于 `pkg/docker/layers.go`，流程如下：

```text
FindImageLayers(imageRef)
    ↓
getImageID(imageRef)          ← 读 repositories.json
    ↓
getImageDiffIDs(imageID)      ← 读 imagedb/content/sha256/<hash>
    ↓
resolveLayer(diffID)           ← 读 layerdb/sha256/<hash>/cache-id
    ↓
返回 ImageLayer{Path, HasFiles, HasHives, ...}
```

测试位于 `pkg/docker/layers_test.go`。

---

*文档基于 Windows 11 Pro + Docker Desktop 实际验证*
