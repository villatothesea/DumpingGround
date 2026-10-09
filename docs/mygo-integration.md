# MyGo 复用与上游候选

依赖固定为 `github.com/egoist/mygo v0.3.5`，未 fork 或改写框架。

## 已复用

- 窗口、生命周期、单实例、窗口激活、原生文件/目录对话框。
- `ui.Table` / `ui.OutlineTable`：进程列表/树、模块、线程、磁盘目录、文件类型统计。
- `ui.Split` / `ui.SplitVertical`：磁盘结果与空间图的可拖拽分区。
- `ui.SearchField`、`ui.Select`、`ui.Checkbox`、按钮、进度条、确认对话框和原生右键菜单。
- MyGo clipboard/shell：复制文本、定位到资源管理器。
- `ui.Tester`：实际点击、键盘选择、缩放、多次布局、上下文菜单等交互验收。
- `Window.CapturePage`：原生窗口绘制结果诊断。

## 自绘部分

Treemap 算法在 `internal/treemap`，不依赖 UI 或 Windows。UI 层使用单个 MyGo `ui.Box` 的 `Draw` / `PointerPosition` / `Tooltip`，统一使用 MyGo 坐标与裁剪，不为每个文件创建控件。树形布局按目录分组，显示预算内绘制，小项目保留为可下钻目录块。

Windows 采集、文件系统扫描、NTFS 解码是产品数据层，不放到 MyGo 框架。

## 可考虑提交上游的内容

通用层级 treemap 组件：只接受 ID、权重和 children 回调，通过已有 Painter 绘制；必须补齐键盘导航、无障碍节点、可配置配色和跨平台视图测试后才适合上游 PR。当前项目版本尚未满足这个标准，不宣称已经可提交。

低内存基线与潜在框架回归必须先有最小复现及同版本对照数据；业务采样的分配问题不得直接归因到框架。

## 开发规则

使用 frame 内 `ui.Element`，不跨帧存储元素。持久化状态使用 `ui.ListState` / `ui.OutlineState`。后台工作通过有界 channel 移交结果，UI 主线程修改视图状态，窗口关闭取消工作。
