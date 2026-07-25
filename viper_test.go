package viper

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	upstream "github.com/spf13/viper"
	stdconfig "goforge.dev/goplus/std/config"
)

type testCodec struct{}

func (testCodec) Encode(values map[string]any) ([]byte, error) {
	return []byte(values["mode"].(string)), nil
}

type finderFunc func(afero.Fs) ([]string, error)

func (finder finderFunc) Find(filesystem afero.Fs) ([]string, error) {
	return finder(filesystem)
}
func (testCodec) Decode(data []byte, values map[string]any) error {
	values["mode"] = string(data)
	return nil
}

type localRemoteFactory struct {
	get      string
	watch    string
	channel  chan *RemoteResponse
	provider RemoteProvider
}

func (factory *localRemoteFactory) Get(provider RemoteProvider) (io.Reader, error) {
	factory.provider = provider
	return strings.NewReader(factory.get), nil
}
func (factory *localRemoteFactory) Watch(provider RemoteProvider) (io.Reader, error) {
	factory.provider = provider
	return strings.NewReader(factory.watch), nil
}
func (factory *localRemoteFactory) WatchChannel(provider RemoteProvider) (<-chan *RemoteResponse, chan bool) {
	factory.provider = provider
	return factory.channel, make(chan bool)
}

type upstreamRemoteFactory struct {
	get      string
	watch    string
	channel  chan *upstream.RemoteResponse
	provider upstream.RemoteProvider
}

func (factory *upstreamRemoteFactory) Get(provider upstream.RemoteProvider) (io.Reader, error) {
	factory.provider = provider
	return strings.NewReader(factory.get), nil
}
func (factory *upstreamRemoteFactory) Watch(provider upstream.RemoteProvider) (io.Reader, error) {
	factory.provider = provider
	return strings.NewReader(factory.watch), nil
}
func (factory *upstreamRemoteFactory) WatchChannel(provider upstream.RemoteProvider) (<-chan *upstream.RemoteResponse, chan bool) {
	factory.provider = provider
	return factory.channel, make(chan bool)
}

