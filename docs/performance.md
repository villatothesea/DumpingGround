# 性能基线

> 当前优先级：功能验收。短时内存数据不能作为长期达标结论。下面历史“私有内存”是 PrivateMemorySize64（私有提交量），不是私有工作集。

## 2026-10-09 持续运行结果

CPU 原生绘制下 600 秒进程页采样：工作集从约 48.1 MiB 增至约 103.6 MiB，私有提交从约 67.7 MiB 增至约 120.4 MiB。第 2–7 分钟工作集一度约 71–72 MiB，第 8 分钟出现台阶增长。采样没有控制窗口大小及用户交互，所以尚不能归因于泄漏或框架缓存。

中途使用 PROCESS_MEMORY_COUNTERS_EX2 单次测量：私有工作集约 42.2 MiB，总工作集约 70.9 MiB，私有提交约 89.8 MiB。此单点不是稳定上限。

脚本：`scripts/Measure-Memory.ps1`、`scripts/Get-ProcessMemory.ps1`。原始采样在 `build/memory-10min.json`。按用户指示暂缓性能调优，保留 50 MiB 私有工作集目标与未解决项。

## 早期构建记录

记录时间：2026-10-09  
工具链：Go 1.27.1、MyGo 0.3.5、`CGO_ENABLED=0`  
目标：Windows amd64  
机器：当前开发机，发布构建使用 `-trimpath -ldflags="-s -w -H=windowsgui"`

## 当前结果

| 项目 | 结果 | 方法 |
|---|---:|---|
| 发布 EXE | 约 12 MB | `GOOS=windows GOARCH=amd64 go build ...` |
| 单元测试 | 通过 | `go test ./...` |
| 静态检查 | 通过 | `go vet ./...` |
| 启动检查 | 通过 | 启动 4 秒，进程未退出 |
| 外部 helper/WebView2 子进程 | 未发现 | 启动后检查 Win32 子进程 |

## 内存结果

| 场景 | 私有内存 | 工作集 | 方法 |
|---|---:|---:|---|
| DumpingGround 100ms | 56 MB | 36 MB | 窗口刚启动，进程首帧尚未完成 |
| DumpingGround 4s | 98 MB | 82 MB | 进程快照和表格首帧完成 |
| MyGo minimal native baseline 100ms | 76 MB | 55 MB | 同尺寸 1280×800 空窗口 |
| MyGo minimal native baseline 4s | 91 MB | 121 MB | 同尺寸 1280×800 空窗口 |

测量命令通过 PowerShell 启动发布 EXE，分别在 100ms 和 4s 读取 `PrivateMemorySize64` 与 `WorkingSet64`。MyGo 空窗口基线也用同一工具链、同一窗口尺寸测量。当前 DumpingGround 在进程首帧后的私有内存比空窗口基线高约 7 MB；工作集低于该次空窗口基线。不同机器的 D3D、字体和桌面合成器行为会改变绝对值，因此后续优化以同机前后对比为准。

## 后续采样

- 只打开进程页，稳定刷新 10 分钟。
- 磁盘页扫描一个已知目录，记录峰值和扫描完成后的回落值。
- 取消扫描并切回进程页，确认索引释放。
- 最小化窗口和恢复窗口，确认刷新与绘制策略。

下一次性能优化提交必须在这里追加前后数据、采样命令和测试目录规模。
