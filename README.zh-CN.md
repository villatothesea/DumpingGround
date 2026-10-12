# DumpingGround

[English](README.md)

Windows 原生维护工具，Go + [MyGo](https://github.com/egoist/mygo) 原生 UI——
进程查看、磁盘空间分析，以及**安全**清理（Rust 构建产物、Windows 垃圾、
带残留审查的彻底卸载）。以 Process Explorer / WizTree 的常用工作流为目标，
仍有功能差距。

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

## 运行

Releases 下载 `DumpingGround.exe`，或按下方构建。双击即用——无安装、普通
使用无需管理员。再次启动会激活已有窗口。

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
