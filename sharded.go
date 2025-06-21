package cache

import (
	"hash/fnv"
	"math"
	"runtime"
	"time"
)

// This is an experimental and unexported (for now) attempt at making a cache
// with better algorithmic complexity than the standard one, namely by
// preventing write locks of the entire cache when an item is added. As of the
// time of writing, the overhead of selecting buckets results in cache
// operations being about twice as slow as for the standard cache with small
// total cache sizes, and faster for larger ones.
//
// See cache_test.go for a few benchmarks.
const defaultShards = 16

type ShardedCache[V any] struct {
	numBuckets int
	cs         []*cache[V]
}

func shardKey(key string, numBuckets int) int {
	// Create an FNV-1a hasher
	hasher := fnv.New32a()
	// Write the key to the hasher
	hasher.Write([]byte(key))
	// Get the hash value
	hash := hasher.Sum32()
	// Map to a bucket
	return int(hash % uint32(numBuckets))
}

func nearestPowerOfTwo(n int) int {
	if n <= 0 {
		return 0 // Or handle as an error, depending on requirements
	}
	if n == 1 {
		return 1
	}

	log2n := math.Log2(float64(n))
	prevPower := 1 << uint(math.Floor(log2n))
	nextPower := 1 << uint(math.Ceil(log2n))

	if math.Abs(float64(n-prevPower)) <= math.Abs(float64(n-nextPower)) {
		return prevPower
	}
	return nextPower
}

func (sc *ShardedCache[V]) bucket(k string) *cache[V] {
	return sc.cs[shardKey(k, sc.numBuckets)]
}

func (sc *ShardedCache[V]) Set(k string, x V, d time.Duration) {
	sc.bucket(k).Set(k, x, d)
}

func (sc *ShardedCache[V]) Add(k string, x V, d time.Duration) error {
	return sc.bucket(k).Add(k, x, d)
}

func (sc *ShardedCache[V]) Replace(k string, x V, d time.Duration) error {
	return sc.bucket(k).Replace(k, x, d)
}

func (sc *ShardedCache[V]) Get(k string) (V, FoundItem) {
	return sc.bucket(k).Get(k)
}

func (sc *ShardedCache[V]) Delete(k string) {
	sc.bucket(k).Delete(k)
}

func (sc *ShardedCache[V]) DeleteExpired() {
	for _, v := range sc.cs {
		v.DeleteExpired()
	}
}

// Returns the items in the cache. This may include items that have expired,
// but have not yet been cleaned up. If this is significant, the Expiration
// fields of the items should be checked. Note that explicit synchronization
// is needed to use a cache and its corresponding Items() return values at
// the same time, as the maps are shared.
func (sc *ShardedCache[V]) Items() []map[string]Item[V] {
	res := make([]map[string]Item[V], len(sc.cs))
	for i, v := range sc.cs {
		res[i] = v.Items()
	}
	return res
}

func (sc *ShardedCache[V]) Flush() {
	for _, v := range sc.cs {
		v.Flush()
	}
}

func NewShardedCache[V any](numShards int, de time.Duration) *ShardedCache[V] {
	if numShards == 0 {
		numShards = runtime.NumCPU() * 2
		if numShards < defaultShards {
			numShards = defaultShards
		}
	}
	numShards = nearestPowerOfTwo(numShards)

	sc := &ShardedCache[V]{
		numBuckets: int(numShards),
		cs:         make([]*cache[V], numShards),
	}
	for i := 0; i < numShards; i++ {
		c := &cache[V]{
			defaultExpiration: de,
			items:             map[string]Item[V]{},
		}
		sc.cs[i] = c
	}
	return sc
}
