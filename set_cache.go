package cache

import (
	"runtime"
	"time"
)

// setItem stores the per-member expiration time inside a set.
// A zero Expiration means the member never expires.
type setItem struct {
	Expiration time.Time
}

// expired returns true if this member's TTL has elapsed.
func (s setItem) expired() bool {
	if s.Expiration.IsZero() {
		return false
	}
	return time.Now().After(s.Expiration)
}

// setData is the value stored per "set key": a map of member → setItem.
type setData map[string]setItem

// ---------------------------------------------------------------------------
// SetCache
// ---------------------------------------------------------------------------

// SetCache tracks unique members per key, each member carrying its own TTL.
//
// Typical use-case: count unique active device/session IDs per user.
// Each device has its own TTL (heartbeat expiry), and the parent key groups
// all devices for that user.
//
//	sc := NewSetCache(5*time.Minute, 10*time.Minute)
//
//	// Record that device "d1" is active for user "u:42", device TTL = 30s
//	count, isNew := sc.AddMember("u:42", "d1", 30*time.Second, 5*time.Minute)
//
//	// Check whether adding "d2" would exceed limit=3, pruning stale members first
//	count, isNew = sc.CheckAndClean("u:42", "d2", 3)
type SetCache struct {
	c *cache[setData]
}

// NewSetCache creates a new SetCache.
//   - defaultExpiration: TTL applied to the set key itself.
//   - cleanupInterval:   How often the janitor removes expired set keys (0 = never).
func NewSetCache(defaultExpiration, cleanupInterval time.Duration) *SetCache {
	inner := New[setData](defaultExpiration, cleanupInterval)
	return &SetCache{c: inner.cache}
}

// WithRedis attaches a Redis L2 layer. The setData is JSON-serialised.
func (sc *SetCache) WithRedis(cli RedisClient) *SetCache {
	sc.c.withRedis(cli)
	return sc
}

// WithCapacity limits the maximum number of set keys in local memory.
func (sc *SetCache) WithCapacity(cap int) *SetCache {
	sc.c.withCapacity(cap)
	return sc
}

// Close stops the background Redis worker and waits for it to drain.
func (sc *SetCache) Close() {
	sc.c.Close()
}

// AddMember adds (or refreshes) a member inside the set identified by setKey.
//
//   - memberTTL: individual TTL for this member (0 = cache default, -1 = no expiry).
//   - setTTL:    TTL for the parent set key (0 = cache default, -1 = no expiry).
//
// Returns:
//   - count: number of live members AFTER this operation (includes this member).
//   - isNew: true if the member was not already alive in the set.
func (sc *SetCache) AddMember(setKey, member string, memberTTL, setTTL time.Duration) (count int, isNew bool) {
	sc.c.mu.Lock()
	defer sc.c.mu.Unlock()

	sd := sc.loadSet(setKey)

	if existing, ok := sd[member]; ok && !existing.expired() {
		isNew = false
	} else {
		isNew = true
	}

	var memberExp time.Time
	eff := memberTTL
	if eff == DefaultExpiration {
		eff = sc.c.defaultExpiration
	}
	if eff > 0 {
		memberExp = time.Now().Add(eff)
	}
	sd[member] = setItem{Expiration: memberExp}

	sc.c.set(setKey, sd, setTTL)
	count = liveCount(sd)
	return
}

// RemoveMember removes a specific member from the set.
func (sc *SetCache) RemoveMember(setKey, member string) {
	sc.c.mu.Lock()
	defer sc.c.mu.Unlock()

	sd := sc.loadSet(setKey)
	delete(sd, member)
	sc.c.set(setKey, sd, DefaultExpiration)
}

// HasMember reports whether member is present and not expired in the set.
func (sc *SetCache) HasMember(setKey, member string) bool {
	sd, found := sc.c.Get(setKey)
	if found == Miss {
		return false
	}
	item, ok := sd[member]
	return ok && !item.expired()
}

// Members returns all non-expired member IDs for the set.
func (sc *SetCache) Members(setKey string) []string {
	sd, found := sc.c.Get(setKey)
	if found == Miss {
		return nil
	}
	out := make([]string, 0, len(sd))
	for m, item := range sd {
		if !item.expired() {
			out = append(out, m)
		}
	}
	return out
}

// Count returns the number of non-expired members without modifying the set.
func (sc *SetCache) Count(setKey string) int {
	sd, found := sc.c.Get(setKey)
	if found == Miss {
		return 0
	}
	return liveCount(sd)
}

