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

Windows 实测：当前进程全局句柄搜索返回约 110 个句柄；当前线程 PSS + DbgHelp 返回 1 个地址帧。句柄缓冲区 GC 生命周期问题已修复；调用栈地址展开链还需在 native PE/PDB 进程上继续验证。受保护进程和符号解析限制见 [diagnostics.md](diagnostics.md)。
