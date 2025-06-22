package cache

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// func TestDjb33(t *testing.T) {
// }

var shardedKeys = []string{
	"f",
	"fo",
	"foo",
	"barf",
	"barfo",
	"foobar",
	"bazbarf",
	"bazbart",
	"bazbarr",
	"bazbare",
	"bazbarw",
	"bazbarq",
	"bazbara",
	"bazbarfo",
	"bazbarfs",
	"bazbarff",
	"bazbarfg",
	"bazbarfr",
	"bazbarfe",
	"bazbarfoo",
	"bazbarfoi",
	"bazbarfou",
	"bazbarfoy",
	"bazbarfog",
	"bazbarfod",
	"bazbarfoz",
	"bazbarfox",
	"bazbarfoc",
	"foobarbazq",
	"foobarbazy",
	"foobarbazu",
	"foobarbazi",
	"foobarbazo",
	"foobarbazl",
	"foobarbazk",
	"foobarbazj",
	"foobarbazh",
	"foobarbazf",
	"foobarbazs",
	"foobarbazz",
	"foobarbazqu",
	"foobarbazquu",
	"foobarbazquux",
}

func TestShardedCache(t *testing.T) {
	tc := NewShardedCache[string](13, DefaultExpiration, DefaultExpiration)
	for _, v := range shardedKeys {
		tc.Set(v, "value", DefaultExpiration)
	}
}

// Test for ShardedNumericCache
func TestShardedModifyNumeric(t *testing.T) {
	// Create a sharded numeric cache with 5 shards
	snc := NewShardedNumeric[int](5, DefaultExpiration, 0)

	// Test incrementing a non-existent key
	newVal, err := snc.ModifyNumeric("counter", 5, true)
	if err != nil {
		t.Error("Error incrementing counter:", err)
	}
	if newVal != 5 {
		t.Errorf("Expected counter to be 5, got %d", newVal)
	}

	// Test incrementing an existing key
	newVal, err = snc.ModifyNumeric("counter", 3, true)
	if err != nil {
		t.Error("Error incrementing counter:", err)
	}
	if newVal != 8 {
		t.Errorf("Expected counter to be 8, got %d", newVal)
	}

	// Test decrementing
	newVal, err = snc.ModifyNumeric("counter", 2, false)
	if err != nil {
		t.Error("Error decrementing counter:", err)
	}
	if newVal != 6 {
		t.Errorf("Expected counter to be 6, got %d", newVal)
	}

	// Test with multiple shards
	var wg sync.WaitGroup
	numKeys := 100
	wg.Add(numKeys)

	for i := 0; i < numKeys; i++ {
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", idx)
			snc.Set(key, 0, DefaultExpiration)
			for j := 0; j < 10; j++ {
				snc.ModifyNumeric(key, 1, true)
			}
		}(i)
	}

	wg.Wait()

	// Verify all keys have been incremented to 10
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		val, found := snc.Get(key)
		if found != Found {
			t.Errorf("Key %s not found", key)
			continue
		}
		if val != 10 {
			t.Errorf("Expected value for %s to be 10, got %d", key, val)
		}
	}
}

func BenchmarkShardedCacheGetExpiring(b *testing.B) {
	benchmarkShardedCacheGet(b, 5*time.Minute)
}

func BenchmarkShardedCacheGetNotExpiring(b *testing.B) {
	benchmarkShardedCacheGet(b, NoExpiration)
}

func benchmarkShardedCacheGet(b *testing.B, exp time.Duration) {
	b.StopTimer()
	tc := NewShardedCache[any](10, exp, 0)
	tc.Set("foobarba", "zquux", DefaultExpiration)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tc.Get("foobarba")
	}
}

func BenchmarkShardedCacheGetManyConcurrentExpiring(b *testing.B) {
	benchmarkShardedCacheGetManyConcurrent(b, 5*time.Minute)
}

func BenchmarkShardedCacheGetManyConcurrentNotExpiring(b *testing.B) {
	benchmarkShardedCacheGetManyConcurrent(b, NoExpiration)
}

func benchmarkShardedCacheGetManyConcurrent(b *testing.B, exp time.Duration) {
	b.StopTimer()
	n := 10000
	tsc := NewShardedCache[any](20, exp, 0)
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		k := "foo" + strconv.Itoa(i)
		keys[i] = k
		tsc.Set(k, "bar", DefaultExpiration)
	}
	each := b.N / n
	wg := new(sync.WaitGroup)
	wg.Add(n)
	for _, v := range keys {
		go func(k string) {
			for j := 0; j < each; j++ {
				tsc.Get(k)
			}
			wg.Done()
		}(v)
	}
	b.StartTimer()
	wg.Wait()
}

// Add benchmark for ShardedNumericCache ModifyNumeric
func BenchmarkShardedModifyNumericInt(b *testing.B) {
	b.StopTimer()
	sc := NewShardedNumeric[int](5, DefaultExpiration, 0)
	sc.Set("foo", 0, DefaultExpiration)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		sc.ModifyNumeric("foo", 1, true) // Increment
	}
}

// Add benchmark comparing sharded vs non-sharded numeric operations
func BenchmarkCompareShardedVsRegularModifyNumeric(b *testing.B) {
	b.Run("Sharded", func(b *testing.B) {
		sc := NewShardedNumeric[int](10, DefaultExpiration, 0)
		sc.Set("counter", 0, DefaultExpiration)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sc.ModifyNumeric("counter", 1, true)
		}
	})

	b.Run("Regular", func(b *testing.B) {
		nc := newNumericTestCache[int](nil, DefaultExpiration, 0)
		nc.Set("counter", 0, DefaultExpiration)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nc.ModifyNumeric("counter", 1, true)
		}
	})
}

func TestShardedCacheItemExpiration(t *testing.T) {
	ts := NewShardedCache[int](2, 50*time.Millisecond, 10*time.Millisecond)
	ts.OnEvicted(func(s string, i int) {
		fmt.Printf("delete %s (%d)\n", s, i)
	})
	ts.Set("short-lived", 1, DefaultExpiration)
	ts.Set("custom-expiry", 2, 20*time.Millisecond)
	ts.Set("no-expiry", 3, NoExpiration)

	// Wait for short-lived to expire but custom-expiry to still be alive
	time.Sleep(300 * time.Millisecond)

	// short-lived should be gone
	_, found := ts.Get("short-lived")
	if found == Found {
		t.Error("short-lived item should have expired")
	}

	// custom-expiry should be gone
	_, found = ts.Get("custom-expiry")
	if found == Found {
		t.Error("custom-expiry item should have expired")
	}

	// no-expiry should still be there
	val, found := ts.Get("no-expiry")
	if found != Found || val != 3 {
		t.Error("no-expiry item should still exist with value 3")
	}

	// Wait for janitor to run multiple times
	time.Sleep(50 * time.Millisecond)

	// Count should be 1 (just no-expiry)
	count := ts.ItemCount()
	if count != 1 {
		t.Errorf("Expected 1 item, got %d", count)
		for i := 0; i < len(ts.Items()); i++ {
			bi := ts.Items()[i]
			for k, v := range bi {
				t.Errorf("item: %s (%v)", k, v)

			}
		}
	}
}
