package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SetCache tests
// ---------------------------------------------------------------------------

func TestSetCache_AddMember_NewMember(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	count, isNew := sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)
	if !isNew {
		t.Error("Expected isNew=true for a fresh member")
	}
	if count != 1 {
		t.Errorf("Expected count=1, got %d", count)
	}
}

func TestSetCache_AddMember_ExistingMember(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)
	count, isNew := sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)
	if isNew {
		t.Error("Expected isNew=false for a repeated member")
	}
	if count != 1 {
		t.Errorf("Expected count=1 (same member refreshed), got %d", count)
	}
}

func TestSetCache_AddMember_MultipleMembersMultipleKeys(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("user:1", "device-a", 30*time.Second, 5*time.Minute)
	sc.AddMember("user:1", "device-b", 30*time.Second, 5*time.Minute)
	count, isNew := sc.AddMember("user:1", "device-c", 30*time.Second, 5*time.Minute)
	if !isNew {
		t.Error("Expected isNew=true for a new device")
	}
	if count != 3 {
		t.Errorf("Expected count=3, got %d", count)
	}

	// Different key is independent
	count2, isNew2 := sc.AddMember("user:2", "device-x", 30*time.Second, 5*time.Minute)
	if !isNew2 {
		t.Error("Expected isNew=true for new set key")
	}
	if count2 != 1 {
		t.Errorf("Expected count=1 for user:2, got %d", count2)
	}
}

func TestSetCache_AddMember_ExpiredMemberBecomesNew(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	// Add with very short TTL
	sc.AddMember("key1", "device-a", 10*time.Millisecond, 5*time.Minute)
	time.Sleep(20 * time.Millisecond)

	// Re-add after expiry — should be treated as new
	count, isNew := sc.AddMember("key1", "device-a", 30*time.Second, 5*time.Minute)
	if !isNew {
		t.Error("Expected isNew=true for an expired member")
	}
	if count != 1 {
		t.Errorf("Expected count=1, got %d", count)
	}
}

func TestSetCache_HasMember(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)

	if !sc.HasMember("key1", "member-a") {
		t.Error("Expected HasMember=true for existing member")
	}
	if sc.HasMember("key1", "member-b") {
		t.Error("Expected HasMember=false for absent member")
	}
	if sc.HasMember("key999", "member-a") {
		t.Error("Expected HasMember=false for absent key")
	}
}

func TestSetCache_HasMember_AfterExpiry(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "device-a", 10*time.Millisecond, 5*time.Minute)
	time.Sleep(20 * time.Millisecond)

	if sc.HasMember("key1", "device-a") {
		t.Error("Expected HasMember=false for expired member")
	}
}

func TestSetCache_RemoveMember(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "member-b", 30*time.Second, 5*time.Minute)
	sc.RemoveMember("key1", "member-a")

	if sc.HasMember("key1", "member-a") {
		t.Error("Expected member-a to be removed")
	}
	if !sc.HasMember("key1", "member-b") {
		t.Error("Expected member-b to still exist")
	}
	if sc.Count("key1") != 1 {
		t.Errorf("Expected count=1 after removal, got %d", sc.Count("key1"))
	}
}

func TestSetCache_Members(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "a", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "b", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "c", 10*time.Millisecond, 5*time.Minute) // will expire
	time.Sleep(20 * time.Millisecond)

	members := sc.Members("key1")
	if len(members) != 2 {
		t.Errorf("Expected 2 live members, got %d: %v", len(members), members)
	}
	memberSet := make(map[string]bool)
	for _, m := range members {
		memberSet[m] = true
	}
	if !memberSet["a"] || !memberSet["b"] {
		t.Errorf("Expected members a and b, got %v", members)
	}
}

func TestSetCache_Count(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	if sc.Count("missing") != 0 {
		t.Error("Expected count=0 for missing key")
	}
	sc.AddMember("key1", "a", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "b", 30*time.Second, 5*time.Minute)
	if sc.Count("key1") != 2 {
		t.Errorf("Expected count=2, got %d", sc.Count("key1"))
	}
}

