# MFT 并行读取实验记录

> 结论先行：**不采用，已回滚**。串行读取在 NVMe 上已打满带宽，并行化无实质收益，反而引入"静默回退"型风险。本文记录完整过程以备复查。
> 实验起点（回滚点）：`8996161 report MFT index-build phases so the scan tail does not look idle`

## 动机

磁盘页"假卡死"排查时发现一个数字：冷缓存下串行读 2.48 GB MFT 流耗时约 **31.76 秒（约 78 MB/s）**。当时判断串行 `io.MultiReader` + `ReadFull` 是队列深度 1 的同步读，可能是瓶颈，决定试一次并行定位读（`ReadAt` 多 goroutine 并发拉取 chunk）。

实验前提（用户要求）：先把前期所有工作提交为回滚点，实验代码单独验证，不得静默替换可靠路径。

## 实现（已回滚）

- 新增 `mftSeg`：把 MFT 逻辑流偏移映射到物理卷偏移（run list → 分段表）。
- 新增 `mftStream`：既实现串行 `io.Reader`，又暴露 `readChunkAt(off, buf)` 定位读。
- `scanMFTRecords` 类型断言：支持定位读的流走并行路径（最多 8 个读 goroutine，原子计数器分发 chunk，解码 worker 不变）；不支持的走原串行路径兜底。
- 进度上报 `report` 加互斥锁（多读 goroutine 并发调用）。

## 过程中发现的 Bug

第一版 chunk 分发写的是 `i := next.Add(1)`——`Add` 返回加后值，**chunk 0（含根记录 5）永远没人读**。症状不是报错，而是：MFT 快照缺根 → `fastVolumeScan` 返回错误 → `Scan()` **静默回退**到逐目录遍历全盘，跑了几分钟、吃 ~2.9 GB 内存，从外面看像"扫描卡死"。

这个 bug 本身值得记录：并行路径的失败模式很坏——不报错，只是悄悄慢一个数量级。

## 实测数据（本机，C: 卷，160 万+ 记录）

### 正确性对照（同一卷，串行 vs 并行各扫一遍）

files / dirs / nodes 完全一致；bytes / allocated 有 KB 级差异——两次扫描间隔数秒内活卷文件在变动（churn），非数据损坏。

### 性能对照

| 场景 | 串行 QD1 | 并行 QD8 |
|---|---:|---:|
| `FILE_FLAG_NO_BUFFERING` 无缓冲直读（绕过页缓存，必打盘） | 4.28 s · **607 MB/s** | 4.01 s · **649 MB/s** |
| 完整 `fastVolumeScan`（standby 已 purge） | 4.13 s | 4.60 s |

**关键事实**：本机是 PCIe 4.0 NVMe SSD，4 MB 大块顺序读在队列深度 1 时就把带宽吃满了。QD8 只多 ~7%，放进完整扫描里被解码/建索引的 CPU 时间完全淹没——并行扫描反而略慢（调度开销，噪声范围内）。

### 当初的 31.76 秒是怎么回事

无法复现。standby purge（`NtSetSystemInformation` / `MemoryPurgeStandbyList`，需 `SeProfileSingleProcessPrivilege`）之后 MFT 页仍在缓存（NTFS 把 `$MFT` 页钉在 active 而非 standby，purge 清不掉）。判断那次慢读是开机初期磁盘被其它进程争抢的瞬态，不是串行读的结构性问题。

## 为什么回滚而不保留

1. **无收益**：NVMe 上 QD1 即满带宽；HDD 上乱序并行读反而增加寻道，可能更差。
2. **复杂度换风险**：约百行分段映射 + 并发分发代码，失败模式是静默回退到目录遍历——错的时候不报警，只是慢。
3. **串行路径已是正确复杂度**：读不是瓶颈，解码和索引才是。

## 保留的成果

实验动机本身修了一个真问题（已含在回滚点 `8996161`）：扫描尾部"空转"几秒其实是纯 CPU/内存的索引构建，现已分阶段上报进度（`building index` → `linking tree` → `aggregating` → `sorting folders` → `indexing files`），进度条不再装死。

## 复现方式（备查）

如需重测：构建带计时的临时测试 → 提权运行（读 `\\.\C:` 需管理员）→ standby purge 通过 `NtSetSystemInformation(0x50, MemoryPurgeStandbyList=4)` 并先 `AdjustTokenPrivileges` 启用 `SeProfileSingleProcessPrivilege`；真无缓冲直读需 `FILE_FLAG_NO_BUFFERING` 打开卷且缓冲区按扇区对齐（`VirtualAlloc` 分配）。