func paired(t *testing.T, configure func(ours *Viper, theirs *upstream.Viper), keys ...string) {
	t.Helper()
	ours, theirs := New(), upstream.New()
	configure(ours, theirs)
	for _, key := range keys {
		if got, want := ours.GetString(key), theirs.GetString(key); got != want {
			t.Errorf("GetString(%q) = %q, want %q", key, got, want)
		}
		if got, want := ours.IsSet(key), theirs.IsSet(key); got != want {
			t.Errorf("IsSet(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestPrecedenceDifferential(t *testing.T) {
	t.Setenv("APP_PORT", "9000")
	oursFlags, upstreamFlags := pflag.NewFlagSet("ours", pflag.ContinueOnError), pflag.NewFlagSet("upstream", pflag.ContinueOnError)
	oursFlags.String("port", "7000", "")
	upstreamFlags.String("port", "7000", "")
	_ = oursFlags.Set("port", "10000")
	_ = upstreamFlags.Set("port", "10000")
	paired(t, func(ours *Viper, theirs *upstream.Viper) {
		ours.SetDefault("port", 80)
		theirs.SetDefault("port", 80)
		_ = ours.MergeConfigMap(map[string]any{"port": 8080})
		_ = theirs.MergeConfigMap(map[string]any{"port": 8080})
		ours.SetEnvPrefix("app")
		theirs.SetEnvPrefix("app")
		_ = ours.BindEnv("port")
		_ = theirs.BindEnv("port")
		_ = ours.BindPFlag("port", oursFlags.Lookup("port"))
		_ = theirs.BindPFlag("port", upstreamFlags.Lookup("port"))
		ours.Set("port", 11000)
		theirs.Set("port", 11000)
	}, "port")
}

func TestEveryPrecedenceBoundaryDifferential(t *testing.T) {
	for _, highest := range []string{"default", "config", "environment", "flag", "override"} {
		t.Run(highest, func(t *testing.T) {
			t.Setenv("APP_MODE", "environment")
			ours, theirs := New(), upstream.New()
			ours.SetDefault("mode", "default")
			theirs.SetDefault("mode", "default")
			if highest != "default" {
				_ = ours.MergeConfigMap(map[string]any{"mode": "config"})
				_ = theirs.MergeConfigMap(map[string]any{"mode": "config"})
			}
			if highest == "environment" || highest == "flag" || highest == "override" {
				ours.SetEnvPrefix("app")
				theirs.SetEnvPrefix("app")
				_ = ours.BindEnv("mode")
				_ = theirs.BindEnv("mode")
			}
			if highest == "flag" || highest == "override" {
				of, tf := pflag.NewFlagSet("ours", pflag.ContinueOnError), pflag.NewFlagSet("theirs", pflag.ContinueOnError)
				of.String("mode", "", "")
				tf.String("mode", "", "")
				_ = of.Set("mode", "flag")
				_ = tf.Set("mode", "flag")
				_ = ours.BindPFlag("mode", of.Lookup("mode"))
				_ = theirs.BindPFlag("mode", tf.Lookup("mode"))
			}
			if highest == "override" {
				ours.Set("mode", "override")
				theirs.Set("mode", "override")
			}
			if got, want := ours.GetString("mode"), theirs.GetString("mode"); got != want || got != highest {
				t.Fatalf("got %q, upstream %q", got, want)
			}
		})
	}
}

func TestEnvironmentPoliciesDifferential(t *testing.T) {
	t.Setenv("OLD_MODE", "fallback")
	t.Setenv("EMPTY_MODE", "")
	paired(t, func(ours *Viper, theirs *upstream.Viper) {
		ours.SetDefault("mode", "default")
		theirs.SetDefault("mode", "default")
		_ = ours.BindEnv("mode", "NEW_MODE", "OLD_MODE")
		_ = theirs.BindEnv("mode", "NEW_MODE", "OLD_MODE")
		ours.AutomaticEnv()
		theirs.AutomaticEnv()
		ours.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		theirs.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	}, "mode")
	paired(t, func(ours *Viper, theirs *upstream.Viper) {
		ours.SetDefault("mode", "default")
		theirs.SetDefault("mode", "default")
		ours.AllowEmptyEnv(true)
		theirs.AllowEmptyEnv(true)
		_ = ours.BindEnv("mode", "EMPTY_MODE")
		_ = theirs.BindEnv("mode", "EMPTY_MODE")
	}, "mode")
}

func TestTypedConversionGettersDifferential(t *testing.T) {
	ours, theirs := New(), upstream.New()
	values := map[string]any{
		"integer":      "42",
		"negative":     "-7",
		"boolean":      1,
		"when":         "2026-07-23T10:11:12Z",
		"duration":     "1250ms",
		"ints":         []any{"1", 2, int64(3)},
		"map":          map[any]any{"Port": 8080, "Enabled": true},
		"string_map":   map[string]any{"Port": 8080, "Mode": "safe"},
		"slice_map":    map[string]any{"Names": []any{"one", 2}},
		"size_kb":      "12 KB",
		"size_bytes":   "99B",
		"size_invalid": "-4GB",
	}
	for key, value := range values {
		ours.Set(key, value)
		theirs.Set(key, value)
	}

	if got, want := ours.GetInt32("integer"), theirs.GetInt32("integer"); got != want {
		t.Errorf("GetInt32 = %v, upstream %v", got, want)
	}
	if got, want := ours.GetUint("negative"), theirs.GetUint("negative"); got != want {
		t.Errorf("GetUint = %v, upstream %v", got, want)
	}
	if got, want := ours.GetUint8("integer"), theirs.GetUint8("integer"); got != want {
		t.Errorf("GetUint8 = %v, upstream %v", got, want)
	}
	if got, want := ours.GetUint16("integer"), theirs.GetUint16("integer"); got != want {
		t.Errorf("GetUint16 = %v, upstream %v", got, want)
	}
	if got, want := ours.GetUint32("integer"), theirs.GetUint32("integer"); got != want {
		t.Errorf("GetUint32 = %v, upstream %v", got, want)
	}
	if got, want := ours.GetUint64("integer"), theirs.GetUint64("integer"); got != want {
		t.Errorf("GetUint64 = %v, upstream %v", got, want)
	}
	if got, want := ours.GetBool("boolean"), theirs.GetBool("boolean"); got != want {
		t.Errorf("GetBool = %v, upstream %v", got, want)
	}
	if got, want := ours.GetTime("when"), theirs.GetTime("when"); !got.Equal(want) {
		t.Errorf("GetTime = %v, upstream %v", got, want)
	}
	if got, want := ours.GetDuration("duration"), theirs.GetDuration("duration"); got != want || got != 1250*time.Millisecond {
		t.Errorf("GetDuration = %v, upstream %v", got, want)
	}
	if got, want := ours.GetIntSlice("ints"), theirs.GetIntSlice("ints"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetIntSlice = %#v, upstream %#v", got, want)
	}
	if got, want := ours.GetStringMap("map"), theirs.GetStringMap("map"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetStringMap = %#v, upstream %#v", got, want)
	}
	if got, want := ours.GetStringMapString("string_map"), theirs.GetStringMapString("string_map"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetStringMapString = %#v, upstream %#v", got, want)
	}
	if got, want := ours.GetStringMapStringSlice("slice_map"), theirs.GetStringMapStringSlice("slice_map"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetStringMapStringSlice = %#v, upstream %#v", got, want)
	}
	for _, key := range []string{"size_kb", "size_bytes", "size_invalid"} {
		if got, want := ours.GetSizeInBytes(key), theirs.GetSizeInBytes(key); got != want {
			t.Errorf("GetSizeInBytes(%q) = %v, upstream %v", key, got, want)
		}
	}
}

func TestConfigurationErrorSurfaceDifferential(t *testing.T) {
	if !reflect.DeepEqual(SupportedExts, upstream.SupportedExts) {
		t.Fatalf("SupportedExts = %v, upstream %v", SupportedExts, upstream.SupportedExts)
	}
	for _, test := range []struct {
		kind  string
		input string
	}{
		{"unsupported", "value"},
		{"yaml", "broken: [yaml"},
		{"json", `{"broken":`},
	} {
		ours, theirs := New(), upstream.New()
		ours.SetConfigType(test.kind)
		theirs.SetConfigType(test.kind)
		got := ours.ReadConfig(strings.NewReader(test.input))
		want := theirs.ReadConfig(strings.NewReader(test.input))
		if (got == nil) != (want == nil) {
			t.Errorf("%s: error = %v, upstream %v", test.kind, got, want)
			continue
		}
		if got != nil && got.Error() != want.Error() {
			t.Errorf("%s: error = %q, upstream %q", test.kind, got, want)
		}
		if test.kind == "unsupported" {
			var typed UnsupportedConfigError
			if !errors.As(got, &typed) {
				t.Errorf("unsupported error has type %T", got)
			}
		} else {
			var typed ConfigParseError
			if !errors.As(got, &typed) || errors.Unwrap(got) == nil {
				t.Errorf("parse error has type/unwrap %T/%v", got, errors.Unwrap(got))
			}
		}
	}
	if got, want := ConfigFileAlreadyExistsError("/tmp/config").Error(),
		upstream.ConfigFileAlreadyExistsError("/tmp/config").Error(); got != want {
		t.Errorf("already-exists error = %q, upstream %q", got, want)
	}
	Reset()
	if GetViper() != global {
		t.Fatal("GetViper did not return the package-global instance")
	}
}

func TestFilesAliasesAndCaseDifferential(t *testing.T) {
	for _, test := range []struct{ kind, input string }{
		{"json", `{"server":{"port":8080},"name":"demo"}`},
		{"yaml", "server:\n  port: 8080\nname: demo\n"},
		{"toml", "name = 'demo'\n[server]\nport = 8080\n"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			paired(t, func(ours *Viper, theirs *upstream.Viper) {
				ours.SetConfigType(test.kind)
				theirs.SetConfigType(test.kind)
				if err := ours.ReadConfig(strings.NewReader(test.input)); err != nil {
					t.Fatal(err)
				}
				if err := theirs.ReadConfig(strings.NewReader(test.input)); err != nil {
					t.Fatal(err)
				}
				ours.RegisterAlias("http.port", "server.port")
				theirs.RegisterAlias("http.port", "server.port")
			}, "name", "SERVER.PORT", "http.port")
		})
	}
}

func TestMergeConfigDifferential(t *testing.T) {
	ours, theirs := New(), upstream.New()
	ours.SetConfigType("yaml")
	theirs.SetConfigType("yaml")
	if err := ours.ReadConfig(strings.NewReader("server:\n  host: localhost\n  port: 8080\nmode: safe\n")); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadConfig(strings.NewReader("server:\n  host: localhost\n  port: 8080\nmode: safe\n")); err != nil {
		t.Fatal(err)
	}
	if err := ours.MergeConfig(strings.NewReader("server:\n  port: 9090\nfeature: enabled\n")); err != nil {
		t.Fatal(err)
	}
	if err := theirs.MergeConfig(strings.NewReader("server:\n  port: 9090\nfeature: enabled\n")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"server.host", "server.port", "mode", "feature"} {
		if got, want := ours.Get(key), theirs.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("Get(%q) = %#v, upstream %#v", key, got, want)
		}
	}
}

func TestSubtreeAndSubDifferential(t *testing.T) {
	ours, theirs := New(), upstream.New()
	config := "server:\n  host: localhost\n  tls:\n    enabled: true\nmode: safe\n"
	ours.SetConfigType("yaml")
	theirs.SetConfigType("yaml")
	if err := ours.ReadConfig(strings.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadConfig(strings.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.Get("server"), theirs.Get("server"); !reflect.DeepEqual(got, want) {
		t.Errorf("Get(server) = %#v, upstream %#v", got, want)
	}
	if got, want := ours.IsSet("server"), theirs.IsSet("server"); got != want {
		t.Errorf("IsSet(server) = %v, upstream %v", got, want)
	}
	if got, want := ours.InConfig("server"), theirs.InConfig("server"); got != want {
		t.Errorf("InConfig(server) = %v, upstream %v", got, want)
	}
	ourSub, theirSub := ours.Sub("server"), theirs.Sub("server")
	if (ourSub == nil) != (theirSub == nil) {
		t.Fatalf("Sub(server) = %#v, upstream %#v", ourSub, theirSub)
	}
	for _, key := range []string{"host", "tls.enabled"} {
		if got, want := ourSub.Get(key), theirSub.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("Sub(server).Get(%q) = %#v, upstream %#v", key, got, want)
		}
	}
	if got, want := ours.Sub("mode"), theirs.Sub("mode"); (got == nil) != (want == nil) {
		t.Errorf("Sub(non-map) nil = %v, upstream %v", got == nil, want == nil)
	}
}

func TestUnmarshalFamilyDifferential(t *testing.T) {
	type server struct {
		Host    string        `mapstructure:"host"`
		Timeout time.Duration `mapstructure:"timeout"`
		Ports   []int         `mapstructure:"ports"`
	}
	type configuration struct {
		Server server `mapstructure:"server"`
		Mode   string `mapstructure:"mode"`
	}
	ours, theirs := New(), upstream.New()
	values := map[string]any{
		"server": map[string]any{
			"host": "localhost", "timeout": "1500ms", "ports": "8080,9090",
		},
		"mode": "safe",
	}
	if err := ours.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	if err := theirs.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	var got, want configuration
	if err := ours.Unmarshal(&got); err != nil {
		t.Fatal(err)
	}
	if err := theirs.Unmarshal(&want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarshal = %#v, upstream %#v", got, want)
	}
	var gotServer, wantServer server
	if err := ours.UnmarshalKey("server", &gotServer); err != nil {
		t.Fatal(err)
	}
	if err := theirs.UnmarshalKey("server", &wantServer); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotServer, wantServer) {
		t.Errorf("UnmarshalKey = %#v, upstream %#v", gotServer, wantServer)
	}

	ours.Set("unused", true)
	theirs.Set("unused", true)
	gotErr := ours.UnmarshalExact(&got)
	wantErr := theirs.UnmarshalExact(&want)
	if (gotErr == nil) != (wantErr == nil) {
		t.Errorf("UnmarshalExact error = %v, upstream %v", gotErr, wantErr)
	}

	type hooked struct {
		When time.Time `mapstructure:"when"`
	}
	ours.Set("when", "2026/07/23")
	theirs.Set("when", "2026/07/23")
	hook := DecodeHook(mapstructure.StringToTimeHookFunc("2006/01/02"))
	upstreamHook := upstream.DecodeHook(mapstructure.StringToTimeHookFunc("2006/01/02"))
	var gotHooked, wantHooked hooked
	if err := ours.UnmarshalKey("when", &gotHooked.When, hook); err != nil {
		t.Fatal(err)
	}
	if err := theirs.UnmarshalKey("when", &wantHooked.When, upstreamHook); err != nil {
		t.Fatal(err)
	}
	if !gotHooked.When.Equal(wantHooked.When) {
		t.Errorf("DecodeHook = %v, upstream %v", gotHooked.When, wantHooked.When)
	}
}

func TestConstructionOptionsDifferential(t *testing.T) {
	t.Setenv("APP_SERVER_PORT", "9090")
	ours := NewWithOptions(EnvKeyReplacer(strings.NewReplacer(".", "_")))
	theirs := upstream.NewWithOptions(upstream.EnvKeyReplacer(strings.NewReplacer(".", "_")))
	ours.SetEnvPrefix("app")
	theirs.SetEnvPrefix("app")
	ours.AutomaticEnv()
	theirs.AutomaticEnv()
	if got, want := ours.GetString("server.port"), theirs.GetString("server.port"); got != want {
		t.Errorf("EnvKeyReplacer lookup = %q, upstream %q", got, want)
	}

	hook := mapstructure.StringToTimeHookFunc("2006/01/02")
	ours = NewWithOptions(WithDecodeHook(hook))
	theirs = upstream.NewWithOptions(upstream.WithDecodeHook(hook))
	ours.Set("when", "2026/07/23")
	theirs.Set("when", "2026/07/23")
	var gotTime, wantTime time.Time
	if err := ours.UnmarshalKey("when", &gotTime); err != nil {
		t.Fatal(err)
	}
	if err := theirs.UnmarshalKey("when", &wantTime); err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(wantTime) {
		t.Errorf("WithDecodeHook = %v, upstream %v", gotTime, wantTime)
	}

	Reset()
	upstream.Reset()
	SetOptions(EnvKeyReplacer(strings.NewReplacer(".", "_")))
	upstream.SetOptions(upstream.EnvKeyReplacer(strings.NewReplacer(".", "_")))
	SetEnvPrefix("app")
	upstream.SetEnvPrefix("app")
	AutomaticEnv()
	upstream.AutomaticEnv()
	if got, want := GetString("server.port"), upstream.GetString("server.port"); got != want {
		t.Errorf("SetOptions lookup = %q, upstream %q", got, want)
	}
}

func TestExperimentalBindStructDifferential(t *testing.T) {
	type server struct {
		Port int `mapstructure:"port"`
	}
	type configuration struct {
		Server server `mapstructure:"server"`
		Mode   string `mapstructure:"mode"`
	}
	t.Setenv("APP_SERVER_PORT", "9090")
	t.Setenv("APP_MODE", "automatic")
	ours := NewWithOptions(
		ExperimentalBindStruct(),
		EnvKeyReplacer(strings.NewReplacer(".", "_")),
	)
	theirs := upstream.NewWithOptions(
		upstream.ExperimentalBindStruct(),
		upstream.EnvKeyReplacer(strings.NewReplacer(".", "_")),
	)
	ours.SetEnvPrefix("app")
	theirs.SetEnvPrefix("app")
	ours.AutomaticEnv()
	theirs.AutomaticEnv()
	var got, want configuration
	if err := ours.Unmarshal(&got); err != nil {
		t.Fatal(err)
	}
	if err := theirs.Unmarshal(&want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("experimental bind struct = %#v, upstream %#v", got, want)
	}
}

func TestKeyDelimiterDifferential(t *testing.T) {
	ours := NewWithOptions(KeyDelimiter("::"))
	theirs := upstream.NewWithOptions(upstream.KeyDelimiter("::"))
	values := map[string]any{
		"chart": map[string]any{
			"values": map[string]any{
				"ingress.annotations": "enabled",
				"replicas":            3,
			},
		},
	}
	if err := ours.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	if err := theirs.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"chart",
		"chart::values",
		"chart::values::ingress.annotations",
		"chart::values::replicas",
	} {
		if got, want := ours.Get(key), theirs.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("Get(%q) = %#v, upstream %#v", key, got, want)
		}
	}
	if got, want := ours.AllSettings(), theirs.AllSettings(); !reflect.DeepEqual(got, want) {
		t.Errorf("AllSettings = %#v, upstream %#v", got, want)
	}
	ourSub, theirSub := ours.Sub("chart::values"), theirs.Sub("chart::values")
	if got, want := ourSub.Get("ingress.annotations"), theirSub.Get("ingress.annotations"); !reflect.DeepEqual(got, want) {
		t.Errorf("Sub custom delimiter = %#v, upstream %#v", got, want)
	}
}