func TestSetCache_CleanAndCount(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "a", 10*time.Millisecond, 5*time.Minute)
	sc.AddMember("key1", "b", 10*time.Millisecond, 5*time.Minute)
	sc.AddMember("key1", "c", 30*time.Second, 5*time.Minute)
	time.Sleep(20 * time.Millisecond)

	cnt := sc.CleanAndCount("key1")
	if cnt != 1 {
		t.Errorf("Expected 1 live member after cleanup, got %d", cnt)
	}
	// Confirm the expired members are gone from the stored map
	if sc.HasMember("key1", "a") || sc.HasMember("key1", "b") {
		t.Error("Expired members should have been removed")
	}
	if !sc.HasMember("key1", "c") {
		t.Error("Live member c should still exist")
	}
}

// TestSetCache_CheckAndClean replicates the JS logic step-by-step.
func TestSetCache_CheckAndClean_BelowLimit(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "existing", 30*time.Second, 5*time.Minute)

	// member "new" is not in the set; projected count = 2 ≤ limit=3
	count, isNew := sc.CheckAndClean("key1", "new", 3)
	if !isNew {
		t.Error("Expected isNew=true")
	}
	if count != 2 {
		t.Errorf("Expected count=2, got %d", count)
	}
	// No member was written by CheckAndClean
	if sc.HasMember("key1", "new") {
		t.Error("CheckAndClean should NOT add the member to the set")
	}
}

func TestSetCache_CheckAndClean_AboveLimit_WithCleanup(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	// 2 live members + 2 expired members
	sc.AddMember("key1", "alive1", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "alive2", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "dead1", 10*time.Millisecond, 5*time.Minute)
	sc.AddMember("key1", "dead2", 10*time.Millisecond, 5*time.Minute)
	time.Sleep(20 * time.Millisecond)

	// Without cleanup: 4 members total (2 alive + 2 expired-but-not-pruned).
	// "newguy" is new →  projected count = 5 > limit=3 → triggers cleanup.
	// After pruning 2 expired → 2 alive + "newguy" (virtual) = 3.
	count, isNew := sc.CheckAndClean("key1", "newguy", 3)
	if !isNew {
		t.Error("Expected isNew=true")
	}
	if count != 3 {
		t.Errorf("Expected count=3 after cleanup, got %d", count)
	}
}

func TestSetCache_CheckAndClean_MemberAlreadyExists(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "member-a", 30*time.Second, 5*time.Minute)
	sc.AddMember("key1", "member-b", 30*time.Second, 5*time.Minute)

	count, isNew := sc.CheckAndClean("key1", "member-a", 5)
	if isNew {
		t.Error("Expected isNew=false for existing alive member")
	}
	if count != 2 {
		t.Errorf("Expected count=2, got %d", count)
	}
}

func TestSetCache_DeleteSet(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "a", 30*time.Second, 5*time.Minute)
	sc.DeleteSet("key1")
	if sc.Count("key1") != 0 {
		t.Error("Expected count=0 after DeleteSet")
	}
	if sc.HasMember("key1", "a") {
		t.Error("Expected member to be gone after DeleteSet")
	}
}

func TestSetCache_Flush(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "a", 30*time.Second, 5*time.Minute)
	sc.AddMember("key2", "b", 30*time.Second, 5*time.Minute)
	sc.Flush()
	if sc.ItemCount() != 0 {
		t.Errorf("Expected ItemCount=0 after Flush, got %d", sc.ItemCount())
	}
}

func TestSetCache_NoExpirationMember(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	sc.AddMember("key1", "forever", NoExpiration, 5*time.Minute)
	time.Sleep(10 * time.Millisecond)
	if !sc.HasMember("key1", "forever") {
		t.Error("Expected member with NoExpiration to persist")
	}
}

func TestSetCache_Concurrency(t *testing.T) {
	sc := NewSetCache(5*time.Minute, 0)
	var wg sync.WaitGroup
	numGoroutines := 10
	membersPerGoroutine := 20

	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for m := 0; m < membersPerGoroutine; m++ {
				member := fmt.Sprintf("device-%d-%d", id, m)
				sc.AddMember("shared-key", member, 30*time.Second, 5*time.Minute)
				sc.HasMember("shared-key", member)
				sc.Count("shared-key")
			}
		}(g)
	}
	wg.Wait()

	total := sc.Count("shared-key")
	expected := numGoroutines * membersPerGoroutine
	if total != expected {
		t.Errorf("Expected %d members, got %d", expected, total)
	}
}

