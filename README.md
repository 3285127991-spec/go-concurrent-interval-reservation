# intervalreservation

并发安全的内存区间预约管理器（Go，零第三方依赖）。每个预约是一个带唯一
非空 ID 的半开区间 `[Start, End)`（要求 `Start < End`）；不同预约互不重叠，
首尾相接（如 `[0,10)` 与 `[10,20)`）允许共存。

## 功能

- `Reserve(r)`：新增预约；ID 重复、区间非法或与现有预约重叠时返回明确错误，状态不变。
- `Replace(id, start, end)`：原子修改已有预约的区间；重叠检查忽略自身；失败时原预约保持不变。
- `Release(id)`：按 ID 删除预约。
- `Get(id)` / `Find(point)`：按 ID 查询 / 查找包含某点（`Start <= point < End`）的预约。
- `Snapshot()`：按 Start 升序、再按 ID 字典序返回副本，与内部状态完全隔离。
- `Len()`：当前预约数量。

错误通过哨兵值区分，可用 `errors.Is` 判断：`ErrEmptyID`、`ErrInvalidRange`、
`ErrDuplicateID`、`ErrOverlap`（由 `*OverlapError` 包装，携带冲突的预约）、`ErrNotFound`。

## 并发与实现

- 单个 `sync.RWMutex` 保护全部状态：重叠检查与写入在同一把写锁内完成，
  两个互相冲突的区间不可能同时预约成功；查询走读锁。
- 预约保存在按 (Start, ID) 排序的切片中（区间互不相交，End 也随之递增），
  重叠检查与 `Find` 均为二分查找 O(log n)，无需每次查询重新排序；
  另有 `map[string]Reservation` 支撑 O(1) 的 `Get`/`Len`。

## 使用

```go
m := intervalreservation.NewManager()
err := m.Reserve(intervalreservation.Reservation{ID: "a", Start: 0, End: 10})
err = m.Replace("a", 5, 15)
r, ok := m.Find(7)
err = m.Release("a")
```

## 运行演示与测试

```sh
go run ./cmd/demo     # 最小 CLI 演示：Reserve/重叠拒绝/Replace/Find/Release/Snapshot
go test -race ./...   # 全部测试，含并发冲突场景
```
