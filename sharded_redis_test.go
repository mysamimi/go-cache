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
