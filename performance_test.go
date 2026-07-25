package viper

import (
	"testing"

	upstream "github.com/spf13/viper"
)

var benchmarkString string

func benchmarkPair() (Snapshot, *upstream.Viper) {
	v := New()
	v.SetDefault("service.endpoint", "https://example.test")
	snapshot := v.Snapshot()
	u := upstream.New()
	u.SetDefault("service.endpoint", "https://example.test")
	return snapshot, u
}

func BenchmarkSnapshotGetString(b *testing.B) {
	snapshot, _ := benchmarkPair()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkString = snapshot.GetString("service.endpoint")
	}
}

func BenchmarkUpstreamGetString(b *testing.B) {
	_, v := benchmarkPair()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkString = v.GetString("service.endpoint")
	}
}

func TestSnapshotAllocationBudget(t *testing.T) {
	snapshot, upstreamViper := benchmarkPair()
	ours := testing.AllocsPerRun(1000, func() { benchmarkString = snapshot.GetString("service.endpoint") })
	theirs := testing.AllocsPerRun(1000, func() { benchmarkString = upstreamViper.GetString("service.endpoint") })
	if ours != 0 {
		t.Fatalf("snapshot allocations = %v, want 0", ours)
	}
	if theirs < 1 {
		t.Fatalf("upstream allocations = %v; paired allocation reduction is not measurable", theirs)
	}
}
