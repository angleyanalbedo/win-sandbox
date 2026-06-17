# Agents Guide

AI agents working on this project should follow these guidelines.

## Project Overview

win-sandbox is a Windows sandbox tool that creates isolated container environments using the HCS (Host Compute Service) API. The goal is to run untrusted code in a lightweight, secure Windows container.

## Key Technical Constraints

### Layer Storage Incompatibility

Windows has two incompatible layer storage systems:

1. **Windows Servicing Stack** (`C:\ProgramData\Microsoft\Windows\Containers\Layers\`) — system-installed base layers, NOT compatible with hcsshim
2. **WCIFS filter driver** (`C:\ProgramData\Docker\windowsfilter\`) — Docker/containerd managed layers, compatible with hcsshim

The hcsshim layer APIs (`CreateScratchLayer`, `ActivateLayer`, `PrepareLayer`) only work with WCIFS-managed layers. Attempting to use system-installed layers will fail with error `0x3`.

### Container Isolation Modes

- **Process isolation (Argon)**: Container shares host kernel. Faster, less secure. `HvPartition: false`
- **Hyper-V isolation (Xenon)**: Container runs in a Utility VM. Slower, more secure. `HvPartition: true`

For a sandbox running untrusted code, Hyper-V isolation is preferred.

### API Levels in hcsshim

```text
Level 1: hcsoci.CreateContainer   — takes OCI Spec, auto-manages layers
Level 2: layers.MountWCOWLayers   — layer mounting
Level 3: hcsshim.CreateContainer  — takes ContainerConfig, manual layer management
Level 4: vmcompute.dll            — raw Windows syscalls
```

Currently the project uses Level 3 (`hcsshim.CreateContainer`). The plan is to migrate to Level 1 (`hcsoci.CreateContainer`) once the OCI spec integration is complete.

## Code Structure

- `main.go` — entry point, container creation and command execution
- `docs/` — API reference documentation
- `scripts/` — diagnostic PowerShell scripts
- `hcsshim/` — git submodule, reference source code only (not imported directly)

## Development Rules

1. **Never commit code that hasn't been tested** — this is a Windows-specific project, many APIs only work on Windows with specific features enabled
2. **Use public hcsshim APIs** — the `internal` packages cannot be imported from outside the hcsshim module
3. **Run diagnostics before debugging** — use `scripts/check_env.ps1` to verify environment state
4. **Document layer-related findings** — layer management is the most error-prone part, always document what works and what doesn't
5. **Admin privileges required** — HCS API calls require administrator privileges

## Known Issues

- System-installed container layers are not compatible with hcsshim filter driver API
- Docker windowsfilter directory requires admin access
- `internal/hcsoci` cannot be imported due to transitive dependency conflicts

## Testing

```powershell
# Check environment
powershell -ExecutionPolicy Bypass -File scripts\check_env.ps1

# Build
go build -o win-sandbox.exe .

# Run (admin required)
.\win-sandbox.exe
```

## Resources

- [hcsshim source](https://github.com/microsoft/hcsshim)
- [HCS API docs](https://learn.microsoft.com/en-us/virtualization/api/)
- [Windows container images](https://mcr.microsoft.com/)
