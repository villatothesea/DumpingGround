# DumpingGround

[English](README.md)

Windows 原生维护工具，Go + [MyGo](https://github.com/egoist/mygo) 原生 UI——
实时硬件总览、进程查看、磁盘空间分析、启动项管理，以及**安全**清理（Rust
构建产物、Windows 垃圾、带残留审查的彻底卸载）。以 Task Manager / Process
Explorer / WizTree / Autoruns 的常用工作流为目标。

## 安全模型

这个应用会删东西，所以规则先行：

- **默认可逆**。能进回收站的一律进回收站。Windows 会静默绕过回收站的情况
  （单项超容量、回收站被禁用、盘不支持回收）——原地保留并报错，绝不降级成
  永久删除。
- **显式确认**。Clean 要点两次。注册表残留先导出 `.reg` 备份（UTF-16、
  类型保真），备份没写成功就不删。
- **不静默扩大范围**。删除时复查守卫：运行中的进程、重解析点、被其他已装
  应用占用的路径、当前 Rust 工具链、进行中的 Windows 服务。
- **理由可见**。每条残留都显示路径和判定原因；猜测项一律不预选。
- **开关而非删除**。关掉启动项从不移除它的条目；Windows 自带的服务和任务
  只读。

## 运行

Releases 下载 `DumpingGround.exe`，或按下方构建。双击后在 UAC 弹窗点"是"——
应用以管理员权限运行，和 WizTree、BCUninstall 一个模式。再次启动会激活
已有窗口。

### 权限

启动时一次性要管理员——NTFS 日志、MFT 快扫、HKLM 残留删除和系统目录
清理都依赖它，所以开头问一次，而不是在每个操作中途打断。状态栏的
`⛨ admin` 徽标显示当前状态。

即便提权运行，应用也不启用 `SeDebugPrivilege`：内核保护的进程（csrss、
lsass、杀毒软件）一律够不着——杀掉它们会直接蓝屏，而读取 lsass 内存
正是凭证窃取工具的特征动作。禁区边界交给内核画，是特性不是缺陷。

### 命令行

- `--page=computer|processes|disk|startup`——启动时打开的页面（默认
  `computer`）。
- `--scan=<盘符或文件夹>`——打开磁盘页并开始扫描。
- `--renderer=cpu|gpu`——软件绘制（默认）或 D3D11。

页面快捷键：Ctrl+1 Computer、Ctrl+2 Processes、Ctrl+3 Disk Usage、
Ctrl+4 Uninstall、Ctrl+5 Startup。

### 计算机

启动后默认打开的页面（Ctrl+1；Ctrl+2–5 依次为其他页签）。

- 顶部是整机身份：计算机名、品牌型号、Windows 版本与内部版本号、处理器、
  内存、显卡、存储、网络、主板和 BIOS——数据来自 SMBIOS、注册表和 DXGI。
- 每个硬件一张实时卡片：CPU、内存、每块 GPU、每块磁盘、每个已连接的网卡，
  笔记本还有电池。卡片显示当前数值和滚动 60 秒的实时图表。
- 点击卡片进入详情页：大图表、实时指标和完整规格——每个逻辑处理器的曲线与
  缓存大小、内存条（插槽、容量、类型、速度、料号）、GPU 各引擎与显存、磁盘
  活动时间/吞吐/卷、网卡地址与链路速率。按 Back、Esc、Backspace 或鼠标
  侧键返回；再点一次 Computer 页签也会回到总览。
- 不安装任何驱动、不写入任何内容：数据源是 SMBIOS、`NtQuerySystemInformation`、
  性能计数器（PDH）、磁盘性能 IOCTL 和 IP Helper API。不显示温度和风扇转速
  ——Windows 只通过厂商驱动才暴露这些。

### 进程

- 默认平铺全部进程，勾选 Tree 切换父子树；进程数始终显示在顶部。
- 搜索名称、PID 或路径；点击表头排序。
- CPU、工作集、私有提交量、读写速率和线程数；无法访问的进程显示不可用状态。
- Pause 暂停采样，刷新间隔可选 1/2/5 秒，F5 手动刷新。
- 选中进程后 Details 或右键 Properties：查看命令行、用户、提权状态、句柄数、
  模块列表和线程列表。
- 打开位置、复制路径；结束进程/进程树先在应用内确认，并核对创建时间以防 PID
  被复用。

### 磁盘

1. 下拉选择盘符并点 Scan drive；也可以点盘符卡片上的扫描按钮。
2. Browse… 选择目录，或在路径框输入目录后点 Scan。
3. 扫描时显示实时计数；Cancel 保留已收集的部分结果。
4. 结果上半部分是目录树/文件表与扩展名统计，下半部分是空间图，分割条可拖动。
5. Enter/双击下钻，Up/Root 返回；点击空间图文件定位到表中。
6. Allocated / Logical 切换口径；Export CSV 导出；Clear index 清除内存索引。

整盘 NTFS 扫描会尝试只读 MFT 路径，失败回退目录遍历并显示原因。严格对照验证
使用单独的 `DumpingGround-verify.exe`，失败标 `INCONCLUSIVE`，不冒充通过。

### 清理

- Rust 构建产物（预设分档：target 目录、cargo 缓存；当前工具链和
  settings/overrides 引用的项目受保护）。
- Windows 垃圾（临时文件、缓存、可选 Downloads），带龄期和占用检查；
  浏览器运行时跳过其缓存。
- 回收站本身永远不在任何预设里。

### 卸载

- 启动软件自带的卸载程序（需要时提权），然后盯注册表条目——条目真消失才
  弹残留审查，中途取消卸载 = 什么都没发生。
- 残留扫描覆盖文件、启动项、注册表键；每条标 Confirmed（有证据）或
  Likely（名字匹配，从不预选）。
- 注册表删除先备份；文件一律走回收站。

### 启动项

所有会自动启动的东西，一张列表，每项一个开关：

- **应用**——`Run` 注册表项（当前用户、所有用户、32 位）和两个启动文件夹。
- **服务**——自动和延迟自动启动的服务，以及已被关掉的。
- **计划任务**——在登录或开机时触发的任务。

每行显示发布者（取自程序自带的版本信息）、此刻是否在运行、完整命令。
选中一行会说明它配置在哪、开关会产生什么效果。

不删除任何东西，每个开关都能撤销：

- 应用用 `StartupApproved` 标志——Task Manager 用的同一机制——`Run` 值或
  快捷方式原地不动。
- 计划任务通过任务计划程序本身启用/禁用。
- 服务改启动类型；原值（自动、是否延迟）会被记住，打开开关时原样恢复。
  关服务前会先确认；正在运行的服务会一直运行到下次开机。
- Windows 自带的服务和任务默认隐藏，显示时只读，按"谁做的程序"判定而不是
  看路径——`System32\DriverStore` 下的厂商驱动服务仍可操作。
- 每次改动都写进应用日志。

## 构建

Go 1.27+，MyGo 固定 v0.3.5。无需 Bun、WebView2 或 C 工具链。

```powershell
go test ./...
$env:CGO_ENABLED = "0"
go build -tags mygo_noinspector -trimpath -ldflags "-s -w -H=windowsgui" -o build/DumpingGround.exe ./cmd/dumpingground
```

默认 CPU 绘制；`--renderer=gpu` 切换 D3D11。

## 文档

- [开发设计与提交规则](docs/development.md)
- [实际功能验收](docs/acceptance.md)
- [诊断能力：MFT 验证、句柄搜索、线程调用栈](docs/diagnostics.md)
- [MFT 严格验证记录](docs/mft-validation.md)

## License

MIT — 见 [LICENSE](LICENSE)。未来可能转 Apache-2.0；贡献按
[CONTRIBUTING.md](CONTRIBUTING.md) 的条款接收。