// CleanAndCount removes expired members from the set in-place and returns
// the number of remaining live members.
func (sc *SetCache) CleanAndCount(setKey string) int {
	sc.c.mu.Lock()
	defer sc.c.mu.Unlock()

	sd := sc.loadSet(setKey)
	changed := false
	for m, item := range sd {
		if item.expired() {
			delete(sd, m)
			changed = true
		}
	}
	if changed {
		sc.c.set(setKey, sd, DefaultExpiration)
	}
	return len(sd)
}

// CheckAndClean is the Go equivalent of the Node.js checkValueInListAndCleanUp.
//
// It determines whether member is already in the set, and if the projected
// count would exceed limit it prunes expired members before returning.
//
// Steps (mirrors the JS logic exactly):
//  1. Load the current set.
//  2. Count live members; note whether member is new.
//  3. If count (+ 1 if new) ≤ limit → return immediately (no write).
//  4. Else delete every expired member that is NOT member, re-count, return.
//
// Returns:
//   - count: live-member count after any cleanup (does NOT write member to the set).
//   - isNew: true if member was absent or expired.
//
// Note: CheckAndClean does NOT add the member to the set. Call AddMember
// separately if you want to persist the new membership.
func (sc *SetCache) CheckAndClean(setKey, member string, limit int) (count int, isNew bool) {
	sc.c.mu.Lock()
	defer sc.c.mu.Unlock()

	sd := sc.loadSet(setKey)

	if existing, ok := sd[member]; ok && !existing.expired() {
		isNew = false
	} else {
		isNew = true
	}

	count = liveCount(sd)
	if isNew {
		count++ // virtual +1 for the not-yet-added member
	}

	if count <= limit {
		return // fast path – no cleanup needed
	}

	// Prune expired members (skip member itself; it may not be in the map yet)
	changed := false
	for m, item := range sd {
		if m == member {
			continue
		}
		if item.expired() {
			delete(sd, m)
			changed = true
		}
	}
	if changed {
		sc.c.set(setKey, sd, DefaultExpiration)
	}

	// Re-compute count
	count = liveCount(sd)
	if isNew {
		count++
	}
	return
}

// DeleteSet removes the entire set for setKey.
func (sc *SetCache) DeleteSet(setKey string) {
	sc.c.Delete(setKey)
}

// Flush removes all sets from local memory.
func (sc *SetCache) Flush() {
	sc.c.Flush()
}

// ItemCount returns the number of set keys currently in local memory
// (may include expired set keys not yet janitor-cleaned).
func (sc *SetCache) ItemCount() int {
	return sc.c.ItemCount()
}

// OnEvicted registers a callback called when a set key is evicted.
func (sc *SetCache) OnEvicted(f func(string, setData)) {
	sc.c.OnEvicted(f)
}

// --- internal helpers (call under the appropriate lock) ---

// loadSet returns a mutable copy of the setData for key, creating one if absent/expired.
// Caller must hold c.mu write-lock.
func (sc *SetCache) loadSet(key string) setData {
	item, found := sc.c.items[key]
	if !found || item.Expired() {
		if val, ttl, ok := sc.c.fetchFromRedis(key); ok {
			sc.c.setLocal(key, val, ttl)
			item = sc.c.items[key]
			found = true
		}
	}
	if !found || item.Expired() {
		return make(setData)
	}
	// Deep-copy so we don't mutate the stored value in-place before set().
	cp := make(setData, len(item.Object))
	for k, v := range item.Object {
		cp[k] = v
	}
	return cp
}


// liveCount counts non-expired entries in sd (nil-safe).
func liveCount(sd setData) (n int) {
	for _, item := range sd {
		if !item.expired() {
			n++
		}
	}
	return
}

// ---------------------------------------------------------------------------
// ShardedSetCache
// ---------------------------------------------------------------------------

// ShardedSetCache is a sharded version of SetCache for high-concurrency workloads.
// Set keys are deterministically hashed to shards, so different keys can be
// operated on concurrently without lock contention.
type ShardedSetCache struct {
	shards     []*SetCache
	numBuckets int
	janitor    *shardedJanitor
}

type unexportedShardedSetCache struct {
	*ShardedSetCache
}

