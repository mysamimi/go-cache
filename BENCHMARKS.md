# go-cache Performance Benchmarks

This document provides performance metrics for the `go-cache` library. Benchmarks were conducted on a high-performance system to establish baseline expectations.

## System Specifications

- **OS**: macOS (Darwin)
- **Arch**: arm64
- **CPU**: Apple M3 Max
- **Go Version**: 1.2x

## Benchmark Results

| Benchmark | Iterations | Time (ns/op) | Memory (B/op) | Allocs (allocs/op) |
|-----------|------------|--------------|---------------|-------------------|
| **Local Cache** | | | | |
| `Cache.Set` | 78,995,493 | 15.19 | 0 | 0 |
| `Cache.Get` | 167,563,831 | 7.15 | 0 | 0 |
| `ShardedCache.Set` (Parallel) | 21,197,948 | 56.81 | 35 | 2 |
| `ShardedCache.Get` (Parallel) | 63,926,983 | 19.51 | 13 | 1 |
| `NumericCache.Incr` | 45,319,717 | 25.78 | 0 | 0 |
| **Set Cache** | | | | |
| `SetCache.AddMember` | 210,420 | 5,733.00 | 5,474 | 5 |
| `SetCache.Count` (100 members) | 320,437 | 3,428.00 | 0 | 0 |
| **Redis Integrated** | | | | |
| `Redis.Set` (Async Write) | 47,962,658 | 22.05 | 0 | 0 |
| `Redis.Get` (Fallback/Network) | 3,790 | 272,521.00 | 1,024 | 17 |
| `RedisNumeric.Incr` (with Sync) | 39,642,016 | 29.91 | 0 | 0 |

## Analysis

### 1. Local Cache Efficiency
The standard `Cache` operations are extremely fast, measuring around **7-15 nanoseconds** per operation with zero allocations. This makes it ideal for high-frequency access patterns on single instances.

### 2. Sharding Overhead vs. Scale
While `ShardedCache` shows higher latency for small benchmarks due to hashing and bucket selection, it is recommended for high-concurrency environments to eliminate lock contention on the global map.

### 3. Redis Synchronization
With the new synchronization logic, `RedisNumeric.Incr` remains highly efficient (~30ns) because it only performs a network read when necessary. The `Redis.Get` fallback demonstrates the expected network latency (~0.27ms) when data must be fetched from an external Redis instance.

### 4. SetCache Complexity
`SetCache.AddMember` is the most expensive operation (~5.7µs) because it involves deep-copying the member map to ensure thread safety and consistency during modifications. This is a tradeoff for the rich feature set (per-member TTLs).

---
*Note: Benchmarks were run using `go test -bench=. -benchmem`. Actual performance may vary depending on hardware, network latency to Redis, and workload characteristics.*
