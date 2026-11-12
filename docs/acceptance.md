# 功能验收

本次优先级：功能和交互先完成，再优化资源占用。构建成功、进程没有退出不等于可用。

## 已自动验证

- 880×580 与 1280×800 逻辑尺寸，100%/150%/200% 缩放：进程数、进程数据、刷新、结束操作入口、盘符选择、扫描、取消、浏览均在窗口范围内。
- 切换页面；选择盘符后路径同步。
- 点击 Scan 扫描真实临时目录，完成后校验文件数和大小，清理索引后释放引用。
- 扫描过程中显示文件计数；取消保持工作线程所有权，最终状态不会被旧进度覆盖。
- 进程列排序、搜索过滤；右键 Properties、结束操作确认以及取消。
- 目录表 Enter 下钻、Up 返回、窗口缩放后空间图仍然可见。
- 点击扩展名筛选并清除；点击空间图文件定位目录和文件。
- CSV 对逗号、中文文件名正确编码；导出路径与扫描结果一致。
- Windows 枚举包含当前进程；当前进程的命令行、用户、模块、线程、句柄数可读取。
- 分配空间与逻辑大小分别计算；硬链接分配空间只计一次。
- treemap 面积比例、不重叠、边界、极端权重与层级矩形预算。
- NTFS 记录的更新序列校验、Unicode 文件名、resident/nonresident 数据、signed run offsets、损坏记录拒绝。
- 本机普通权限的 D 盘扫描：MFT 打开被系统拒绝后回退，限时取消返回部分结果。
- 设置从上一个 `.data` 位置迁移到新的共享/便携副本；未知 JSON 键在保存后保留，便于降级兼容。
- 更新检查器使用本地 HTTP 服务验证 GitHub release JSON、HTTP 错误与版本比较；设置页包含自动检查和立即检查入口。
- 启动页枚举当前用户注册的 AppX/MSIX StartupTask：解析包清单中的 `windows.startupTask`，按包族名读取 `SystemAppData` 状态；合成的 HKCU 项验证 `Enabled(2) → DisabledByUser(1) → Enabled(2)` 往返，启用时写入 `UserEnabledStartupOnce`。
- 本机 `OpenAI.Codex` 同时暴露两个启动入口：`ChatGPT` packaged StartupTask（可由用户持久关闭）和 `CodexSandboxService.OpenAI.Codex` packaged service。服务按 `PackageFullName`/`AppUserModelId` 标记为应用包管理，页面说明包更新或应用可能恢复自动启动，不再暗示服务级关闭一定持久。

## 已做真实窗口检查

通过 MyGo `Window.CapturePage` 捕获本应用绘制结果，不再用可能被其他窗口遮挡的桌面截图判断。

- 实际识别 C、D、E 盘，显示卷标、可用与总容量。
- 扫描 Go 源码目录约 12,018 个文件，约 2.2 秒；逻辑大小约 126.1 MiB，分配空间约 145.5 MiB。数据因测试机器内容变化而不同。
- 扫描中显示当前路径、文件/目录数、活动进度条和 Cancel；完成后显示目录表、文件类型统计和空间图。
- 诊断截图位于忽略的 `build/capture-verified`、`build/review-process` 等目录，不打包到用户应用。

## 仍不能标为已完成

- 管理员环境中的 MFT 全盘结果与 WizTree 对照；当前环境非管理员。
- Windows 10、ARM64、不同文件系统、网络盘的完整兼容矩阵。
- PE 的全局句柄查询、线程调用栈和内核级能力。
- 低内存长期验收：十分钟进程页采样仍发现增长，等待功能稳定后专门处理。
- ChatGPT packaged service 的持久关闭：本次 shell 为普通完整性令牌，`DG_LIVE_SVC` 路径在取得服务所有权时被拒绝，未实际改写服务；未重启 Windows，也未运行 `Reset-AppxPackage`（会删除应用数据/登录状态）。包清单与挂起的 AppX 更新事件确认包服务注册来自 manifest，但“禁用后跨包更新持久”没有在本机实际验收。

## 已修复

- 长时间运行后窗口假卡死（页面无法点击、右上角窗口控制消失、无法拖动）：MyGo v0.3.5 在最大化窗口恢复时按当前离屏窗口位置选择显示器，非主显示器上的客户区和窗口控件会落到错误位置。升级到包含上游修复的 MyGo v0.4.6。
- 最大化窗口最小化到托盘后先还原为小窗：恢复路径对可见窗口也调用 `Restore()`，而 MyGo v0.4.6 会把它解除最大化。现在只在窗口真正处于 minimized 状态时调用 `Restore()`；托盘与第二实例共用该路径。

## 已知缺陷（待排期）

- 暂无记录。

## 复现

```powershell
go test ./...
go vet ./...
$env:DG_TEST_VOLUME = "D:\"
go test ./internal/disk -run TestLiveVolumeScanAndFallback -v
go test ./internal/startup -run 'TestPackaged|TestLivePackagedItems' -v
$env:DG_LIVE_SVC = "1"
$env:DG_APPDIR = "$env:TEMP\dg-live-svc"
go test ./internal/startup -run TestHardenedServiceToggle -v
```

`DG_TEST_VOLUME` 测试是只读且限时的，扫描中取消，不修改文件。`TestLivePackagedItems` 只读；`TestPackaged*` 只写测试用 HKCU package-family 键并清理。`DG_LIVE_SVC` 会切换真实服务，须以管理员运行；可另设 `DG_SVC_ON=1` 恢复。`DG_APPDIR` 可把 undo 记录隔离到测试目录。
