package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- Basic Cache Benchmarks ---

func BenchmarkCache_Set(b *testing.B) {
	c := New[int](DefaultExpiration, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set("key", i, DefaultExpiration)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	c := New[int](DefaultExpiration, 0)
	c.Set("key", 1, DefaultExpiration)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Get("key")
	}
}

func BenchmarkShardedCache_Set(b *testing.B) {
	c := NewShardedCache[int](32, DefaultExpiration, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Set(fmt.Sprintf("key-%d", i), i, DefaultExpiration)
			i++
		}
	})
}

func BenchmarkShardedCache_Get(b *testing.B) {
	c := NewShardedCache[int](32, DefaultExpiration, 0)
	for i := 0; i < 1000; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, DefaultExpiration)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = c.Get(fmt.Sprintf("key-%d", i%1000))
			i++
		}
	})
}

func BenchmarkCache_WithCapacity_Set(b *testing.B) {
	c := New[int](DefaultExpiration, 0)
	c.WithCapacity(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, DefaultExpiration)
	}
}

// --- Numeric Cache Benchmarks ---

func BenchmarkNumericCache_Incr(b *testing.B) {
	nc := &NumericCache[int]{New[int](DefaultExpiration, 0).cache}
	nc.Set("counter", 0, DefaultExpiration)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = nc.Incr("counter", 1)
	}
}

// --- Set Cache Benchmarks ---

func BenchmarkSetCache_Add(b *testing.B) {
	sc := NewSetCache(DefaultExpiration, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.AddMember("key", fmt.Sprintf("mem-%d", i%100), time.Minute, time.Minute)
	}
}

func BenchmarkSetCache_Count_100(b *testing.B) {
	sc := NewSetCache(DefaultExpiration, 0)
	for i := 0; i < 100; i++ {
		sc.AddMember("key", fmt.Sprintf("mem-%d", i), time.Minute, time.Minute)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sc.Count("key")
	}
}

// --- Redis Integrated Benchmarks ---

func BenchmarkRedisCache_Set_Async(b *testing.B) {
	rdb := getRedisClientForBench()
	if rdb == nil {
		b.Skip("Redis not available")
	}
	c := New[int](DefaultExpiration, 0)
	c.WithRedis(rdb)
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set("bench_key", i, DefaultExpiration)
	}
}

func BenchmarkRedisCache_Get_Fallback(b *testing.B) {
	rdb := getRedisClientForBench()
	if rdb == nil {
		b.Skip("Redis not available")
	}
	c := New[int](DefaultExpiration, 0)
	c.WithRedis(rdb)
	defer c.Close()

	key := "bench_fallback_key"
	c.Set(key, 123, DefaultExpiration)
	time.Sleep(100 * time.Millisecond) // Wait for async write

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Flush() // Clear local to force fallback
		_, _ = c.Get(key)
	}
}

func BenchmarkRedisNumeric_Incr_Sync(b *testing.B) {
	rdb := getRedisClientForBench()
	if rdb == nil {
		b.Skip("Redis not available")
	}
	nc := &NumericCache[int]{New[int](DefaultExpiration, 0).cache}
	nc.WithRedis(rdb)
	defer nc.Close()

	key := "bench_nc_sync"
	nc.Set(key, 0, DefaultExpiration)
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = nc.Incr(key, 1)
	}
}

// --- Helpers ---

func getRedisClientForBench() RedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil
	}
	rdb.FlushDB(ctx)
	return &RedisAdapter{client: rdb}
}