func TestTypeByDefaultValueDifferential(t *testing.T) {
	t.Setenv("APP_ENABLED", "true")
	t.Setenv("APP_PORTS", "8080 9090")
	t.Setenv("APP_TIMEOUT", "1500ms")
	ours, theirs := New(), upstream.New()
	for _, registry := range []interface {
		SetDefault(string, any)
		SetEnvPrefix(string)
		AutomaticEnv()
		SetTypeByDefaultValue(bool)
	}{ours, theirs} {
		registry.SetDefault("enabled", false)
		registry.SetDefault("ports", []string{})
		registry.SetDefault("timeout", time.Duration(0))
		registry.SetEnvPrefix("app")
		registry.AutomaticEnv()
		registry.SetTypeByDefaultValue(true)
	}
	for _, key := range []string{"enabled", "ports", "timeout"} {
		if got, want := ours.Get(key), theirs.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("Get(%q) = %#v (%T), upstream %#v (%T)", key, got, got, want, want)
		}
	}
}

func TestConfigFileDiscoveryDifferential(t *testing.T) {
	ourFS, theirFS := afero.NewMemMapFs(), afero.NewMemMapFs()
	for _, filesystem := range []afero.Fs{ourFS, theirFS} {
		if err := filesystem.MkdirAll("/configs", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := afero.WriteFile(filesystem, "/configs/application.yaml", []byte("server:\n  host: localhost\n  port: 8080\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ours, theirs := New(), upstream.New()
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	ours.SetConfigName("application")
	theirs.SetConfigName("application")
	ours.AddConfigPath("/configs")
	theirs.AddConfigPath("/configs")
	if err := ours.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.GetInt("server.port"), theirs.GetInt("server.port"); got != want {
		t.Errorf("discovered server.port = %v, upstream %v", got, want)
	}
	if got, want := ours.ConfigFileUsed(), theirs.ConfigFileUsed(); got != want {
		t.Errorf("ConfigFileUsed = %q, upstream %q", got, want)
	}
	for _, filesystem := range []afero.Fs{ourFS, theirFS} {
		if err := afero.WriteFile(filesystem, "/configs/application.yaml", []byte("server:\n  port: 9090\nfeature: enabled\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ours.MergeInConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.MergeInConfig(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"server.host", "server.port", "feature"} {
		if got, want := ours.Get(key), theirs.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("merged discovered Get(%q) = %#v, upstream %#v", key, got, want)
		}
	}

	ours, theirs = New(), upstream.New()
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	ours.SetConfigFile("/configs/application.yaml")
	theirs.SetConfigFile("/configs/application.yaml")
	if err := ours.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.AllSettings(), theirs.AllSettings(); !reflect.DeepEqual(got, want) {
		t.Errorf("explicit file settings = %#v, upstream %#v", got, want)
	}

	ours, theirs = New(), upstream.New()
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	ours.SetConfigName("missing")
	theirs.SetConfigName("missing")
	ours.AddConfigPath("/configs")
	theirs.AddConfigPath("/configs")
	gotErr, wantErr := ours.ReadInConfig(), theirs.ReadInConfig()
	if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) {
		t.Errorf("missing discovery error = %v, upstream %v", gotErr, wantErr)
	}
	var missing ConfigFileNotFoundError
	if !errors.As(gotErr, &missing) {
		t.Errorf("missing discovery error has type %T", gotErr)
	}
}

func TestFinderOptionsDifferential(t *testing.T) {
	ourFS, theirFS := afero.NewMemMapFs(), afero.NewMemMapFs()
	for _, filesystem := range []afero.Fs{ourFS, theirFS} {
		if err := afero.WriteFile(filesystem, "/selected.yaml", []byte("mode: custom\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	finder := finderFunc(func(afero.Fs) ([]string, error) {
		return []string{"/selected.yaml"}, nil
	})
	ours := NewWithOptions(WithFinder(finder), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	theirs := upstream.NewWithOptions(upstream.WithFinder(finder), upstream.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	if err := ours.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.GetString("mode"), theirs.GetString("mode"); got != want {
		t.Errorf("custom finder mode = %q, upstream %q", got, want)
	}
	if got, want := ours.ConfigFileUsed(), theirs.ConfigFileUsed(); got != want {
		t.Errorf("custom finder file = %q, upstream %q", got, want)
	}

	failure := errors.New("first finder failed")
	combinedOurs := Finders(
		finderFunc(func(afero.Fs) ([]string, error) { return nil, failure }),
		nil,
		finder,
	)
	combinedTheirs := upstream.Finders(
		finderFunc(func(afero.Fs) ([]string, error) { return nil, failure }),
		nil,
		finder,
	)
	gotResults, gotErr := combinedOurs.Find(ourFS)
	wantResults, wantErr := combinedTheirs.Find(theirFS)
	if !reflect.DeepEqual(gotResults, wantResults) ||
		(gotErr == nil) != (wantErr == nil) ||
		(gotErr != nil && gotErr.Error() != wantErr.Error()) {
		t.Errorf("combined finder = %v/%v, upstream %v/%v", gotResults, gotErr, wantResults, wantErr)
	}

	ours = NewWithOptions(ExperimentalFinder())
	theirs = upstream.NewWithOptions(upstream.ExperimentalFinder())
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	ours.SetConfigName("selected")
	theirs.SetConfigName("selected")
	ours.AddConfigPath("/")
	theirs.AddConfigPath("/")
	if err := ours.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestWatchConfigDifferential(t *testing.T) {
	type watchable interface {
		SetConfigFile(string)
		ReadInConfig() error
		OnConfigChange(func(fsnotify.Event))
		WatchConfig()
		GetInt(string) int
	}
	run := func(t *testing.T, registry watchable) (fsnotify.Op, int) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("port: 8080\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		registry.SetConfigFile(path)
		if err := registry.ReadInConfig(); err != nil {
			t.Fatal(err)
		}
		events := make(chan fsnotify.Event, 4)
		registry.OnConfigChange(func(event fsnotify.Event) { events <- event })
		registry.WatchConfig()
		if err := os.WriteFile(path, []byte("port: 9090\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case event := <-events:
			return event.Op, registry.GetInt("port")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for configuration event")
		}
		return 0, 0
	}
	gotOp, gotPort := run(t, New())
	wantOp, wantPort := run(t, upstream.New())
	if gotPort != wantPort || gotPort != 9090 {
		t.Errorf("watched port = %d, upstream %d", gotPort, wantPort)
	}
	if gotOp&(fsnotify.Write|fsnotify.Create) == 0 || wantOp&(fsnotify.Write|fsnotify.Create) == 0 {
		t.Errorf("watch operations = %v, upstream %v", gotOp, wantOp)
	}
}

func TestConfigWriteFamilyDifferential(t *testing.T) {
	values := map[string]any{
		"server":  map[string]any{"host": "localhost", "port": 8080},
		"feature": true,
	}
	for _, kind := range []string{"json", "yaml", "toml"} {
		t.Run(kind, func(t *testing.T) {
			ours, theirs := New(), upstream.New()
			ours.SetConfigType(kind)
			theirs.SetConfigType(kind)
			if err := ours.MergeConfigMap(values); err != nil {
				t.Fatal(err)
			}
			if err := theirs.MergeConfigMap(values); err != nil {
				t.Fatal(err)
			}
			var got, want bytes.Buffer
			if err := ours.WriteConfigTo(&got); err != nil {
				t.Fatal(err)
			}
			if err := theirs.WriteConfigTo(&want); err != nil {
				t.Fatal(err)
			}
			if got.String() != want.String() {
				t.Errorf("WriteConfigTo bytes = %q, upstream %q", got.String(), want.String())
			}
		})
	}

	ourFS, theirFS := afero.NewMemMapFs(), afero.NewMemMapFs()
	ours, theirs := New(), upstream.New()
	ours.SetFs(ourFS)
	theirs.SetFs(theirFS)
	ours.SetConfigType("yaml")
	theirs.SetConfigType("yaml")
	ours.SetConfigPermissions(0o640)
	theirs.SetConfigPermissions(0o640)
	if err := ours.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	if err := theirs.MergeConfigMap(values); err != nil {
		t.Fatal(err)
	}
	const path = "/written.yaml"
	if err := ours.SafeWriteConfigAs(path); err != nil {
		t.Fatal(err)
	}
	if err := theirs.SafeWriteConfigAs(path); err != nil {
		t.Fatal(err)
	}
	gotData, _ := afero.ReadFile(ourFS, path)
	wantData, _ := afero.ReadFile(theirFS, path)
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("SafeWriteConfigAs bytes = %q, upstream %q", gotData, wantData)
	}
	gotInfo, _ := ourFS.Stat(path)
	wantInfo, _ := theirFS.Stat(path)
	if gotInfo.Mode().Perm() != wantInfo.Mode().Perm() {
		t.Errorf("written permissions = %v, upstream %v", gotInfo.Mode().Perm(), wantInfo.Mode().Perm())
	}
	gotErr, wantErr := ours.SafeWriteConfigAs(path), theirs.SafeWriteConfigAs(path)
	if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) {
		t.Errorf("safe overwrite error = %v, upstream %v", gotErr, wantErr)
	}
	var exists ConfigFileAlreadyExistsError
	if !errors.As(gotErr, &exists) {
		t.Errorf("safe overwrite error has type %T", gotErr)
	}

	ours.Set("feature", false)
	theirs.Set("feature", false)
	if err := ours.WriteConfigAs(path); err != nil {
		t.Fatal(err)
	}
	if err := theirs.WriteConfigAs(path); err != nil {
		t.Fatal(err)
	}
	gotData, _ = afero.ReadFile(ourFS, path)
	wantData, _ = afero.ReadFile(theirFS, path)
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("WriteConfigAs bytes = %q, upstream %q", gotData, wantData)
	}
}

func TestCodecRegistryDifferential(t *testing.T) {
	ourRegistry, theirRegistry := NewCodecRegistry(), upstream.NewCodecRegistry()
	if err := ourRegistry.RegisterCodec("ENV", testCodec{}); err != nil {
		t.Fatal(err)
	}
	if err := theirRegistry.RegisterCodec("ENV", testCodec{}); err != nil {
		t.Fatal(err)
	}
	ours := NewWithOptions(WithCodecRegistry(ourRegistry))
	theirs := upstream.NewWithOptions(upstream.WithCodecRegistry(theirRegistry))
	ours.SetConfigType("env")
	theirs.SetConfigType("env")
	if err := ours.ReadConfig(strings.NewReader("custom-value")); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadConfig(strings.NewReader("custom-value")); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.GetString("mode"), theirs.GetString("mode"); got != want {
		t.Errorf("custom decode = %q, upstream %q", got, want)
	}
	var got, want bytes.Buffer
	if err := ours.WriteConfigTo(&got); err != nil {
		t.Fatal(err)
	}
	if err := theirs.WriteConfigTo(&want); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("custom encode = %q, upstream %q", got.String(), want.String())
	}

	ourEncoder, ourEncoderErr := ourRegistry.Encoder("missing")
	theirEncoder, theirEncoderErr := theirRegistry.Encoder("missing")
	if (ourEncoder == nil) != (theirEncoder == nil) ||
		ourEncoderErr.Error() != theirEncoderErr.Error() {
		t.Errorf("missing encoder = %v/%v, upstream %v/%v", ourEncoder, ourEncoderErr, theirEncoder, theirEncoderErr)
	}
	ourDecoder, ourDecoderErr := ourRegistry.Decoder("missing")
	theirDecoder, theirDecoderErr := theirRegistry.Decoder("missing")
	if (ourDecoder == nil) != (theirDecoder == nil) ||
		ourDecoderErr.Error() != theirDecoderErr.Error() {
		t.Errorf("missing decoder = %v/%v, upstream %v/%v", ourDecoder, ourDecoderErr, theirDecoder, theirDecoderErr)
	}
}

func TestDebugSurfaceDifferential(t *testing.T) {
	ours, theirs := New(), upstream.New()
	ours.SetDefault("mode", "safe")
	theirs.SetDefault("mode", "safe")
	ours.Set("port", 8080)
	theirs.Set("port", 8080)
	var got, want bytes.Buffer
	ours.DebugTo(&got)
	theirs.DebugTo(&want)
	for _, heading := range []string{
		"Aliases:", "Override:", "PFlags:", "Env:",
		"Key/Value Store:", "Config:", "Defaults:",
	} {
		if strings.Contains(got.String(), heading) != strings.Contains(want.String(), heading) {
			t.Errorf("%q presence differs: ours=%q upstream=%q", heading, got.String(), want.String())
		}
	}
	for _, value := range []string{"mode", "safe", "port", "8080"} {
		if strings.Contains(got.String(), value) != strings.Contains(want.String(), value) {
			t.Errorf("%q visibility differs: ours=%q upstream=%q", value, got.String(), want.String())
		}
	}
}

func TestRemoteConfigurationDifferential(t *testing.T) {
	oldLocalConfig, oldUpstreamConfig := RemoteConfig, upstream.RemoteConfig
	oldLocalProviders := append([]string(nil), SupportedRemoteProviders...)
	oldUpstreamProviders := append([]string(nil), upstream.SupportedRemoteProviders...)
	defer func() {
		RemoteConfig, upstream.RemoteConfig = oldLocalConfig, oldUpstreamConfig
		SupportedRemoteProviders = oldLocalProviders
		upstream.SupportedRemoteProviders = oldUpstreamProviders
	}()

	ours, theirs := New(), upstream.New()
	ours.SetConfigType("yaml")
	theirs.SetConfigType("yaml")

	ourUnsupported := ours.AddRemoteProvider("unsupported", "localhost", "/app")
	theirUnsupported := theirs.AddRemoteProvider("unsupported", "localhost", "/app")
	if reflect.TypeOf(ourUnsupported).Name() != reflect.TypeOf(theirUnsupported).Name() ||
		ourUnsupported.Error() != theirUnsupported.Error() {
		t.Fatalf("unsupported provider = %T %q, upstream %T %q",
			ourUnsupported, ourUnsupported, theirUnsupported, theirUnsupported)
	}
	if err := ours.AddRemoteProvider("consul", "localhost:8500", "/app"); err != nil {
		t.Fatal(err)
	}
	if err := theirs.AddRemoteProvider("consul", "localhost:8500", "/app"); err != nil {
		t.Fatal(err)
	}
	_ = ours.AddRemoteProvider("consul", "localhost:8500", "/app")
	_ = theirs.AddRemoteProvider("consul", "localhost:8500", "/app")

	ourFactory := &localRemoteFactory{
		get:     "mode: remote\nport: 8080\n",
		watch:   "mode: watched\nport: 9090\n",
		channel: make(chan *RemoteResponse, 1),
	}
	theirFactory := &upstreamRemoteFactory{
		get:     ourFactory.get,
		watch:   ourFactory.watch,
		channel: make(chan *upstream.RemoteResponse, 1),
	}
	RemoteConfig, upstream.RemoteConfig = ourFactory, theirFactory

	if err := ours.ReadRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.ReadRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mode", "port"} {
		if got, want := ours.Get(key), theirs.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("remote %s = %#v, upstream %#v", key, got, want)
		}
	}
	if got, want := []string{
		ourFactory.provider.Provider(), ourFactory.provider.Endpoint(),
		ourFactory.provider.Path(), ourFactory.provider.SecretKeyring(),
	}, []string{
		theirFactory.provider.Provider(), theirFactory.provider.Endpoint(),
		theirFactory.provider.Path(), theirFactory.provider.SecretKeyring(),
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("provider = %#v, upstream %#v", got, want)
	}

	if err := ours.WatchRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	if err := theirs.WatchRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	if got, want := ours.GetInt("port"), theirs.GetInt("port"); got != want {
		t.Errorf("watched port = %d, upstream %d", got, want)
	}

	if err := ours.WatchRemoteConfigOnChannel(); err != nil {
		t.Fatal(err)
	}
	ourFactory.channel <- &RemoteResponse{Value: []byte("mode: channel\n")}
	deadline := time.Now().Add(time.Second)
	for ours.GetString("mode") != "channel" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := ours.GetString("mode"); got != "channel" {
		t.Errorf("channel mode = %q", got)
	}
}

func TestRemoteErrorsAndResetDifferential(t *testing.T) {
	oldLocalConfig, oldUpstreamConfig := RemoteConfig, upstream.RemoteConfig
	defer func() { RemoteConfig, upstream.RemoteConfig = oldLocalConfig, oldUpstreamConfig }()
	RemoteConfig, upstream.RemoteConfig = nil, nil

	ours, theirs := New(), upstream.New()
	ourErr, theirErr := ours.ReadRemoteConfig(), theirs.ReadRemoteConfig()
	if reflect.TypeOf(ourErr).Name() != reflect.TypeOf(theirErr).Name() ||
		ourErr.Error() != theirErr.Error() {
		t.Errorf("missing remote factory = %T %q, upstream %T %q",
			ourErr, ourErr, theirErr, theirErr)
	}

	ourFactory := &localRemoteFactory{}
	theirFactory := &upstreamRemoteFactory{}
	RemoteConfig, upstream.RemoteConfig = ourFactory, theirFactory
	ourErr, theirErr = ours.ReadRemoteConfig(), theirs.ReadRemoteConfig()
	if ourErr.Error() != theirErr.Error() {
		t.Errorf("missing provider = %q, upstream %q", ourErr, theirErr)
	}

	SupportedExts = []string{"custom"}
	SupportedRemoteProviders = []string{"custom"}
	Reset()
	if !reflect.DeepEqual(SupportedExts, upstream.SupportedExts) ||
		!reflect.DeepEqual(SupportedRemoteProviders, upstream.SupportedRemoteProviders) {
		t.Errorf("reset lists = %#v/%#v, upstream %#v/%#v",
			SupportedExts, SupportedRemoteProviders,
			upstream.SupportedExts, upstream.SupportedRemoteProviders)
	}
}

func TestSnapshotIsImmutableAndRetainsProvenance(t *testing.T) {
	v := New()
	v.SetDefault("mode", "safe")
	v.SetDefault("names", []string{"owned"})
	snapshot := v.Snapshot()
	v.Set("mode", "fast")
	if got := snapshot.GetString("mode"); got != "safe" {
		t.Fatalf("snapshot changed: %q", got)
	}
	entry, ok := snapshot.Get("mode")
	if !ok || entry.Source != SourceDefault {
		t.Fatalf("provenance = %#v", entry)
	}
	names, _ := snapshot.Get("names")
	names.Value.([]string)[0] = "mutated"
	again, _ := snapshot.Get("names")
	if again.Value.([]string)[0] != "owned" {
		t.Fatal("snapshot getter exposed mutable storage")
	}
}

func TestFlagWithoutChangeDoesNotOverride(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("mode", "flag", "")
	v := New()
	v.SetDefault("mode", "default")
	_ = v.BindPFlag("mode", flags.Lookup("mode"))
	if got := v.GetString("mode"); got != "default" {
		t.Fatal(got)
	}
}

func TestStandardSnapshotBridge(t *testing.T) {
	v := New()
	v.SetDefault("mode", "safe")
	v.Set("port", 8080)
	snapshot := v.StandardSnapshot(41)
	entry, ok := stdconfig.Untyped(snapshot, "port")
	if !ok || entry.Value != 8080 || !stdconfig.SourceEqual(entry.Source, stdconfig.OverrideSource{}) {
		t.Fatalf("bridge entry = %#v, %v", entry, ok)
	}
}

func TestExhaustiveSnapshotResolution(t *testing.T) {
	registry := New()
	registry.SetDefault("port", 8080)
	snapshot := registry.Snapshot()
	value, source, found := ResolutionGet(snapshot.Resolve("port"))
	if !found || value != 8080 || source != SourceDefault {
		t.Fatalf("port resolution = %#v/%v/%v", value, source, found)
	}
	value, source, found = ResolutionGet(snapshot.Resolve("missing"))
	if found || value != nil || source != 0 {
		t.Fatalf("missing resolution = %#v/%v/%v", value, source, found)
	}
}
