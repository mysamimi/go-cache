package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestShardedCache_RedisIntegration(t *testing.T) {
	rdb := getRedisClient(t)
	// 4 shards, default exp 5m, cleanup 10m
	sc := NewShardedCache[RedisTestStruct](4, 5*time.Minute, 10*time.Minute)
	sc.WithRedis(rdb)

	ctx := context.Background()
	key := "sharded_test_key"
	val := RedisTestStruct{Name: "bar", Value: 100}

	// 1. Set in Sharded Cache -> Should go to Redis async
	sc.Set(key, val, 0)
	time.Sleep(200 * time.Millisecond) // Wait for async write

	// Verify in Redis
	data, err := rdb.Get(ctx, key)
	if err != nil {
		t.Fatalf("Expected key in Redis, got error: %v", err)
	}
	var res RedisTestStruct
	json.Unmarshal(data, &res)
	if res != val {
		t.Errorf("Redis value mismatch. Got %v, want %v", res, val)
	}

	// 2. Flush Local Cache
	sc.Flush()
	// Flush clears all shards.
	if sc.ItemCount() != 0 {
		t.Errorf("Expected 0 items after Flush, got %d", sc.ItemCount())
	}

	// 3. Get from Sharded Cache -> Should fallback to Redis
	got, found := sc.Get(key)
	if found != Found {
		t.Errorf("Expected to find item via Redis fallback")
	}
	if got != val {
		t.Errorf("Got %v, want %v", got, val)
	}
}

func TestShardedCache_Capacity(t *testing.T) {
	// 2 shards. Total capacity 10. => 5 per shard.
	sc := NewShardedCache[int](2, NoExpiration, 0)
	sc.WithCapacity(10)

	capacity := 10
	itemsToAdd := 30

	// Add enough items to likely trigger eviction in at least one shard.
	for i := 0; i < itemsToAdd; i++ {
		sc.Set(fmt.Sprintf("key-%d", i), i, 0)
	}

	count := sc.ItemCount()
	t.Logf("Total items after adding %d: %d", itemsToAdd, count)

	// Max allowed per shard is 5.
	// Target per shard after eviction is 75% of 5 = 3.
	// After adding one item, could be 4.
	// So max per shard is 5.
	// Total max = 5 * 2 = 10.

	if count > 10 {
		t.Errorf("Expected eviction to keep size low. Total capacity %d, got %d items", capacity, count)
	}

	if count == itemsToAdd {
		t.Errorf("No eviction happened. Got %d items", count)
	}
}

func TestShardedModifyNumeric_Redis(t *testing.T) {
	rdb := getRedisClient(t)
	sc := NewShardedCache[int](2, 5*time.Minute, 10*time.Minute)
	sc.WithRedis(rdb)
	numericSc := &ShardedNumericCache[int]{ShardedCache: sc}

	ctx := context.Background()
	key := "numeric_redis_key"
	val := 10

	// 1. Set initial value
	sc.Set(key, val, 0)
	time.Sleep(100 * time.Millisecond)

	// 2. Modify Numeric (Increment)
	newVal, err := numericSc.ModifyNumeric(key, 5, true)
	if err != nil {
		t.Fatalf("ModifyNumeric failed: %v", err)
	}
	if newVal != 15 {
		t.Errorf("Expected 15, got %d", newVal)
	}

	time.Sleep(100 * time.Millisecond) // Wait for async write

	// 3. Verify in Redis
	data, err := rdb.Get(ctx, key)
	if err != nil {
		t.Fatalf("Expected key in Redis, got error: %v", err)
	}
	var res int
	json.Unmarshal(data, &res)
	if res != 15 {
		t.Errorf("Redis value mismatch. Got %d, want 15", res)
	}

	// 4. Modify Numeric (Decrement)
	newVal, err = numericSc.ModifyNumeric(key, 3, false)
	if err != nil {
		t.Fatalf("ModifyNumeric failed: %v", err)
	}
	if newVal != 12 {
		t.Errorf("Expected 12, got %d", newVal)
	}

	time.Sleep(100 * time.Millisecond) // Wait for async write

	// 5. Verify in Redis
	data, err = rdb.Get(ctx, key)
	if err != nil {
		t.Fatalf("Expected key in Redis, got error: %v", err)
	}
	json.Unmarshal(data, &res)
	if res != 12 {
		t.Errorf("Redis value mismatch. Got %d, want 12", res)
	}
}

func TestShardedCache_GracefulShutdown(t *testing.T) {
	rdb := getRedisClient(t)
	sc := NewShardedCache[string](4, 5*time.Minute, 10*time.Minute)
	sc.WithRedis(rdb)

	key := "shutdown_key"
	val := "shutdown_val"

	// 1. Set item
	sc.Set(key, val, 0)

	// 2. Immediately Close (should wait for async write)
	sc.Close()

	// 3. Verify in Redis
	ctx := context.Background()
	data, err := rdb.Get(ctx, key)
	if err != nil {
		t.Fatalf("Expected key in Redis after graceful shutdown, got error: %v", err)
	}
	var res string
	json.Unmarshal(data, &res)
	if res != val {
		t.Errorf("Redis value mismatch. Got %s, want %s", res, val)
	}
}

func TestShardedCache_Sync(t *testing.T) {
	rdb := getRedisClient(t)
	sc := NewShardedCache[string](4, 5*time.Minute, 10*time.Minute)
	sc.WithRedis(rdb)

	key := "sync_key"
	val := "initial_val"
	newVal := "updated_in_redis"

	// 1. Set initial value
	sc.Set(key, val, 0)
	time.Sleep(100 * time.Millisecond)

	// 2. Update directly in Redis (simulating another service)
	rdb.Set(context.Background(), key, "\""+newVal+"\"", 0)

	// 3. Verify Local Cache has old value
	got, found := sc.Get(key)
	if found != Found || got != val {
		t.Errorf("Expected old value, got %s", got)
	}

	// 4. Call Sync
	gotSync, err := sc.Sync(key)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if gotSync != newVal {
		t.Errorf("Sync returned %s, want %s", gotSync, newVal)
	}

	// 5. Verify Local Cache updated
	got, found = sc.Get(key)
	if found != Found || got != newVal {
		t.Errorf("Expected new value after Sync, got %s", got)
	}
}
