I forked this project from github.com/patrickmn/go-cache and modified this
I want to be able to increment/decriment and if not found set it in my case
# go-cache
![Go](https://github.com/mysamimi/go-cache/actions/workflows/test.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/mysamimi/go-cache/v3)](https://goreportcard.com/report/github.com/mysamimi/go-cache/v3)
[![Go Reference](https://pkg.go.dev/badge/github.com/mysamimi/go-cache/v3.svg)](https://pkg.go.dev/github.com/mysamimi/go-cache/v3)

go-cache is an in-memory key:value store/cache similar to memcached that is
suitable for applications running on a single machine. Its major advantage is
that, being essentially a thread-safe `map[string]interface{}` with expiration
times, it doesn't need to serialize or transmit its contents over the network.

Any object can be stored, for a given duration or forever, and the cache can be
safely used by multiple goroutines.

Although go-cache isn't meant to be used as a persistent datastore, the entire
cache can be saved to and loaded from a file (using `c.Items()` to retrieve the
items map to serialize, and `NewFrom()` to create a cache from a deserialized
one) to recover from downtime quickly. (See the docs for `NewFrom()` for caveats.)

### Installation

`go get github.com/mysamimi/go-cache/v3`

### Usage

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

	// Set the value of the key "baz" to "yes", with no expiration time
	// (the item won't be removed until it is re-set, or removed using
	// c.Delete("baz")
	c.Set("baz", "yes", cache.NoExpiration)

	// Get the string associated with the key "foo" from the cache
	foo, found := c.Get("foo")
	if found == cache.Found {
		fmt.Println(foo)
	}
}
```

### Redis Integration (L2 Cache)

You can configure a Redis client to act as a Level 2 cache and persistent store. Writes are asynchronous to Redis, while reads fallback to Redis if the key is missing from memory.

```go
import "github.com/redis/go-redis/v9"

rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

c := cache.New[MyStruct](5*time.Minute, 10*time.Minute)
c.WithRedis(rdb)
```

### Capacity & Eviction

You can set a maximum number of items for the local in-memory cache.

```go
c.WithCapacity(1000)
```

**Bulk Eviction Strategy**: When the cache reaches the configured capacity, it automatically triggers a bulk eviction, removing random items until the cache size drops to **75%** of the capacity. This prevents "thrashing" (constant delete/add cycles) under heavy load.


### ShardedCache

For high concurrency scenarios, you can use `ShardedCache` to reduce lock contention. `ShardedCache` automatically partitions keys into multiple buckets.

```go
// Create a sharded cache with 16 shards
c := cache.NewShardedCache[string](16, 5*time.Minute, 10*time.Minute)

// ShardedCache also supports Redis and Capacity configuration
c.WithRedis(rdb)

// Capacity is distributed across shards (e.g. 1000 total / 16 shards)
c.WithCapacity(1000)

c.Set("foo", "bar", 0)
val, found := c.Get("foo")
```
