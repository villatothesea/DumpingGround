# MFT 验证

DumpingGround 提供严格 MFT 对照程序：

```powershell
# 先构建 build/DumpingGround-verify.exe
powershell -ExecutionPolicy Bypass -File .\scripts\Verify-MFT.ps1 -Volume D:\ -TimeoutSeconds 600
```

验证过程有两条独立路径：

1. 严格只读打开 `\\.\D:`，读取 NTFS 卷参数和 `$MFT` 数据流，解析 FILE record、更新序列数组、`$FILE_NAME`、`$DATA`、run list、目录父子关系、逻辑大小和分配大小。
2. 对同一个卷再执行一遍普通目录遍历，建立路径到逻辑/分配大小的表。

最终只会出现三种状态：

- `PASS`：两条路径的可达文件集合、逻辑大小和分配大小完全一致。
- `FAIL`：两条路径都完成，但存在缺失文件、父子图差异或大小差异；输出前 20 条差异。
- `INCONCLUSIVE`：MFT 被拒绝、卷不是支持的 NTFS 布局、属性列表/稀疏压缩数据无法安全解析、读取中断或目录参考扫描失败。程序不会回退到普通目录扫描后再声称 MFT 通过。

当前开发机普通权限实测：

```text
status=INCONCLUSIVE volume=D:\
error: strict MFT read failed: MFT access unavailable: Access is denied.
exit=2
```

这是正确结果。管理员 PowerShell 中重新运行同一命令，才是 MFT 全盘验证；需要同时记录管理员状态、文件系统、MFT duration、目录 walk duration 和差异数量。不要用 `tree`、资源管理器的“属性”或单纯目录扫描结果宣称 MFT 已验证。

## 句柄搜索

进程详情的 `Handles` 页按需运行：

- `NtQuerySystemInformation(SystemExtendedHandleInformation)` 获取系统句柄表。
- 按 PID 过滤；通过 `OpenProcess(PROCESS_DUP_HANDLE)` 和 `DuplicateHandle` 把句柄复制到当前进程。
- `NtQueryObject(ObjectTypeInformation/ObjectNameInformation)` 读取类型和对象名。
- 访问拒绝、已退出进程、无名对象和超时对象被跳过并计数，不阻塞进程采样。
- 搜索框会在类型和对象名上过滤，结果上限为 5000。

这覆盖 PE 的“谁打开了这个对象”的核心路径；受保护进程、内核句柄和部分对象名称仍受 Windows 权限限制。

## 线程调用栈

进程详情的 `Stack` 页按需运行：

- `PssCaptureSnapshot(PSS_CAPTURE_THREADS | PSS_CAPTURE_THREAD_CONTEXT)` 获取线程上下文。
- 用 `StackWalk64` 展开 x64 地址帧。
- DbgHelp `SymFromAddr` 尝试解析符号；无 PDB 时仍显示地址帧。
- 线程上下文、进程读取权限或架构不满足时显示失败原因。

当前先保证地址栈链路，符号服务器、PDB 缓存、WOW64 x86 栈和 ARM64 栈属于后续兼容工作。调用栈不是“读取某个 API 的静态列表”，它依赖线程暂停/快照、可读栈内存、正确的架构上下文和 unwind 元数据。
