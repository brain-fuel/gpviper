// viper as a std/config consumer.
//
// A viper configuration FILE is a capability-gated source (granted when a file
// is in use and readable), and its mtime is the Fingerprint that WatchConfig
// reloads on. Those are exactly the std/config source-loading laws — the same
// ones direnv uses for its allow-gated `.envrc` — so viper and direnv are two
// independent consumers of one primitive, meeting the promotion bar.
package viper

import (
	"fmt"
	"os"

	stdconfig "goforge.dev/goplus/std/config"
)

// fileSource adapts viper's config file to the std/config Loader contract.
type fileSource struct{ v *Viper }

// Provenance: a config file contributes the FileSource layer.
func (s fileSource) Provenance() stdconfig.Source { return stdconfig.FileSource{} }

// Probe fingerprints the in-use config file by its mtime.
func (s fileSource) Probe() (stdconfig.Fingerprint, error) {
	path := s.v.ConfigFileUsed()
	if path == "" {
		return stdconfig.Fingerprint{Exists: false}, nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return stdconfig.Fingerprint{Exists: false}, nil
	}
	if err != nil {
		return stdconfig.Fingerprint{}, err
	}
	return stdconfig.Fingerprint{Token: fmt.Sprint(info.ModTime().UnixNano()), Exists: true}, nil
}

// Load contributes the file-source values only when a config file is in use
// (the granting capability); otherwise the source is Skipped.
func (s fileSource) Load(capability stdconfig.Capability) (stdconfig.Loaded, error) {
	fp, err := s.Probe()
	if err != nil {
		return stdconfig.Skipped{Reason: err.Error(), Fingerprint: fp}, err
	}
	if !stdconfig.IsGranted(capability) {
		return stdconfig.Skipped{Reason: "no config file", Fingerprint: fp}, nil
	}
	return stdconfig.Applied{
		Layer:       stdconfig.Layer{Source: stdconfig.FileSource{}, Values: s.v.fileSourceValues()},
		Fingerprint: fp,
	}, nil
}

// fileCapability grants the load exactly when viper is using a config file.
func fileCapability(v *Viper) stdconfig.Capability {
	if v.ConfigFileUsed() == "" {
		return stdconfig.Denied{Reason: "no config file"}
	}
	return stdconfig.Granted{}
}

// fileSourceValues returns the file-source portion of the resolved config.
func (v *Viper) fileSourceValues() map[string]any {
	values := map[string]any{}
	for key, entry := range v.Snapshot().values {
		if entry.Source == SourceConfig {
			values[key] = entry.Value
		}
	}
	return values
}

// ReloadConfigSource reports whether the config file changed since prev, using
// the std/config watch law (fingerprint comparison) — the same decision
// WatchConfig makes when fsnotify reports a write — and returns the updated
// fingerprint set to seed the next check.
func (v *Viper) ReloadConfigSource(prev map[stdconfig.Source]stdconfig.Fingerprint) (bool, map[stdconfig.Source]stdconfig.Fingerprint) {
	changed, now, _ := stdconfig.Reload(prev, fileSource{v: v})
	return len(changed) > 0, now
}