// NewShardedSetCache creates a sharded SetCache.
//   - numShards: 0 = auto (max of defaultShards and 2×CPU count), rounded to nearest power of two.
func NewShardedSetCache(numShards int, defaultExpiration, cleanupInterval time.Duration) *ShardedSetCache {
	if numShards == 0 {
		numShards = runtime.NumCPU() * 2
		if numShards < defaultShards {
			numShards = defaultShards
		}
	}
	numShards = nearestPowerOfTwo(numShards)

	ssc := &ShardedSetCache{
		shards:     make([]*SetCache, numShards),
		numBuckets: numShards,
	}
	for i := 0; i < numShards; i++ {
		ssc.shards[i] = NewSetCache(defaultExpiration, 0) // janitor managed at this level
	}

	if cleanupInterval > 0 {
		j := &shardedJanitor{Interval: cleanupInterval}
		ssc.janitor = j
		go j.Run(ssc)
		usc := &unexportedShardedSetCache{ssc}
		runtime.SetFinalizer(usc, stopShardedSetJanitor)
	}
	return ssc
}

func stopShardedSetJanitor(usc *unexportedShardedSetCache) {
	if usc.janitor != nil {
		usc.janitor.stop <- true
	}
}

// shard returns the SetCache responsible for setKey.
func (ssc *ShardedSetCache) shard(setKey string) *SetCache {
	return ssc.shards[shardKey(setKey, ssc.numBuckets)]
}

// DeleteExpired is called by the janitor on each cleanup tick.
func (ssc *ShardedSetCache) DeleteExpired() {
	for _, s := range ssc.shards {
		s.c.DeleteExpired()
	}
}

// AddMember delegates to the appropriate shard.
func (ssc *ShardedSetCache) AddMember(setKey, member string, memberTTL, setTTL time.Duration) (int, bool) {
	return ssc.shard(setKey).AddMember(setKey, member, memberTTL, setTTL)
}

// RemoveMember delegates to the appropriate shard.
func (ssc *ShardedSetCache) RemoveMember(setKey, member string) {
	ssc.shard(setKey).RemoveMember(setKey, member)
}

// HasMember delegates to the appropriate shard.
func (ssc *ShardedSetCache) HasMember(setKey, member string) bool {
	return ssc.shard(setKey).HasMember(setKey, member)
}

// Members delegates to the appropriate shard.
func (ssc *ShardedSetCache) Members(setKey string) []string {
	return ssc.shard(setKey).Members(setKey)
}

// Count delegates to the appropriate shard.
func (ssc *ShardedSetCache) Count(setKey string) int {
	return ssc.shard(setKey).Count(setKey)
}

// CleanAndCount delegates to the appropriate shard.
func (ssc *ShardedSetCache) CleanAndCount(setKey string) int {
	return ssc.shard(setKey).CleanAndCount(setKey)
}

// CheckAndClean delegates to the appropriate shard.
func (ssc *ShardedSetCache) CheckAndClean(setKey, member string, limit int) (int, bool) {
	return ssc.shard(setKey).CheckAndClean(setKey, member, limit)
}

// DeleteSet delegates to the appropriate shard.
func (ssc *ShardedSetCache) DeleteSet(setKey string) {
	ssc.shard(setKey).DeleteSet(setKey)
}

// Flush removes all sets from all shards.
func (ssc *ShardedSetCache) Flush() {
	for _, s := range ssc.shards {
		s.Flush()
	}
}

// ItemCount returns the total number of set keys across all shards.
func (ssc *ShardedSetCache) ItemCount() int {
	total := 0
	for _, s := range ssc.shards {
		total += s.ItemCount()
	}
	return total
}

// Close stops the Redis workers for all shards.
func (ssc *ShardedSetCache) Close() {
	for _, s := range ssc.shards {
		s.Close()
	}
}

// WithRedis attaches a Redis backend to every shard.
func (ssc *ShardedSetCache) WithRedis(cli RedisClient) *ShardedSetCache {
	for _, s := range ssc.shards {
		s.WithRedis(cli)
	}
	return ssc
}

// WithCapacity sets a per-shard local-memory limit (total / numShards).
func (ssc *ShardedSetCache) WithCapacity(totalCap int) *ShardedSetCache {
	capPerShard := totalCap / ssc.numBuckets
	if capPerShard < 1 {
		capPerShard = 1
	}
	for _, s := range ssc.shards {
		s.WithCapacity(capPerShard)
	}
	return ssc
}