// ---------------------------------------------------------------------------
// ShardedSetCache tests
// ---------------------------------------------------------------------------

func TestShardedSetCache_BasicAddAndCheck(t *testing.T) {
	ssc := NewShardedSetCache(4, 5*time.Minute, 0)
	count, isNew := ssc.AddMember("user:1", "device-a", 30*time.Second, 5*time.Minute)
	if !isNew {
		t.Error("Expected isNew=true")
	}
	if count != 1 {
		t.Errorf("Expected count=1, got %d", count)
	}

	count, isNew = ssc.AddMember("user:1", "device-a", 30*time.Second, 5*time.Minute)
	if isNew {
		t.Error("Expected isNew=false on re-add")
	}
	if count != 1 {
		t.Errorf("Expected count=1 (same member), got %d", count)
	}
}

func TestShardedSetCache_CheckAndClean(t *testing.T) {
	ssc := NewShardedSetCache(4, 5*time.Minute, 0)
	ssc.AddMember("user:99", "d1", 30*time.Second, 5*time.Minute)
	ssc.AddMember("user:99", "d2", 10*time.Millisecond, 5*time.Minute) // will expire
	time.Sleep(20 * time.Millisecond)

	// projected count before cleanup: 2 (1 alive + 1 expired) + 1 new = 3 > limit=2
	// after cleanup: 1 alive + 1 new = 2
	count, isNew := ssc.CheckAndClean("user:99", "d3", 2)
	if !isNew {
		t.Error("Expected isNew=true for d3")
	}
	if count != 2 {
		t.Errorf("Expected count=2, got %d", count)
	}
}

func TestShardedSetCache_KeysSpreadAcrossShards(t *testing.T) {
	ssc := NewShardedSetCache(8, 5*time.Minute, 0)
	// Add members under many distinct set keys; they should hash to different shards.
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("user:%d", i)
		ssc.AddMember(key, "device-x", 30*time.Second, 5*time.Minute)
	}
	if ssc.ItemCount() != 50 {
		t.Errorf("Expected 50 set-key entries, got %d", ssc.ItemCount())
	}
}

func TestShardedSetCache_Concurrency(t *testing.T) {
	ssc := NewShardedSetCache(8, 5*time.Minute, 0)
	var wg sync.WaitGroup
	numGoroutines := 20

	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("user:%d", id%5) // 5 distinct keys
			for m := 0; m < 10; m++ {
				member := fmt.Sprintf("device-%d-%d", id, m)
				ssc.AddMember(key, member, 30*time.Second, 5*time.Minute)
				ssc.HasMember(key, member)
				ssc.Count(key)
				ssc.CheckAndClean(key, member, 1000)
			}
		}(g)
	}
	wg.Wait()
}

func TestShardedSetCache_Flush(t *testing.T) {
	ssc := NewShardedSetCache(4, 5*time.Minute, 0)
	for i := 0; i < 10; i++ {
		ssc.AddMember(fmt.Sprintf("key:%d", i), "m", 30*time.Second, 5*time.Minute)
	}
	ssc.Flush()
	if ssc.ItemCount() != 0 {
		t.Errorf("Expected 0 items after Flush, got %d", ssc.ItemCount())
	}
}

func TestShardedSetCache_Members(t *testing.T) {
	ssc := NewShardedSetCache(4, 5*time.Minute, 0)
	ssc.AddMember("set1", "a", 30*time.Second, 5*time.Minute)
	ssc.AddMember("set1", "b", 30*time.Second, 5*time.Minute)
	ssc.AddMember("set1", "c", 10*time.Millisecond, 5*time.Minute)
	time.Sleep(20 * time.Millisecond)

	members := ssc.Members("set1")
	if len(members) != 2 {
		t.Errorf("Expected 2 live members, got %d: %v", len(members), members)
	}
}

func TestShardedSetCache_DeleteSet(t *testing.T) {
	ssc := NewShardedSetCache(4, 5*time.Minute, 0)
	ssc.AddMember("set99", "m1", 30*time.Second, 5*time.Minute)
	ssc.DeleteSet("set99")
	if ssc.Count("set99") != 0 {
		t.Error("Expected count=0 after DeleteSet")
	}
}

// (Benchmarks moved to benchmarks_test.go)
