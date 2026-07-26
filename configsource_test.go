package viper

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	stdconfig "goforge.dev/goplus/std/config"
)

// viper's config file → a capability-gated std/config source; its mtime → the
// Fingerprint WatchConfig reloads on. Same laws as direnv's .envrc bridge.

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileCapabilityAndLoad(t *testing.T) {
	v := New()
	// no config file → Denied → Skipped.
	if stdconfig.IsGranted(fileCapability(v)) {
		t.Fatal("no config file must be a Denied capability")
	}
	loaded, _ := (fileSource{v: v}).Load(fileCapability(v))
	if _, ok := loaded.(stdconfig.Skipped); !ok {
		t.Fatalf("denied load must be Skipped; got %#v", loaded)
	}

	// with a config file → Granted → Applied carrying the file values.
	path := writeCfg(t, "port: 8080\nname: app\n")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if !stdconfig.IsGranted(fileCapability(v)) {
		t.Fatal("in-use config file must be a Granted capability")
	}
	loaded, lerr := (fileSource{v: v}).Load(fileCapability(v))
	applied, ok := mustApplied(t, loaded, lerr)
	if !ok {
		return
	}
	if applied.Layer.Values["port"] != 8080 {
		t.Fatalf("file layer must carry port=8080; got %#v", applied.Layer.Values["port"])
	}
}

func TestReloadConfigSource(t *testing.T) {
	v := New()
	path := writeCfg(t, "port: 1\n")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	_, prev := v.ReloadConfigSource(nil)
	// unchanged → no reload
	if changed, now := v.ReloadConfigSource(prev); changed {
		t.Fatal("unchanged config file must not reload")
	} else {
		prev = now
	}
	// bump mtime → reload
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if changed, _ := v.ReloadConfigSource(prev); !changed {
		t.Fatal("changed config file must reload")
	}
}

func mustApplied(t *testing.T, loaded stdconfig.Loaded, _ error) (stdconfig.Applied, bool) {
	t.Helper()
	a, ok := loaded.(stdconfig.Applied)
	if !ok {
		t.Fatalf("expected Applied; got %#v", loaded)
	}
	return a, ok
}
