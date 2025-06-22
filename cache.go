package cache

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

type Item[V any] struct {
	Object     V
	Expiration time.Time
}

type FoundItem uint8

const (
	Found FoundItem = iota
	Miss
	Expired
)

// Returns true if the item has expired.
func (item Item[V]) Expired() bool {
	if item.Expiration.IsZero() {
		return false
	}
	return time.Now().After(item.Expiration)
}

const (
	// For use with functions that take an expiration time.
	NoExpiration time.Duration = -1
	// For use with functions that take an expiration time. Equivalent to
	// passing in the same expiration duration as was given to New() or
	// NewFrom() when the cache was created (e.g. 5 minutes.)
	DefaultExpiration time.Duration = 0
)

type Cache[V any] struct {
	*cache[V]
	// If this is confusing, see the comment at the bottom of New()
}

type cache[V any] struct {
	defaultExpiration time.Duration
	items             map[string]Item[V]
	mu                sync.RWMutex
	onEvicted         func(string, V)
	janitor           *janitor[V]
}

// Add an item to the cache, replacing any existing item. If the duration is 0
// (DefaultExpiration), the cache's default expiration time is used. If it is -1
// (NoExpiration), the item never expires.
func (c *cache[V]) Set(k string, x V, d time.Duration) {
	// "Inlining" of set
	var e time.Time
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d)
	}
	c.mu.Lock()
	c.items[k] = Item[V]{
		Object:     x,
		Expiration: e,
	}
	// TODO: Calls to mu.Unlock are currently not deferred because defer
	// adds ~200 ns (as of go1.)
	c.mu.Unlock()
}

func (c *cache[V]) set(k string, x V, d time.Duration) {
	var e time.Time
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d)
	}
	c.items[k] = Item[V]{
		Object:     x,
		Expiration: e,
	}
}

// Add an item to the cache, replacing any existing item, using the default
// expiration.
func (c *cache[V]) SetDefault(k string, x V) {
	c.Set(k, x, DefaultExpiration)
}

