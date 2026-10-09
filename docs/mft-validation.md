# MFT 验证记录

日期：2026-10-09  
卷：D:\  
权限：普通用户，非管理员  
工具：`build/DumpingGround-verify.exe -volume D:\ -timeout 5s`

结果：`INCONCLUSIVE`，退出码 2。

```text
strict MFT read failed: MFT access unavailable: Access is denied.
```

该结果证明严格模式没有回退后伪造 PASS。管理员验证待在管理员 PowerShell 中运行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Verify-MFT.ps1 -Volume D:\ -TimeoutSeconds 600
```

## 解析加固（2026-10-09，代码级核查）

非管理员权限下 `CreateFile \\.\D:` 在 IOCTL 之前即返回 Access denied，因此提升权限前的失败点核查为代码级：

- 单个 MFT 记录解码失败（撕裂/损坏记录、非法文件名）原会导致整卷扫描中止并回退到目录遍历。现改为计入 `Snapshot.Skipped` 后继续处理剩余记录，与目录遍历对不可读条目的处理一致。
- 常规文件缺少可识别的无名 `$DATA` 流（EFS、命名流、跨扩展记录存放的数据）原返回 `ErrUnsupported` 触发整卷回退。现照常建立索引并把大小记为估算（`Snapshot.Estimated`）。
- 结构性无法解释的布局（扩展运行列表等必须解析 `$MFT` 自身属性的情形）仍返回显式错误，不做静默回退。
- `internal/disk/mft_scan_test.go` 用合成 MFT 流覆盖：正常聚合、无 `$DATA` 文件、坏记录跳过、非法文件名跳过。

待管理员实机验证：`DeviceIoControl(FSCTL_GET_NTFS_VOLUME_DATA)` 返回值、96 字节参数块（sector/cluster/recordBytes/MftStartLcn/TotalClusters）与真实卷 MFT 运行列表解码结果。

Windows 实测：当前进程全局句柄搜索返回约 110 个句柄；当前线程 PSS + DbgHelp 返回 1 个地址帧。句柄缓冲区 GC 生命周期问题已修复；调用栈地址展开链还需在 native PE/PDB 进程上继续验证。受保护进程和符号解析限制见 [diagnostics.md](diagnostics.md)。
