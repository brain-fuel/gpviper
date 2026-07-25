package viper

import (
	"fmt"
	"sync"
	"testing"

	upstream "github.com/spf13/viper"
)

func FuzzPrecedenceDifferential(f *testing.F) {
	for _, seed := range []struct{ key, low, high string }{{"port", "80", "90"}, {"Server.Name", "a", "b"}, {"x-y", "", "set"}} {
		f.Add(seed.key, seed.low, seed.high)
	}
	f.Fuzz(func(t *testing.T, key, low, high string) {
		if key == "" || len(key) > 64 || len(low) > 256 || len(high) > 256 {
			t.Skip()
		}
		ours, theirs := New(), upstream.New()
		ours.SetDefault(key, low)
		theirs.SetDefault(key, low)
		if got, want := ours.GetString(key), theirs.GetString(key); got != want {
			t.Fatalf("default: %q != %q", got, want)
		}
		ours.Set(key, high)
		theirs.Set(key, high)
		if got, want := ours.GetString(key), theirs.GetString(key); got != want {
			t.Fatalf("override: %q != %q", got, want)
		}
	})
}

func TestSnapshotConcurrentReadsAndMutableFacadeRace(t *testing.T) {
	v := New()
	v.SetDefault("key", "initial")
	snapshot := v.Snapshot()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_ = snapshot.GetString("key")
				v.Set("key", fmt.Sprintf("%d-%d", id, i))
				_ = v.GetString("key")
			}
		}(worker)
	}
	wg.Wait()
	if got := snapshot.GetString("key"); got != "initial" {
		t.Fatalf("immutable snapshot changed: %q", got)
	}
}

func TestMergeLastWinsAndInputDoesNotAlias(t *testing.T) {
	input := map[string]any{"nested": map[string]any{"value": "first"}}
	v := New()
	_ = v.MergeConfigMap(input)
	input["nested"].(map[string]any)["value"] = "mutated"
	if got := v.GetString("nested.value"); got != "first" {
		t.Fatalf("aliased input: %q", got)
	}
	_ = v.MergeConfigMap(map[string]any{"nested": map[string]any{"value": "second"}})
	if got := v.GetString("nested.value"); got != "second" {
		t.Fatalf("last merge did not win: %q", got)
	}
}