// Add an item to the cache only if an item doesn't already exist for the given
// key, or if the existing item has expired. Returns an error otherwise.
func (c *cache[V]) Add(k string, x V, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if found {
		c.mu.Unlock()
		return fmt.Errorf("Item %s already exists", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// Set a new value for the cache key only if it already exists, and the existing
// item hasn't expired. Returns an error otherwise.
func (c *cache[V]) Replace(k string, x V, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if !found {
		c.mu.Unlock()
		return fmt.Errorf("Item %s doesn't exist", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// Get an item from the cache. Returns the item or nil, and a founItem(Found,Miss,Expired) indicating
// whether the key was found.
func (c *cache[V]) Get(k string) (V, FoundItem) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		var zero V
		return zero, Miss
	}
	if !item.Expiration.IsZero() {
		if time.Now().After(item.Expiration) {
			c.mu.RUnlock()
			var zero V
			return zero, Expired
		}
	}
	c.mu.RUnlock()
	return item.Object, Found
}

// GetWithExpiration returns an item and its expiration time from the cache.
// It returns the item or nil, the expiration time if one is set (if the item
// never expires a zero value for time.Time is returned), and a bool indicating
// whether the key was found.
func (c *cache[V]) GetWithExpiration(k string) (V, time.Time, bool) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		var zero V
		return zero, time.Time{}, false
	}

	if !item.Expiration.IsZero() {
		if time.Now().After(item.Expiration) {
			c.mu.RUnlock()
			var zero V
			return zero, time.Time{}, false
		}

		// Return the item and the expiration time
		c.mu.RUnlock()
		return item.Object, item.Expiration, true
	}

	// If expiration <= 0 (i.e. no expiration time set) then return the item
	// and a zeroed time.Time
	c.mu.RUnlock()
	return item.Object, time.Time{}, true
}

func (c *cache[V]) get(k string) (V, bool) {
	item, found := c.items[k]
	if !found {
		var zero V
		return zero, false
	}
	// "Inlining" of Expired
	if !item.Expiration.IsZero() {
		if time.Now().After(item.Expiration) {
			var zero V
			return zero, false
		}
	}
	return item.Object, true
}

// SignedInteger is a constraint for signed integer types.
type SignedInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// UnsignedInteger is a constraint for unsigned integer types.
type UnsignedInteger interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Float is a constraint for float types.
type Float interface {
	~float32 | ~float64
}

// Number is a constraint for any numeric type that supports addition and subtraction.
type Number interface {
	SignedInteger | UnsignedInteger | Float
}

// ModifyNumeric atomically modifies a numeric item in the cache.
// If the item does not exist or is expired, it is set to `operand`.
// If isIncrement is true, `operand` is added to the existing value.
// If isIncrement is false, `operand` is subtracted from the existing value.
// It returns the new value and an error if the existing item is not of type N.
// This method replaces all previous Increment<Type> and Decrement<Type> methods.
type NumericCache[N Number] struct {
	*cache[N]
}

// ModifyNumeric atomically modifies a numeric item in the cache.
// If the item does not exist or is expired, it is set to `operand`.
// If isIncrement is true, `operand` is added to the existing value.
// If isIncrement is false, `operand` is subtracted from the existing value.
// It returns the new value and an error if the existing item is not of type N.
func (c *NumericCache[N]) ModifyNumeric(k string, operand N, isIncrement bool) (N, error) {
	c.mu.Lock()
	item, itemFound := c.items[k]

	// Handle cases where item is not found or is expired
	if !itemFound || item.Expired() {
		if itemFound && item.Expired() {
			if c.onEvicted != nil {
				c.onEvicted(k, item.Object)
			}
		}
		c.set(k, operand, DefaultExpiration)
		c.mu.Unlock()
		return operand, nil
	}

	currentVal := item.Object

	var newVal N
	if isIncrement {
		newVal = currentVal + operand
	} else {
		newVal = currentVal - operand
	}

	item.Object = newVal
	c.items[k] = item
	c.mu.Unlock()
	return newVal, nil
}

// Delete an item from the cache. Does nothing if the key is not in the cache.
func (c *cache[V]) Delete(k string) {
	c.mu.Lock()
	v, evicted := c.delete(k)
	c.mu.Unlock()
	if evicted {
		c.onEvicted(k, v)
	}
}

func (c *cache[V]) delete(k string) (V, bool) {
	if c.onEvicted != nil {
		if v, found := c.items[k]; found {
			delete(c.items, k)
			// Also call onEvicted here if we are deleting an item that has an onEvicted callback
			// However, the current logic in Delete() and DeleteExpired() calls onEvicted *after*
			// the lock is released, based on the collected items.
			// For consistency with that pattern, we might not call it here directly,
			// or the calling functions (Delete, DeleteExpired) must be aware.
			// The current `Delete` method does:
			//   c.mu.Lock()
			//   v, evicted := c.delete(k)
			//   c.mu.Unlock()
			//   if evicted { c.onEvicted(k, v) }
			// This implies c.delete should just return the value and a flag.
			return v.Object, true
		}
	}
	delete(c.items, k)
	var zero V
	return zero, false
}

type keyAndValue[V any] struct {
	key   string
	value V
}

// Delete all expired items from the cache.
func (c *cache[V]) DeleteExpired() {
	var evictedItems []keyAndValue[V]
	now := time.Now()
	c.mu.Lock()
	for k, v := range c.items {
		// "Inlining" of expired
		if !v.Expiration.IsZero() && now.After(v.Expiration) {
			ov, evicted := c.delete(k) // delete will return V, bool
			if evicted {
				// ov is of type V, so this is type-safe
				evictedItems = append(evictedItems, keyAndValue[V]{k, ov})
			}
		}
	}
	c.mu.Unlock()
	if c.onEvicted != nil {
		for _, v := range evictedItems {
			c.onEvicted(v.key, v.value) // v.value is of type V
		}
	}
}

// Sets an (optional) function that is called with the key and value when an
// item is evicted from the cache. (Including when it is deleted manually, but
// not when it is overwritten.) Set to nil to disable.
func (c *cache[V]) OnEvicted(f func(string, V)) {
	c.mu.Lock()
	c.onEvicted = f
	c.mu.Unlock()
}

// Write the cache's items (using Gob) to an io.Writer.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache[V]) Save(w io.Writer) (err error) {
	enc := gob.NewEncoder(w)
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("error registering item types with Gob library")
		}
	}()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, v := range c.items {
		gob.Register(v.Object)
	}
	err = enc.Encode(&c.items)
	return
}

