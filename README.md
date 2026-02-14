# go-cache

![Go](https://github.com/mysamimi/go-cache/actions/workflows/test.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/mysamimi/go-cache/v3)](https://goreportcard.com/report/github.com/mysamimi/go-cache/v3)
[![Go Reference](https://pkg.go.dev/badge/github.com/mysamimi/go-cache/v3.svg)](https://pkg.go.dev/github.com/mysamimi/go-cache/v3)

go-cache is an in-memory key:value store/cache similar to memcached that is suitable for applications running on a single machine. Its major advantage is that, being essentially a thread-safe `map[string]interface{}` with expiration times, it doesn't need to serialize or transmit its contents over the network.

**Key Features:**

*   **Sharding**: Reduces lock contention for high-concurrency workloads.
*   **Redis Integration**: Optional L2 caching and persistence layer using `go-redis` (supports v8 and v9).
*   **Capacity Management**: Internal LRU-like eviction when memory limits are reached.
*   **Generics**: Type-safe API (Go 1.18+).
*   **Numeric Operations**: Atomic increment/decrement support for numeric types, persisted to Redis.
*   **Graceful Shutdown**: Ensures pending Redis operations are completed before exit.
*   **Sync**: Force refresh items from Redis.

### Installation

`go get github.com/mysamimi/go-cache/v3`

### Usage

#### Basic Cache

```go
import (
	"fmt"
	"github.com/mysamimi/go-cache/v3"
	"time"
)

func main() {
	// Create a cache with a default expiration time of 5 minutes, and which
	// purges expired items every 10 minutes
	c := cache.New[string](5*time.Minute, 10*time.Minute)

	// Set the value of the key "foo" to "bar", with the default expiration time
	c.Set("foo", "bar", cache.DefaultExpiration)

	// Get the string associated with the key "foo" from the cache
	foo, found := c.Get("foo")
	if found == cache.Found {
		fmt.Println(foo)
	}
}
```

#### Sharded Cache (Recommended for High Concurrency)

`ShardedCache` automatically partitions keys into multiple buckets to reduce lock contention.

```go
// Create a sharded cache with 16 shards
c := cache.NewShardedCache[string](16, 5*time.Minute, 10*time.Minute)

c.Set("foo", "bar", 0)
val, found := c.Get("foo")
```

#### Redis Integration (L2 Cache & Persistence)

You can configure a Redis client to act as a Level 2 cache and persistent store. Writes (Set/Delete/ModifyNumeric) are asynchronous to Redis, while reads look up Redis if the key is missing from memory.

Supports both `go-redis/v8` and `go-redis/v9` via adapters.

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/mysamimi/go-cache/v3"
    redisv9 "github.com/mysamimi/go-cache/v3/redis/v9"
)

func main() {
    // 1. Initialize Redis Client
    rdb := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })

    // 2. Create Cache
    c := cache.NewShardedCache[MyStruct](16, 5*time.Minute, 10*time.Minute)

    // 3. Enable Redis Integration with Adapter
    c.WithRedis(redisv9.New(rdb))
    
    // 4. Use Cache
    // This will asynchronously write to Redis
    c.Set("key", MyStruct{Val: 1}, 0)
    
    // 5. Graceful Shutdown
    // Ensures all pending Redis writes are flushed before exit
    defer c.Close()
}
```

#### Sync from Redis

If you know an item has changed in Redis (e.g. by another service) and want to refresh the local cache immediately:

```go
val, err := c.Sync("key")
```

#### Capacity & Aviction

Set a maximum number of items for the local cache. When the limit is reached, items are evicted (approx 25% of cache is cleared) to prevent thrashing.

```go
// Limit local cache to 1000 items (distributed across shards)
c.WithCapacity(1000)

// Register a callback for when items are evicted (due to expiry or capacity)
c.OnEvicted(func(k string, v MyStruct) {
    fmt.Printf("Item %s evicted\n", k)
})
```

#### Numeric Operations

For counters and numeric values, use `NumericCache` (or `ShardedNumericCache`). Operations are atomic and persisted to Redis.

```go
// Create a Sharded Numeric Cache for int64
nc := cache.NewShardedNumeric[int64](16, 5*time.Minute, 10*time.Minute)
nc.WithRedis(redisv9.New(rdb))

// Increment "counter" by 1
newVal, err := nc.ModifyNumeric("counter", 1, true)

// Decrement "counter" by 5
newVal, err = nc.ModifyNumeric("counter", 5, false)
```
