package cache

import (
	"crypto/rand"
	"math"
	"math/big"
	insecurerand "math/rand"
	"os"
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

type unexportedShardedCache[V any] struct {
	*shardedCache[V]
}

type shardedCache[V any] struct {
	seed    uint32
	m       uint32
	cs      []*cache[V]
	janitor *shardedJanitor
}

// djb2 with better shuffling. 5x faster than FNV with the hash.Hash overhead.
func djb33(seed uint32, k string) uint32 {
	var (
		l = uint32(len(k))
		d = 5381 + seed + l
		i = uint32(0)
	)
	// Why is all this 5x faster than a for loop?
	if l >= 4 {
		for i < l-4 {
			d = (d * 33) ^ uint32(k[i])
			d = (d * 33) ^ uint32(k[i+1])
			d = (d * 33) ^ uint32(k[i+2])
			d = (d * 33) ^ uint32(k[i+3])
			i += 4
		}
	}
	switch l - i {
	case 1:
	case 2:
		d = (d * 33) ^ uint32(k[i])
	case 3:
		d = (d * 33) ^ uint32(k[i])
		d = (d * 33) ^ uint32(k[i+1])
	case 4:
		d = (d * 33) ^ uint32(k[i])
		d = (d * 33) ^ uint32(k[i+1])
		d = (d * 33) ^ uint32(k[i+2])
	}
	return d ^ (d >> 16)
}

func (sc *shardedCache[V]) bucket(k string) *cache[V] {
	return sc.cs[djb33(sc.seed, k)%sc.m]
}

func (sc *shardedCache[V]) Set(k string, x V, d time.Duration) {
	sc.bucket(k).Set(k, x, d)
}

func (sc *shardedCache[V]) Add(k string, x V, d time.Duration) error {
	return sc.bucket(k).Add(k, x, d)
}

func (sc *shardedCache[V]) Replace(k string, x V, d time.Duration) error {
	return sc.bucket(k).Replace(k, x, d)
}

func (sc *shardedCache[V]) Get(k string) (V, FoundItem) {
	return sc.bucket(k).Get(k)
}

func (sc *shardedCache[V]) Delete(k string) {
	sc.bucket(k).Delete(k)
}

func (sc *shardedCache[V]) DeleteExpired() {
	for _, v := range sc.cs {
		v.DeleteExpired()
	}
}

// Returns the items in the cache. This may include items that have expired,
// but have not yet been cleaned up. If this is significant, the Expiration
// fields of the items should be checked. Note that explicit synchronization
// is needed to use a cache and its corresponding Items() return values at
// the same time, as the maps are shared.
func (sc *shardedCache[V]) Items() []map[string]Item[V] {
	res := make([]map[string]Item[V], len(sc.cs))
	for i, v := range sc.cs {
		res[i] = v.Items()
	}
	return res
}

func (sc *shardedCache[V]) Flush() {
	for _, v := range sc.cs {
		v.Flush()
	}
}

type shardedJanitor struct {
	Interval time.Duration
	stop     chan bool
}

func (j *shardedJanitor) Run(sc interface{ DeleteExpired() }) {
	j.stop = make(chan bool)
	tick := time.NewTicker(j.Interval)
	for {
		select {
		case <-tick.C:
			sc.DeleteExpired()
		case <-j.stop:
			return
		}
	}
}

func stopShardedJanitor[V any](sc *unexportedShardedCache[V]) {
	sc.janitor.stop <- true
}

func runShardedJanitor[V any](sc *shardedCache[V], ci time.Duration) {
	j := &shardedJanitor{
		Interval: ci,
	}
	sc.janitor = j
	go j.Run(sc)
}

func NewShardedCache[V any](n int, de time.Duration) *shardedCache[V] {
	numShards := uint32(runtime.NumCPU() * 2)
	if numShards < defaultShards {
		numShards = defaultShards
	}
	max := big.NewInt(0).SetUint64(uint64(math.MaxUint32))
	rnd, err := rand.Int(rand.Reader, max)
	var seed uint32
	if err != nil {
		os.Stderr.Write([]byte("WARNING: go-cache's newShardedCache failed to read from the system CSPRNG (/dev/urandom or equivalent.) Your system's security may be compromised. Continuing with an insecure seed.\n"))
		seed = insecurerand.Uint32()
	} else {
		seed = uint32(rnd.Uint64())
	}
	sc := &shardedCache[V]{
		seed: seed,
		m:    uint32(n),
		cs:   make([]*cache[V], n),
	}
	for i := 0; i < n; i++ {
		c := &cache[V]{
			defaultExpiration: de,
			items:             map[string]Item[V]{},
		}
		sc.cs[i] = c
	}
	return sc
}

func unexportedNewSharded[V any](defaultExpiration, cleanupInterval time.Duration, shards int) *unexportedShardedCache[V] {
	if defaultExpiration == 0 {
		defaultExpiration = -1
	}
	sc := NewShardedCache[V](shards, defaultExpiration)
	SC := &unexportedShardedCache[V]{sc}
	if cleanupInterval > 0 {
		runShardedJanitor(sc, cleanupInterval)
		runtime.SetFinalizer(SC, stopShardedJanitor[V])
	}
	return SC
}

// Add the ShardedNumericCache type that wraps shardedCache
type ShardedNumericCache[N Number] struct {
	*shardedCache[N]
}

// ModifyNumeric atomically modifies a numeric item in the cache.
// If the item does not exist or is expired, it is set to `operand`.
// If isIncrement is true, `operand` is added to the existing value.
// If isIncrement is false, `operand` is subtracted from the existing value.
// It returns the new value and an error if the existing item is not of type N.
func (sc *ShardedNumericCache[N]) ModifyNumeric(k string, operand N, isIncrement bool) (N, error) {
	// Get the appropriate bucket for this key
	bucket := sc.bucket(k)

	// Convert the bucket to a NumericCache so we can call ModifyNumeric on it
	nc := &NumericCache[N]{cache: bucket}

	// Delegate to the bucket's ModifyNumeric implementation
	return nc.ModifyNumeric(k, operand, isIncrement)
}

// Helper function to create a new ShardedNumericCache
func NewShardedNumeric[N Number](shards int, defaultExpiration, cleanupInterval time.Duration) *ShardedNumericCache[N] {
	// Create the underlying shardedCache
	sc := NewShardedCache[N](shards, defaultExpiration)

	// Create and return the ShardedNumericCache wrapper
	result := &ShardedNumericCache[N]{shardedCache: sc}

	// Set up the janitor if needed
	if cleanupInterval > 0 {
		j := &shardedJanitor{
			Interval: cleanupInterval,
		}
		sc.janitor = j
		go j.Run(sc)
	}

	return result
}
