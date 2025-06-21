package cache

import (
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
	tc := NewShardedCache[string](13, DefaultExpiration)
	for _, v := range shardedKeys {
		tc.Set(v, "value", DefaultExpiration)
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
	tc := NewShardedCache[any](10, exp)
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
	tsc := NewShardedCache[any](20, exp)
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

// Add benchmark comparing sharded vs non-sharded numeric operations
func BenchmarkCompareShardedVsRegularModifyNumeric(b *testing.B) {
	b.Run("Regular", func(b *testing.B) {
		nc := newNumericTestCache[int](nil, DefaultExpiration, 0)
		nc.Set("counter", 0, DefaultExpiration)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nc.ModifyNumeric("counter", 1, true)
		}
	})
}