// Save the cache's items to the given filename, creating the file if it
// doesn't exist, and overwriting it if it does.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache[V]) SaveFile(fname string) error {
	fp, err := os.Create(fname)
	if err != nil {
		return err
	}
	err = c.Save(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// Add (Gob-serialized) cache items from an io.Reader, excluding any items with
// keys that already exist (and haven't expired) in the current cache.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache[V]) Load(r io.Reader) error {
	dec := gob.NewDecoder(r)
	items := map[string]Item[V]{}
	err := dec.Decode(&items)
	if err == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range items {
			ov, found := c.items[k]
			if !found || ov.Expired() {
				c.items[k] = v
			}
		}
	}
	return err
}

// Load and add cache items from the given filename, excluding any items with
// keys that already exist in the current cache.
//
// NOTE: This method is deprecated in favor of c.Items() and NewFrom() (see the
// documentation for NewFrom().)
func (c *cache[V]) LoadFile(fname string) error {
	fp, err := os.Open(fname)
	if err != nil {
		return err
	}
	err = c.Load(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// Copies all unexpired items in the cache into a new map and returns it.
func (c *cache[V]) Items() map[string]Item[V] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[string]Item[V], len(c.items))
	now := time.Now()
	for k, v := range c.items {
		// "Inlining" of Expired
		if !v.Expiration.IsZero() {
			if now.After(v.Expiration) {
				continue
			}
		}
		m[k] = v
	}
	return m
}

// Returns the number of items in the cache. This may include items that have
// expired, but have not yet been cleaned up.
func (c *cache[V]) ItemCount() int {
	c.mu.RLock()
	n := len(c.items)
	c.mu.RUnlock()
	return n
}

// Delete all items from the cache.
func (c *cache[V]) Flush() {
	c.mu.Lock()
	c.items = map[string]Item[V]{}
	c.mu.Unlock()
}

type janitor[V any] struct {
	Interval time.Duration
	stop     chan bool
}

func (j *janitor[V]) Run(c *cache[V]) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func stopJanitor[V any](c *Cache[V]) {
	c.janitor.stop <- true
}

func runJanitor[V any](c *cache[V], ci time.Duration) {
	j := &janitor[V]{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.janitor = j
	go j.Run(c)
}

func newCache[V any](de time.Duration, m map[string]Item[V]) *cache[V] {
	if de == 0 {
		de = -1
	}
	c := &cache[V]{
		defaultExpiration: de,
		items:             m,
	}
	return c
}

func newCacheWithJanitor[V any](de time.Duration, ci time.Duration, m map[string]Item[V]) *Cache[V] {
	c := newCache(de, m)
	// This trick ensures that the janitor goroutine (which--granted it
	// was enabled--is running DeleteExpired on c forever) does not keep
	// the returned C object from being garbage collected. When it is
	// garbage collected, the finalizer stops the janitor goroutine, after
	// which c can be collected.
	C := &Cache[V]{c}
	if ci > 0 {
		runJanitor(c, ci)
		runtime.SetFinalizer(C, stopJanitor[V])
	}
	return C
}

// Return a new cache with a given default expiration duration and cleanup
// interval. If the expiration duration is less than one (or NoExpiration),
// the items in the cache never expire (by default), and must be deleted
// manually. If the cleanup interval is less than one, expired items are not
// deleted from the cache before calling c.DeleteExpired().
func New[V any](defaultExpiration, cleanupInterval time.Duration) *Cache[V] {
	items := make(map[string]Item[V])
	return newCacheWithJanitor[V](defaultExpiration, cleanupInterval, items)
}

// Return a new cache with a given default expiration duration and cleanup
// interval. If the expiration duration is less than one (or NoExpiration),
// the items in the cache never expire (by default), and must be deleted
// manually. If the cleanup interval is less than one, expired items are not
// deleted from the cache before calling c.DeleteExpired().
//
// NewFrom() also accepts an items map which will serve as the underlying map
// for the cache. This is useful for starting from a deserialized cache
// (serialized using e.g. gob.Encode() on c.Items()), or passing in e.g.
// make(map[string]Item, 500) to improve startup performance when the cache
// is expected to reach a certain minimum size.
//
// Only the cache's methods synchronize access to this map, so it is not
// recommended to keep any references to the map around after creating a cache.
// If need be, the map can be accessed at a later point using c.Items() (subject
// to the same caveat.)
//
// Note regarding serialization: When using e.g. gob, make sure to
// gob.Register() the individual types stored in the cache before encoding a
// map retrieved with c.Items(), and to register those same types before
// decoding a blob containing an items map.
func NewFrom[V any](defaultExpiration, cleanupInterval time.Duration, items map[string]Item[V]) *Cache[V] {
	return newCacheWithJanitor[V](defaultExpiration, cleanupInterval, items)
}
