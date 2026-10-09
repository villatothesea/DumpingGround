# DumpingGround

Windows 原生进程查看器与磁盘空间分析工具，使用 Go + [MyGo](https://github.com/egoist/mygo) native UI。以 Process Explorer / WizTree 的常用工作流为目标，仍有功能差距，详见下方。

## 运行

双击 `build/DumpingGround.exe`。默认显示真实进程列表；顶部 Processes / Disk usage 切换页面。再次启动会激活已有窗口。

### 进程

- 默认平铺全部进程，勾选 Tree 切换父子树；进程数始终显示在顶部。
- 搜索名称、PID 或路径；点击表头排序。
- CPU、工作集、私有提交量、读写速率和线程数；无法访问的进程显示不可用状态。
- Pause 暂停采样，刷新间隔可选 1/2/5 秒，F5 手动刷新。
- 选中进程后 Details 或右键 Properties：查看命令行、用户、提权状态、句柄数、模块列表和线程列表。
- 打开位置、复制路径；结束进程/进程树先在应用内确认，并核对创建时间以防 PID 被复用。

### 磁盘

1. 下拉选择盘符并点 Scan drive；也可以点盘符卡片上的扫描按钮。
2. Browse… 选择目录，或在路径框输入目录后点 Scan。
3. 扫描时显示当前路径、已发现文件/目录数、统计容量、耗时和跳过数。普通目录遍历不知道最终文件总数，因此使用活动进度条，不伪造百分比。
4. Cancel 请求停止；等扫描工作线程结束后显示已收集的部分结果，可以重新扫描。
5. 结果上半部分是目录树/文件表与扩展名统计，下半部分是按目录分组的空间图，分割条可拖动。
6. Enter/双击目录下钻，Up/Root 返回；点击空间图文件定位其目录并选中文件。点击扩展名筛选对应文件，Clear type filter 清除。
7. Allocated / Logical 切换空间图口径。Export CSV 导出本次扫描结果；Clear index 清除内存索引。

整盘 NTFS 扫描会尝试只读 MFT 路径；缺少权限或不支持的元数据布局时自动回退目录遍历，并显示原因。**本机未获管理员权限，MFT 解码测试已通过，但管理员整盘读取尚未完成实机验收。** 普通用户扫描已验证，不需要提权才能使用。

## 构建

Go 1.27.1，MyGo 固定为 v0.3.5。无需 Bun、WebView2 或 C 工具链。

```powershell
go test ./...
go vet ./...
$env:CGO_ENABLED = "0"
go build -tags mygo_noinspector -trimpath -ldflags "-s -w -H=windowsgui" -o build/DumpingGround.exe ./cmd/dumpingground
```

默认使用 MyGo CPU 原生绘制；`--renderer=gpu` 可切换 D3D11。UI 组件与自绘空间图都使用 MyGo 公共 API。

诊断参数（可选，不影响普通双击启动）：

```powershell
.\build\DumpingGround.exe --page=disk
.\build\DumpingGround.exe --scan="D:\MyWords"
.\build\DumpingGround.exe --capture-dir="build\captures" --close-after=8s
```

截图参数只捕获本应用窗口。普通启动不会扫描磁盘、写截图或导出日志。

## 当前限制

- 尚未实现 Process Explorer 的全局句柄搜索、线程调用栈、符号解析和内核驱动能力。
- 模块/线程详情按选择加载；受保护进程仍受 Windows 权限限制。
- NTFS 快速扫描是实验性路径，复杂属性列表可能回退。扫描活动文件系统不是一致性快照。
- 重解析点和离线/云占位文件跳过，避免循环和触发下载；硬链接分配空间每次扫描计一次。
- 统计默认数据流；备用数据流、NTFS 元数据、卷影副本等会导致与卷已用空间不同。
- 空间图按显示预算合并很小的项目为可下钻目录块；文件表保留所有扫描条目。
- 不提供自动删除；可以定位到资源管理器自行处理。
- 低内存仍是目标；十分钟基线发现增长，性能优化已按当前优先级暂停，不能声称长期占用已达标。

## 文档

- [开发设计与提交规则](docs/development.md)
- [实际功能验收](docs/acceptance.md)
- [历史性能记录](docs/performance.md)
- [MyGo 复用与上游候选](docs/mygo-integration.md)

使用 `type(scope): imperative summary` 提交消息。一项功能或修复一个提交；构建产物不入库。
