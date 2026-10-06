# intervalres

并发安全的内存区间预约管理器（Go，仅标准库）。每个预约包含唯一非空字符串 ID
和半开区间 [Start, End)（要求 Start < End）。不同预约不允许重叠，首尾相接
（如 [0,10) 与 [10,20)）是允许的。

## 设计

- 内部维护按 Start 升序的有序切片，查询与重叠检查走二分查找，不会在每次
  查询时重新排序。
- 所有写操作（Reserve / Replace / Release）在同一把写锁内完成"重叠检查 +
  写入"，冲突的并发预约不可能同时成功；读操作（Get / Find / Snapshot /
  Len）使用读锁。
- Replace 是原子的：检查重叠时忽略自身，失败时原预约保持不变。
- Snapshot 返回按 Start 升序（Start 唯一，亦满足 ID 字典序次序）的值拷贝，
  与内部状态隔离。

## API

| 方法 | 说明 |
| --- | --- |
| NewManager() | 创建空管理器 |
| Reserve(id, start, end) error | 新增预约；失败返回 ErrEmptyID / ErrInvalidRange / ErrDuplicateID / ErrOverlap |
| Replace(id, start, end) error | 原子修改已有预约区间；失败返回 ErrInvalidRange / ErrNotFound / ErrOverlap，原预约不变 |
| Release(id) error | 按 ID 删除；不存在返回 ErrNotFound |
| Get(id) (Reservation, bool) | 按 ID 查询 |
| Find(point) (Reservation, bool) | 返回包含 point 的预约 |
| Snapshot() []Reservation | 按 Start 升序返回全部预约的拷贝 |
| Len() int | 当前预约数量 |

重叠错误为 *OverlapError（errors.Is 匹配 ErrOverlap），其中 Conflict 字段
给出冲突的已有预约。

## 示例

```go
m := intervalres.NewManager()
err := m.Reserve("a", 0, 10)          // ok
err = m.Reserve("b", 10, 20)          // ok，首尾相接
err = m.Reserve("c", 5, 15)           // ErrOverlap，Conflict 为 "a"
err = m.Replace("b", 20, 30)          // ok
r, ok := m.Find(25)                   // "b" [20, 30)
err = m.Release("a")                  // ok
snap := m.Snapshot()                  // ["b" [20, 30)]
```

## 运行

```sh
go run ./cmd/demo        # 内置区间演示 Reserve/重叠拒绝/Replace/Find/Release/Snapshot
go test -race ./...      # 运行全部测试（含并发冲突场景）
```

## 目录

- manager.go：库实现（package intervalres）
- cmd/demo/main.go：最小 CLI 演示
- manager_test.go：自动化测试
