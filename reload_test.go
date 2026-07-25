package viper

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestReloadStreamPublishesOwnedSuccessAndFailure(t *testing.T) {
	stream := NewReloadStream(2)
	registry := New()
	registry.Set("version", "one")
	first, err := stream.Reload(context.Background(), func(context.Context) (*Viper, error) { return registry, nil })
	if err != nil || first.Err != nil || first.Version != 1 {
		t.Fatalf("first reload = %#v, %v", first, err)
	}
	registry.Set("version", "two")
	if got := first.Snapshot.GetString("version"); got != "one" {
		t.Fatalf("published snapshot mutated: %q", got)
	}
	wantErr := errors.New("unavailable")
	second, err := stream.Reload(context.Background(), func(context.Context) (*Viper, error) { return nil, wantErr })
	if err != nil || !errors.Is(second.Err, wantErr) || second.Version != 2 {
		t.Fatalf("second reload = %#v, %v", second, err)
	}
	if (<-stream.Events()).Version != 1 || (<-stream.Events()).Version != 2 {
		t.Fatal("events published out of order")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Reload(context.Background(), func(context.Context) (*Viper, error) { return registry, nil }); !errors.Is(err, ErrReloadStreamClosed) {
		t.Fatalf("reload after close = %v", err)
	}
}

func TestReloadStreamConcurrentAttemptsAreOrderedAndRaceFree(t *testing.T) {
	const attempts = 64
	stream := NewReloadStream(attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(value int) {
			defer wg.Done()
			_, err := stream.Reload(context.Background(), func(context.Context) (*Viper, error) {
				v := New()
				v.Set("value", fmt.Sprint(value))
				return v, nil
			})
			if err != nil {
				t.Errorf("Reload: %v", err)
			}
		}(i)
	}
	wg.Wait()
	seen := make([]bool, attempts+1)
	for i := 0; i < attempts; i++ {
		event := <-stream.Events()
		if event.Version == 0 || event.Version > attempts || seen[event.Version] {
			t.Fatalf("invalid version %d", event.Version)
		}
		seen[event.Version] = true
		if event.Err != nil || event.Snapshot.GetString("value") == "" {
			t.Fatalf("event = %#v", event)
		}
	}
}
