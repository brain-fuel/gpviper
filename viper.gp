// Package viper is a bounded, deterministic compatibility facade for the
// high-use configuration surface of github.com/spf13/viper.
// Package viper is authored in Go+ and generated into portable Go.
package viper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cast"
	"github.com/spf13/pflag"
	"github.com/spf13/afero"
	"go.yaml.in/yaml/v3"
	stdconfig "goforge.dev/goplus/std/config"
)

// FlagValue and FlagValueSet match Viper's migration interfaces.
type FlagValue interface {
	HasChanged() bool
	Name() string
	ValueString() string
	ValueType() string
}

type FlagValueSet interface{ VisitAll(func(FlagValue)) }

type DecoderConfigOption func(*mapstructure.DecoderConfig)

func DecodeHook(hook mapstructure.DecodeHookFunc) DecoderConfigOption {
	return func(config *mapstructure.DecoderConfig) {
		config.DecodeHook = hook
	}
}

type Option interface{ apply(*Viper) }
type optionFunc func(*Viper)

func (option optionFunc) apply(v *Viper) { option(v) }

type StringReplacer interface{ Replace(string) string }

type Finder interface {
	Find(afero.Fs) ([]string, error)
}
type combinedFinder struct{ finders []Finder }

func (combined *combinedFinder) Find(filesystem afero.Fs) ([]string, error) {
	var results []string
	var failures []error
	for _, finder := range combined.finders {
		if finder == nil {
			continue
		}
		found, err := finder.Find(filesystem)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		results = append(results, found...)
	}
	return results, errors.Join(failures...)
}
func Finders(finders ...Finder) Finder {
	return &combinedFinder{finders: append([]Finder(nil), finders...)}
}
func WithFinder(finder Finder) Option {
	return optionFunc(func(v *Viper) {
		if finder != nil {
			v.finder = finder
		}
	})
}
func ExperimentalFinder() Option {
	return optionFunc(func(v *Viper) { v.experimentalFinder = true })
}
func ExperimentalBindStruct() Option {
	return optionFunc(func(v *Viper) { v.experimentalBindStruct = true })
}
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(v *Viper) { v.logger = logger })
}

type Encoder interface {
	Encode(map[string]any) ([]byte, error)
}
type Decoder interface {
	Decode([]byte, map[string]any) error
}
type Codec interface {
	Encoder
	Decoder
}
type EncoderRegistry interface {
	Encoder(string) (Encoder, error)
}
type DecoderRegistry interface {
	Decoder(string) (Decoder, error)
}
type CodecRegistry interface {
	EncoderRegistry
	DecoderRegistry
}

type DefaultCodecRegistry struct {
	mu     sync.RWMutex
	codecs map[string]Codec
}

func NewCodecRegistry() *DefaultCodecRegistry {
	return &DefaultCodecRegistry{codecs: map[string]Codec{}}
}
func (registry *DefaultCodecRegistry) RegisterCodec(format string, codec Codec) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.codecs == nil {
		registry.codecs = map[string]Codec{}
	}
	registry.codecs[strings.ToLower(format)] = codec
	return nil
}
func (registry *DefaultCodecRegistry) codec(format string) (Codec, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	codec, ok := registry.codecs[strings.ToLower(format)]
	return codec, ok
}
func (registry *DefaultCodecRegistry) Encoder(format string) (Encoder, error) {
	if codec, ok := registry.codec(format); ok {
		return codec, nil
	}
	return nil, fmt.Errorf("encoder not found for this format")
}
func (registry *DefaultCodecRegistry) Decoder(format string) (Decoder, error) {
	if codec, ok := registry.codec(format); ok {
		return codec, nil
	}
	return nil, fmt.Errorf("decoder not found for this format")
}

type standardCodec string

func (codec standardCodec) Encode(values map[string]any) ([]byte, error) {
	switch codec {
	case "json":
		return json.MarshalIndent(values, "", "  ")
	case "yaml":
		return yaml.Marshal(values)
	case "toml":
		return toml.Marshal(values)
	}
	return nil, fmt.Errorf("encoder not found for this format")
}
func (codec standardCodec) Decode(data []byte, values map[string]any) error {
	switch codec {
	case "json":
		return json.Unmarshal(data, &values)
	case "yaml":
		return yaml.Unmarshal(data, &values)
	case "toml":
		return toml.Unmarshal(data, &values)
	}
	return fmt.Errorf("decoder not found for this format")
}

func defaultCodecRegistry() *DefaultCodecRegistry {
	registry := NewCodecRegistry()
	_ = registry.RegisterCodec("json", standardCodec("json"))
	_ = registry.RegisterCodec("yaml", standardCodec("yaml"))
	_ = registry.RegisterCodec("yml", standardCodec("yaml"))
	_ = registry.RegisterCodec("toml", standardCodec("toml"))
	return registry
}

func WithEncoderRegistry(registry EncoderRegistry) Option {
	return optionFunc(func(v *Viper) {
		if registry != nil {
			v.encoderRegistry = registry
		}
	})
}
func WithDecoderRegistry(registry DecoderRegistry) Option {
	return optionFunc(func(v *Viper) {
		if registry != nil {
			v.decoderRegistry = registry
		}
	})
}
func WithCodecRegistry(registry CodecRegistry) Option {
	return optionFunc(func(v *Viper) {
		if registry != nil {
			v.encoderRegistry = registry
			v.decoderRegistry = registry
		}
	})
}

func KeyDelimiter(delimiter string) Option {
	return optionFunc(func(v *Viper) {
		v.keyDelimiter = delimiter
	})
}

func EnvKeyReplacer(replacer StringReplacer) Option {
	return optionFunc(func(v *Viper) {
		if replacer != nil {
			v.replacer = replacer
		}
	})
}

func WithDecodeHook(hook mapstructure.DecodeHookFunc) Option {
	return optionFunc(func(v *Viper) {
		if hook != nil {
			v.decodeHook = hook
		}
	})
}

type UnsupportedConfigError string

func (kind UnsupportedConfigError) Error() string {
	return fmt.Sprintf("Unsupported Config Type %q", string(kind))
}

type ConfigParseError struct{ err error }

func (parse ConfigParseError) Error() string {
	return fmt.Sprintf("While parsing config: %s", parse.err.Error())
}
func (parse ConfigParseError) Unwrap() error { return parse.err }

type ConfigFileAlreadyExistsError string

func (path ConfigFileAlreadyExistsError) Error() string {
	return fmt.Sprintf("Config File %q Already Exists", string(path))
}

type ConfigFileNotFoundError struct{ name, locations string }

func (missing ConfigFileNotFoundError) Error() string {
	return fmt.Sprintf("Config File %q Not Found in %q", missing.name, missing.locations)
}

type ConfigMarshalError struct{ err error }

func (marshal ConfigMarshalError) Error() string {
	return fmt.Sprintf("While marshaling config: %s", marshal.err.Error())
}

type RemoteResponse struct {
	Value []byte
	Error error
}
type RemoteProvider interface {
	Provider() string
	Endpoint() string
	Path() string
	SecretKeyring() string
}
type remoteConfigFactory interface {
	Get(RemoteProvider) (io.Reader, error)
	Watch(RemoteProvider) (io.Reader, error)
	WatchChannel(RemoteProvider) (<-chan *RemoteResponse, chan bool)
}

var RemoteConfig remoteConfigFactory
var SupportedRemoteProviders = []string{"etcd", "etcd3", "consul", "firestore", "nats"}

type UnsupportedRemoteProviderError string

func (provider UnsupportedRemoteProviderError) Error() string {
	return fmt.Sprintf("Unsupported Remote Provider Type %q", string(provider))
}
type RemoteConfigError string

func (remote RemoteConfigError) Error() string {
	return fmt.Sprintf("Remote Configurations Error: %s", string(remote))
}

type defaultRemoteProvider struct {
	provider, endpoint, path, secretKeyring string
}

func (provider defaultRemoteProvider) Provider() string      { return provider.provider }
func (provider defaultRemoteProvider) Endpoint() string      { return provider.endpoint }
func (provider defaultRemoteProvider) Path() string          { return provider.path }
func (provider defaultRemoteProvider) SecretKeyring() string { return provider.secretKeyring }

var SupportedExts = []string{
	"json", "toml", "yaml", "yml", "properties", "props", "prop",
	"hcl", "tfvars", "dotenv", "env", "ini",
}

// Source is the winning source for a resolved value.
type Source uint8

const (
	SourceDefault Source = iota + 1
	SourceConfig
	SourceEnvironment
	SourceFlag
	SourceOverride
)

type Resolved struct {
	Value  any
	Source Source
}

// Resolution is the exhaustive Go+ lookup surface. It removes the mutable
// zero-value-plus-bool convention while retaining source provenance.
type Resolution[T any] enum {
	ResolvedValue(Value T, Source Source)
	MissingResolution
}

// ResolutionGet is the ordinary-Go erasure boundary for Resolution.
func ResolutionGet[T any](resolution Resolution[T]) (T, Source, bool) {
	match resolution {
	case ResolvedValue(value, source):
		return value, source, true
	case MissingResolution:
		var zero T
		return zero, 0, false
	}
}

// Snapshot is an immutable, provenance-retaining view. It is safe for
// concurrent lock-free reads and never aliases Viper's mutable maps.
type Snapshot struct{ values map[string]Resolved }

func (s Snapshot) Get(key string) (Resolved, bool) {
	v, ok := s.values[normalize(key)]
	v.Value = cloneValue(v.Value)
	return v, ok
}

func (s Snapshot) GetString(key string) string {
	v, _ := s.Get(key)
	return toString(v.Value)
}

func (s Snapshot) Resolve(key string) Resolution[any] {
	if value, ok := s.Get(key); ok {
		return ResolvedValue(value.Value, value.Source)
	}
	return MissingResolution
}

type Viper struct {
	mu         sync.RWMutex
	defaults   map[string]any
	config     map[string]any
	kvstore    map[string]any
	overrides  map[string]any
	aliases    map[string]string
	env        map[string][]string
	flags      map[string]FlagValue
	automatic  bool
	allowEmpty bool
	envPrefix  string
	replacer   StringReplacer
	configType string
	decodeHook mapstructure.DecodeHookFunc
	keyDelimiter string
	typeByDefault bool
	fs              afero.Fs
	configPaths     []string
	configName      string
	configFile      string
	configPermissions os.FileMode
	encoderRegistry   EncoderRegistry
	decoderRegistry   DecoderRegistry
	onConfigChange    func(fsnotify.Event)
	finder            Finder
	experimentalFinder bool
	experimentalBindStruct bool
	logger            *slog.Logger
	remoteProviders   []defaultRemoteProvider
}

func New() *Viper {
	codecs := defaultCodecRegistry()
	return &Viper{
		defaults: map[string]any{}, config: map[string]any{}, kvstore: map[string]any{}, overrides: map[string]any{},
		aliases: map[string]string{}, env: map[string][]string{}, flags: map[string]FlagValue{},
		keyDelimiter: ".", fs: afero.NewOsFs(), configName: "config",
		configPermissions: 0o644,
		encoderRegistry: codecs, decoderRegistry: codecs,
	}
}

func NewWithOptions(options ...Option) *Viper {
	v := New()
	for _, option := range options {
		option.apply(v)
	}
	return v
}

func normalize(key string) string { return strings.ToLower(strings.TrimSpace(key)) }

func cloneValue(value any) any {
	switch x := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[normalize(k)] = cloneValue(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[normalize(cast.ToString(k))] = cloneValue(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneValue(x[i])
		}
		return out
	case []string:
		return append([]string(nil), x...)
	case []int:
		return append([]int(nil), x...)
	case []time.Duration:
		return append([]time.Duration(nil), x...)
	default:
		return value
	}
}

func flatten(dst map[string]any, prefix string, src map[string]any, delimiter string) {
	for raw, value := range src {
		key := normalize(raw)
		if prefix != "" {
			key = prefix + delimiter + key
		}
		if nested, ok := value.(map[string]any); ok {
			flatten(dst, key, nested, delimiter)
			continue
		}
		dst[key] = cloneValue(value)
	}
}

func (v *Viper) aliasLocked(key string) string {
	key = normalize(key)
	seen := map[string]bool{}
	for !seen[key] {
		seen[key] = true
		next, ok := v.aliases[key]
		if !ok {
			break
		}
		key = next
	}
	return key
}

func (v *Viper) envNameLocked(key string) string {
	name := strings.ToUpper(key)
	if v.replacer != nil {
		name = v.replacer.Replace(name)
	}
	if v.envPrefix != "" {
		name = strings.ToUpper(v.envPrefix) + "_" + name
	}
	return name
}

func (v *Viper) resolveLocked(raw string) (Resolved, bool) {
	key := v.aliasLocked(raw)
	if value, ok := v.overrides[key]; ok {
		return Resolved{cloneValue(value), SourceOverride}, true
	}
	if flag, ok := v.flags[key]; ok && flag.HasChanged() {
		return Resolved{flag.ValueString(), SourceFlag}, true
	}
	names := v.env[key]
	if len(names) == 0 && v.automatic {
		names = []string{v.envNameLocked(key)}
	}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && (value != "" || v.allowEmpty) {
			return Resolved{value, SourceEnvironment}, true
		}
	}
	if value, ok := v.config[key]; ok {
		return Resolved{cloneValue(value), SourceConfig}, true
	}
	if value, ok := v.kvstore[key]; ok {
		return Resolved{cloneValue(value), SourceConfig}, true
	}
	if value, ok := v.defaults[key]; ok {
		return Resolved{cloneValue(value), SourceDefault}, true
	}
	return Resolved{}, false
}

func (v *Viper) SetDefault(key string, value any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.defaults[v.aliasLocked(key)] = cloneValue(value)
}
func (v *Viper) Set(key string, value any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.overrides[v.aliasLocked(key)] = cloneValue(value)
}
func (v *Viper) Get(key string) any {
	v.mu.RLock()
	x, found := v.resolveLocked(key)
	v.mu.RUnlock()
	if found {
		value := x.Value
		v.mu.RLock()
		infer := v.typeByDefault
		template := v.defaults[v.aliasLocked(key)]
		v.mu.RUnlock()
		if infer {
			if template == nil {
				template = value
			}
			switch template.(type) {
			case bool:
				return cast.ToBool(value)
			case string:
				return cast.ToString(value)
			case int, int8, int16, int32:
				return cast.ToInt(value)
			case int64:
				return cast.ToInt64(value)
			case uint:
				return cast.ToUint(value)
			case uint32:
				return cast.ToUint32(value)
			case uint64:
				return cast.ToUint64(value)
			case float32, float64:
				return cast.ToFloat64(value)
			case time.Time:
				return cast.ToTime(value)
			case time.Duration:
				return cast.ToDuration(value)
			case []string:
				return cast.ToStringSlice(value)
			case []int:
				return cast.ToIntSlice(value)
			case []time.Duration:
				return cast.ToDurationSlice(value)
			}
		}
		return value
	}
	normalized := normalize(key)
	v.mu.RLock()
	delimiter := v.keyDelimiter
	v.mu.RUnlock()
	prefix := normalized + delimiter
	subtree := map[string]any{}
	for _, candidate := range v.AllKeys() {
		if strings.HasPrefix(candidate, prefix) {
			setNested(subtree, strings.Split(strings.TrimPrefix(candidate, prefix), delimiter), v.Get(candidate))
		}
	}
	if len(subtree) != 0 {
		return subtree
	}
	return nil
}
func (v *Viper) IsSet(key string) bool {
	v.mu.RLock()
	_, ok := v.resolveLocked(key)
	v.mu.RUnlock()
	return ok || v.Get(key) != nil
}
func (v *Viper) InConfig(key string) bool {
	v.mu.RLock()
	normalized := v.aliasLocked(key)
	_, ok := v.config[normalized]
	if !ok {
		prefix := normalized + v.keyDelimiter
		for candidate := range v.config {
			if strings.HasPrefix(candidate, prefix) {
				ok = true
				break
			}
		}
	}
	v.mu.RUnlock()
	return ok
}

func (v *Viper) GetString(key string) string          { return cast.ToString(v.Get(key)) }
func (v *Viper) GetBool(key string) bool              { return cast.ToBool(v.Get(key)) }
func (v *Viper) GetInt(key string) int                { return cast.ToInt(v.Get(key)) }
func (v *Viper) GetInt32(key string) int32            { return cast.ToInt32(v.Get(key)) }
func (v *Viper) GetInt64(key string) int64            { return cast.ToInt64(v.Get(key)) }
func (v *Viper) GetUint(key string) uint              { return cast.ToUint(v.Get(key)) }
func (v *Viper) GetUint8(key string) uint8            { return cast.ToUint8(v.Get(key)) }
func (v *Viper) GetUint16(key string) uint16          { return cast.ToUint16(v.Get(key)) }
func (v *Viper) GetUint32(key string) uint32          { return cast.ToUint32(v.Get(key)) }
func (v *Viper) GetUint64(key string) uint64          { return cast.ToUint64(v.Get(key)) }
func (v *Viper) GetFloat64(key string) float64        { return cast.ToFloat64(v.Get(key)) }
func (v *Viper) GetTime(key string) time.Time         { return cast.ToTime(v.Get(key)) }
func (v *Viper) GetDuration(key string) time.Duration { return cast.ToDuration(v.Get(key)) }
func (v *Viper) GetIntSlice(key string) []int         { return cast.ToIntSlice(v.Get(key)) }
func (v *Viper) GetStringSlice(key string) []string   { return cast.ToStringSlice(v.Get(key)) }
func (v *Viper) GetStringMap(key string) map[string]any {
	return cast.ToStringMap(v.Get(key))
}
func (v *Viper) GetStringMapString(key string) map[string]string {
	return cast.ToStringMapString(v.Get(key))
}
func (v *Viper) GetStringMapStringSlice(key string) map[string][]string {
	return cast.ToStringMapStringSlice(v.Get(key))
}

func parseSizeInBytes(size string) uint {
	size = strings.TrimSpace(size)
	last := len(size) - 1
	multiplier := uint(1)
	if last > 0 && (size[last] == 'b' || size[last] == 'B') && last > 1 {
		switch unicode.ToLower(rune(size[last-1])) {
		case 'k':
			multiplier, size = 1<<10, strings.TrimSpace(size[:last-1])
		case 'm':
			multiplier, size = 1<<20, strings.TrimSpace(size[:last-1])
		case 'g':
			multiplier, size = 1<<30, strings.TrimSpace(size[:last-1])
		default:
			size = strings.TrimSpace(size[:last])
		}
	}
	value := cast.ToInt(size)
	if value < 0 {
		return 0
	}
	product := uint(value) * multiplier
	if value > 1 && multiplier > 1 && product/multiplier != uint(value) {
		return 0
	}
	return product
}

func (v *Viper) GetSizeInBytes(key string) uint {
	return parseSizeInBytes(cast.ToString(v.Get(key)))
}

func (v *Viper) SetTypeByDefaultValue(enabled bool) {
	v.mu.Lock()
	v.typeByDefault = enabled
	v.mu.Unlock()
}

func (v *Viper) Sub(key string) *Viper {
	data := v.Get(key)
	if data == nil {
		return nil
	}
	if reflect.TypeOf(data).Kind() != reflect.Map {
		return nil
	}
	values, ok := data.(map[string]any)
	if !ok {
		converted := cast.ToStringMap(data)
		if converted == nil {
			return nil
		}
		values = converted
	}
	child := New()
	v.mu.RLock()
	child.automatic = v.automatic
	child.allowEmpty = v.allowEmpty
	child.envPrefix = v.envPrefix
	child.replacer = v.replacer
	child.keyDelimiter = v.keyDelimiter
	child.typeByDefault = v.typeByDefault
	child.decodeHook = v.decodeHook
	child.encoderRegistry = v.encoderRegistry
	child.decoderRegistry = v.decoderRegistry
	child.finder = v.finder
	child.experimentalFinder = v.experimentalFinder
	child.experimentalBindStruct = v.experimentalBindStruct
	child.logger = v.logger
	v.mu.RUnlock()
	_ = child.MergeConfigMap(values)
	return child
}

func stringToWeakSliceHook(separator string) mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to.Kind() != reflect.Slice {
			return data, nil
		}
		raw := data.(string)
		if raw == "" {
			return []string{}, nil
		}
		return strings.Split(raw, separator), nil
	}
}

func (v *Viper) decoderConfig(output any, exact bool, options ...DecoderConfigOption) *mapstructure.DecoderConfig {
	hook := v.decodeHook
	if hook == nil {
		hook = mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			stringToWeakSliceHook(","),
		)
	}
	config := &mapstructure.DecoderConfig{
		Result:           output,
		WeaklyTypedInput: true,
		ErrorUnused:      exact,
		DecodeHook:       hook,
	}
	for _, option := range options {
		option(config)
	}
	config.Result = output
	config.ErrorUnused = exact
	return config
}

func (v *Viper) decodeInto(input, output any, exact bool, options ...DecoderConfigOption) error {
	decoder, err := mapstructure.NewDecoder(v.decoderConfig(output, exact, options...))
	if err != nil {
		return err
	}
	return decoder.Decode(input)
}

func collectStructKeys(kind reflect.Type, prefix, delimiter string, keys *[]string) {
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	if kind.Kind() != reflect.Struct {
		return
	}
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := strings.Split(field.Tag.Get("mapstructure"), ",")[0]
		if tag == "-" {
			continue
		}
		name := tag
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		path := name
		if prefix != "" {
			path = prefix + delimiter + name
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct && fieldType != reflect.TypeOf(time.Time{}) {
			collectStructKeys(field.Type, path, delimiter, keys)
			continue
		}
		*keys = append(*keys, path)
	}
}

func (v *Viper) unmarshalSettings(output any) map[string]any {
	settings := v.AllSettings()
	v.mu.RLock()
	enabled := v.experimentalBindStruct
	delimiter := v.keyDelimiter
	v.mu.RUnlock()
	if !enabled {
		return settings
	}
	var keys []string
	collectStructKeys(reflect.TypeOf(output), "", delimiter, &keys)
	for _, key := range keys {
		if value := v.Get(key); value != nil {
			setNested(settings, strings.Split(key, delimiter), value)
		}
	}
	return settings
}

func (v *Viper) UnmarshalKey(key string, output any, options ...DecoderConfigOption) error {
	return v.decodeInto(v.Get(key), output, false, options...)
}
func (v *Viper) Unmarshal(output any, options ...DecoderConfigOption) error {
	return v.decodeInto(v.unmarshalSettings(output), output, false, options...)
}
func (v *Viper) UnmarshalExact(output any, options ...DecoderConfigOption) error {
	return v.decodeInto(v.unmarshalSettings(output), output, true, options...)
}

func toString(value any) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	case fmt.Stringer:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func (v *Viper) RegisterAlias(alias, key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	a, target := normalize(alias), v.aliasLocked(key)
	if a != "" && a != target {
		v.aliases[a] = target
	}
}

func (v *Viper) BindEnv(input ...string) error {
	if len(input) == 0 {
		return fmt.Errorf("missing key to bind to")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	key := v.aliasLocked(input[0])
	names := append([]string(nil), input[1:]...)
	if len(names) == 0 {
		names = []string{v.envNameLocked(key)}
	}
	v.env[key] = names
	return nil
}
func (v *Viper) MustBindEnv(input ...string) {
	if err := v.BindEnv(input...); err != nil {
		panic(err)
	}
}
func (v *Viper) AutomaticEnv()                         { v.mu.Lock(); v.automatic = true; v.mu.Unlock() }
func (v *Viper) AllowEmptyEnv(allow bool)              { v.mu.Lock(); v.allowEmpty = allow; v.mu.Unlock() }
func (v *Viper) SetEnvPrefix(prefix string)            { v.mu.Lock(); v.envPrefix = prefix; v.mu.Unlock() }
func (v *Viper) GetEnvPrefix() string                  { v.mu.RLock(); defer v.mu.RUnlock(); return v.envPrefix }
func (v *Viper) SetEnvKeyReplacer(r *strings.Replacer) { v.mu.Lock(); v.replacer = r; v.mu.Unlock() }

func (v *Viper) BindFlagValue(key string, flag FlagValue) error {
	if flag == nil {
		return fmt.Errorf("flag for %q is nil", key)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.flags[v.aliasLocked(key)] = flag
	return nil
}
func (v *Viper) BindFlagValues(flags FlagValueSet) error {
	if flags == nil {
		return fmt.Errorf("flag value set is nil")
	}
	flags.VisitAll(func(flag FlagValue) { _ = v.BindFlagValue(flag.Name(), flag) })
	return nil
}

type pflagValue struct{ flag *pflag.Flag }

func (p pflagValue) HasChanged() bool    { return p.flag.Changed }
func (p pflagValue) Name() string        { return p.flag.Name }
func (p pflagValue) ValueString() string { return p.flag.Value.String() }
func (p pflagValue) ValueType() string   { return p.flag.Value.Type() }
func (v *Viper) BindPFlag(key string, flag *pflag.Flag) error {
	if flag == nil {
		return fmt.Errorf("flag for %q is nil", key)
	}
	return v.BindFlagValue(key, pflagValue{flag})
}
func (v *Viper) BindPFlags(flags *pflag.FlagSet) error {
	if flags == nil {
		return fmt.Errorf("flag set is nil")
	}
	var first error
	flags.VisitAll(func(flag *pflag.Flag) {
		if err := v.BindPFlag(flag.Name, flag); first == nil {
			first = err
		}
	})
	return first
}

func (v *Viper) MergeConfigMap(values map[string]any) error {
	flat := map[string]any{}
	v.mu.Lock()
	defer v.mu.Unlock()
	flatten(flat, "", values, v.keyDelimiter)
	for key, value := range flat {
		v.config[key] = value
	}
	return nil
}
func (v *Viper) SetConfigType(kind string) {
	v.mu.Lock()
	v.configType = strings.ToLower(kind)
	v.mu.Unlock()
}
func (v *Viper) SetFs(filesystem afero.Fs) {
	v.mu.Lock()
	v.fs = filesystem
	v.mu.Unlock()
}
func (v *Viper) SetConfigFile(path string) {
	if path == "" {
		return
	}
	v.mu.Lock()
	v.configFile = path
	v.mu.Unlock()
}
func (v *Viper) SetConfigName(name string) {
	if name == "" {
		return
	}
	v.mu.Lock()
	v.configName = name
	v.configFile = ""
	v.mu.Unlock()
}
func (v *Viper) SetConfigPermissions(permissions os.FileMode) {
	v.mu.Lock()
	v.configPermissions = permissions.Perm()
	v.mu.Unlock()
}
func (v *Viper) AddConfigPath(path string) {
	if path == "" {
		return
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, existing := range v.configPaths {
		if existing == absolute {
			return
		}
	}
	v.configPaths = append(v.configPaths, absolute)
}
func (v *Viper) ConfigFileUsed() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.configFile
}

func (v *Viper) OnConfigChange(callback func(fsnotify.Event)) {
	v.mu.Lock()
	v.onConfigChange = callback
	v.mu.Unlock()
}

func (v *Viper) WatchConfig() {
	path, err := v.configFilePath()
	if err != nil {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	directory := filepath.Dir(path)
	if err := watcher.Add(directory); err != nil {
		_ = watcher.Close()
		return
	}
	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(event.Name) != filepath.Clean(path) ||
					event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				if err := v.ReadInConfig(); err != nil {
					continue
				}
				v.mu.RLock()
				callback := v.onConfigChange
				v.mu.RUnlock()
				if callback != nil {
					callback(event)
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

func (v *Viper) configFilePath() (string, error) {
	v.mu.RLock()
	explicit := v.configFile
	name := v.configName
	paths := append([]string(nil), v.configPaths...)
	kind := v.configType
	filesystem := v.fs
	finder := v.finder
	v.mu.RUnlock()
	if explicit != "" {
		return explicit, nil
	}
	if finder != nil {
		results, err := finder.Find(filesystem)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return "", ConfigFileNotFoundError{name: name, locations: fmt.Sprint(paths)}
		}
		selected := filepath.Clean(results[0])
		v.mu.Lock()
		v.configFile = selected
		v.mu.Unlock()
		return selected, nil
	}
	for _, path := range paths {
		for _, extension := range SupportedExts {
			candidate := filepath.Join(path, name+"."+extension)
			if info, err := filesystem.Stat(candidate); err == nil && !info.IsDir() {
				v.mu.Lock()
				v.configFile = candidate
				v.mu.Unlock()
				return candidate, nil
			}
		}
		if kind != "" {
			candidate := filepath.Join(path, name)
			if info, err := filesystem.Stat(candidate); err == nil && !info.IsDir() {
				v.mu.Lock()
				v.configFile = candidate
				v.mu.Unlock()
				return candidate, nil
			}
		}
	}
	return "", ConfigFileNotFoundError{name: name, locations: fmt.Sprint(paths)}
}

func (v *Viper) ReadInConfig() error {
	path, err := v.configFilePath()
	if err != nil {
		return err
	}
	v.mu.RLock()
	filesystem := v.fs
	kind := v.configType
	v.mu.RUnlock()
	if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); extension != "" {
		kind = extension
	}
	data, err := afero.ReadFile(filesystem, path)
	if err != nil {
		return err
	}
	v.SetConfigType(kind)
	return v.ReadConfig(strings.NewReader(string(data)))
}

func (v *Viper) MergeInConfig() error {
	path, err := v.configFilePath()
	if err != nil {
		return err
	}
	v.mu.RLock()
	filesystem := v.fs
	kind := v.configType
	v.mu.RUnlock()
	if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); extension != "" {
		kind = extension
	}
	data, err := afero.ReadFile(filesystem, path)
	if err != nil {
		return err
	}
	v.SetConfigType(kind)
	return v.MergeConfig(strings.NewReader(string(data)))
}

func (v *Viper) marshalConfig(kind string) ([]byte, error) {
	settings := v.AllSettings()
	kind = strings.ToLower(kind)
	supported := false
	for _, extension := range SupportedExts {
		if extension == kind {
			supported = true
			break
		}
	}
	if !supported {
		return nil, UnsupportedConfigError(kind)
	}
	v.mu.RLock()
	registry := v.encoderRegistry
	v.mu.RUnlock()
	encoder, err := registry.Encoder(kind)
	if err != nil {
		return nil, ConfigMarshalError{err}
	}
	data, err := encoder.Encode(settings)
	if err != nil {
		return nil, ConfigMarshalError{err}
	}
	return data, nil
}

func (v *Viper) WriteConfigTo(writer io.Writer) error {
	v.mu.RLock()
	kind := v.configType
	path := v.configFile
	v.mu.RUnlock()
	if kind == "" {
		kind = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	data, err := v.marshalConfig(kind)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return ConfigMarshalError{err}
	}
	return nil
}

func (v *Viper) writeConfig(path string, force bool) error {
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	v.mu.RLock()
	if kind == "" {
		kind = v.configType
	}
	filesystem := v.fs
	permissions := v.configPermissions
	v.mu.RUnlock()
	if kind == "" {
		return fmt.Errorf("config type could not be determined for %s", path)
	}
	data, err := v.marshalConfig(kind)
	if err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	if !force {
		flags |= os.O_EXCL
	}
	file, err := filesystem.OpenFile(path, flags, permissions)
	if err != nil {
		if !force {
			if exists, existsErr := afero.Exists(filesystem, path); existsErr == nil && exists {
				return ConfigFileAlreadyExistsError(path)
			}
		}
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return ConfigMarshalError{err}
	}
	return file.Sync()
}

func (v *Viper) WriteConfig() error {
	path, err := v.configFilePath()
	if err != nil {
		return err
	}
	return v.writeConfig(path, true)
}
func (v *Viper) WriteConfigAs(path string) error { return v.writeConfig(path, true) }
func (v *Viper) SafeWriteConfigAs(path string) error {
	v.mu.RLock()
	filesystem := v.fs
	v.mu.RUnlock()
	if exists, err := afero.Exists(filesystem, path); err == nil && exists {
		return ConfigFileAlreadyExistsError(path)
	}
	return v.writeConfig(path, false)
}
func (v *Viper) SafeWriteConfig() error {
	v.mu.RLock()
	paths := append([]string(nil), v.configPaths...)
	name, kind := v.configName, v.configType
	v.mu.RUnlock()
	if len(paths) == 0 {
		return fmt.Errorf("missing configuration for 'configPath'")
	}
	return v.SafeWriteConfigAs(filepath.Join(paths[0], name+"."+kind))
}

func (v *Viper) ReadConfig(reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	v.mu.RLock()
	kind := v.configType
	registry := v.decoderRegistry
	v.mu.RUnlock()
	values := map[string]any{}
	supported := false
	for _, extension := range SupportedExts {
		if extension == kind {
			supported = true
			break
		}
	}
	if !supported {
		return UnsupportedConfigError(kind)
	}
	decoder, err := registry.Decoder(kind)
	if err == nil {
		err = decoder.Decode(data, values)
	}
	if err != nil {
		return ConfigParseError{err}
	}
	v.mu.Lock()
	v.config = map[string]any{}
	v.mu.Unlock()
	return v.MergeConfigMap(values)
}

func (v *Viper) MergeConfig(reader io.Reader) error {
	v.mu.RLock()
	kind := v.configType
	v.mu.RUnlock()
	incoming := New()
	incoming.SetConfigType(kind)
	if err := incoming.ReadConfig(reader); err != nil {
		return err
	}
	incoming.mu.RLock()
	values := cloneValue(incoming.config).(map[string]any)
	incoming.mu.RUnlock()
	return v.MergeConfigMap(values)
}

func (v *Viper) AllKeys() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	set := map[string]bool{}
	for _, source := range []map[string]any{v.defaults, v.kvstore, v.config, v.overrides} {
		for key := range source {
			set[key] = true
		}
	}
	for key := range v.env {
		set[key] = true
	}
	for key := range v.flags {
		set[key] = true
	}
	for key := range v.aliases {
		set[key] = true
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (v *Viper) AllSettings() map[string]any {
	out := map[string]any{}
	v.mu.RLock()
	delimiter := v.keyDelimiter
	v.mu.RUnlock()
	for _, key := range v.AllKeys() {
		setNested(out, strings.Split(key, delimiter), v.Get(key))
	}
	return out
}

func remoteProviderSupported(provider string) bool {
	for _, supported := range SupportedRemoteProviders {
		if provider == supported {
			return true
		}
	}
	return false
}

func (v *Viper) addRemoteProvider(provider, endpoint, path, secret string) error {
	if !remoteProviderSupported(provider) {
		return UnsupportedRemoteProviderError(provider)
	}
	if provider == "" || endpoint == "" {
		return nil
	}
	candidate := defaultRemoteProvider{
		provider: provider, endpoint: endpoint, path: path, secretKeyring: secret,
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, existing := range v.remoteProviders {
		if existing == candidate {
			return nil
		}
	}
	v.remoteProviders = append(v.remoteProviders, candidate)
	return nil
}
func (v *Viper) AddRemoteProvider(provider, endpoint, path string) error {
	return v.addRemoteProvider(provider, endpoint, path, "")
}
func (v *Viper) AddSecureRemoteProvider(provider, endpoint, path, secret string) error {
	return v.addRemoteProvider(provider, endpoint, path, secret)
}

func (v *Viper) decodeRemote(reader io.Reader) (map[string]any, error) {
	v.mu.RLock()
	kind := v.configType
	registry := v.decoderRegistry
	delimiter := v.keyDelimiter
	v.mu.RUnlock()
	incoming := NewWithOptions(WithDecoderRegistry(registry), KeyDelimiter(delimiter))
	incoming.SetConfigType(kind)
	if err := incoming.ReadConfig(reader); err != nil {
		return nil, err
	}
	incoming.mu.RLock()
	defer incoming.mu.RUnlock()
	return cloneValue(incoming.config).(map[string]any), nil
}

func (v *Viper) remoteProviderSnapshot() ([]defaultRemoteProvider, error) {
	v.mu.RLock()
	providers := append([]defaultRemoteProvider(nil), v.remoteProviders...)
	v.mu.RUnlock()
	if len(providers) == 0 {
		return nil, RemoteConfigError("No Remote Providers")
	}
	return providers, nil
}

func (v *Viper) ReadRemoteConfig() error {
	if RemoteConfig == nil {
		return RemoteConfigError("Enable the remote features by doing a blank import of the viper/remote package: '_ github.com/spf13/viper/remote'")
	}
	providers, err := v.remoteProviderSnapshot()
	if err != nil {
		return err
	}
	for _, provider := range providers {
		reader, getErr := RemoteConfig.Get(provider)
		if getErr != nil {
			continue
		}
		values, decodeErr := v.decodeRemote(reader)
		if decodeErr != nil {
			continue
		}
		v.mu.Lock()
		v.kvstore = values
		v.mu.Unlock()
		return nil
	}
	return RemoteConfigError("No Files Found")
}

func (v *Viper) WatchRemoteConfig() error {
	if RemoteConfig == nil {
		return RemoteConfigError("Enable the remote features by doing a blank import of the viper/remote package: '_ github.com/spf13/viper/remote'")
	}
	providers, err := v.remoteProviderSnapshot()
	if err != nil {
		return err
	}
	for _, provider := range providers {
		reader, watchErr := RemoteConfig.Watch(provider)
		if watchErr != nil {
			continue
		}
		values, decodeErr := v.decodeRemote(reader)
		if decodeErr != nil {
			continue
		}
		v.mu.Lock()
		v.kvstore = values
		v.mu.Unlock()
		return nil
	}
	return RemoteConfigError("No Files Found")
}

func (v *Viper) WatchRemoteConfigOnChannel() error {
	providers, err := v.remoteProviderSnapshot()
	if err != nil {
		return err
	}
	responses, _ := RemoteConfig.WatchChannel(providers[0])
	go func() {
		for response := range responses {
			if response == nil {
				continue
			}
			values, decodeErr := v.decodeRemote(strings.NewReader(string(response.Value)))
			if decodeErr != nil {
				continue
			}
			v.mu.Lock()
			v.kvstore = values
			v.mu.Unlock()
		}
	}()
	return nil
}

func setNested(dst map[string]any, path []string, value any) {
	if len(path) == 1 {
		dst[path[0]] = value
		return
	}
	next, ok := dst[path[0]].(map[string]any)
	if !ok {
		next = map[string]any{}
		dst[path[0]] = next
	}
	setNested(next, path[1:], value)
}
func (v *Viper) Snapshot() Snapshot {
	keys := v.AllKeys()
	values := make(map[string]Resolved, len(keys))
	v.mu.RLock()
	defer v.mu.RUnlock()
	for _, key := range keys {
		if value, ok := v.resolveLocked(key); ok {
			values[key] = value
		}
	}
	return Snapshot{values}
}

// StandardSnapshot exports the resolved view into the Go+-authored
// schema-indexed standard representation. Go callers provide the retained
// schema ID; Go+ consumers normally construct and use Snapshot[s] directly.
func (v *Viper) StandardSnapshot(schema int) stdconfig.Snapshot {
	snapshot := v.Snapshot()
	layers := map[Source]map[string]any{}
	for key, entry := range snapshot.values {
		values := layers[entry.Source]
		if values == nil {
			values = map[string]any{}
			layers[entry.Source] = values
		}
		values[key] = entry.Value
	}
	ordered := []struct {
		facade   Source
		standard stdconfig.Source
	}{
		{SourceDefault, stdconfig.DefaultSource{}},
		{SourceConfig, stdconfig.FileSource{}},
		{SourceEnvironment, stdconfig.EnvironmentSource{}},
		{SourceFlag, stdconfig.FlagSource{}},
		{SourceOverride, stdconfig.OverrideSource{}},
	}
	standardLayers := make([]stdconfig.Layer, 0, len(ordered))
	for _, pair := range ordered {
		if values := layers[pair.facade]; len(values) != 0 {
			standardLayers = append(standardLayers, stdconfig.Layer{Source: pair.standard, Values: values})
		}
	}
	return stdconfig.Resolve(schema, standardLayers...)
}

func (v *Viper) Debug() { v.DebugTo(os.Stdout) }
func (v *Viper) DebugTo(writer io.Writer) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, _ = fmt.Fprintf(writer, "Aliases:\n%#v\n", v.aliases)
	_, _ = fmt.Fprintf(writer, "Override:\n%#v\n", v.overrides)
	_, _ = fmt.Fprintf(writer, "PFlags:\n%#v\n", v.flags)
	_, _ = fmt.Fprintf(writer, "Env:\n%#v\n", v.env)
	_, _ = fmt.Fprintf(writer, "Key/Value Store:\n%#v\n", v.kvstore)
	_, _ = fmt.Fprintf(writer, "Config:\n%#v\n", v.config)
	_, _ = fmt.Fprintf(writer, "Defaults:\n%#v\n", v.defaults)
}

var global = New()

func Reset() {
	global = New()
	SupportedExts = []string{"json", "toml", "yaml", "yml", "properties", "props", "prop", "hcl", "tfvars", "dotenv", "env", "ini"}
	SupportedRemoteProviders = []string{"etcd", "etcd3", "consul", "firestore", "nats"}
}
func SetOptions(options ...Option) {
	for _, option := range options {
		option.apply(global)
	}
}
func GetViper() *Viper                               { return global }
func Debug()                                         { global.Debug() }
func DebugTo(writer io.Writer)                       { global.DebugTo(writer) }
func SetDefault(key string, value any)               { global.SetDefault(key, value) }
func Set(key string, value any)                      { global.Set(key, value) }
func Get(key string) any                             { return global.Get(key) }
func GetString(key string) string                    { return global.GetString(key) }
func GetBool(key string) bool                        { return global.GetBool(key) }
func GetInt(key string) int                          { return global.GetInt(key) }
func GetInt32(key string) int32                      { return global.GetInt32(key) }
func GetInt64(key string) int64                      { return global.GetInt64(key) }
func GetUint(key string) uint                        { return global.GetUint(key) }
func GetUint8(key string) uint8                      { return global.GetUint8(key) }
func GetUint16(key string) uint16                    { return global.GetUint16(key) }
func GetUint32(key string) uint32                    { return global.GetUint32(key) }
func GetUint64(key string) uint64                    { return global.GetUint64(key) }
func GetFloat64(key string) float64                  { return global.GetFloat64(key) }
func GetTime(key string) time.Time                   { return global.GetTime(key) }
func GetDuration(key string) time.Duration           { return global.GetDuration(key) }
func GetIntSlice(key string) []int                   { return global.GetIntSlice(key) }
func GetStringSlice(key string) []string             { return global.GetStringSlice(key) }
func GetStringMap(key string) map[string]any         { return global.GetStringMap(key) }
func GetStringMapString(key string) map[string]string {
	return global.GetStringMapString(key)
}
func GetStringMapStringSlice(key string) map[string][]string {
	return global.GetStringMapStringSlice(key)
}
func GetSizeInBytes(key string) uint { return global.GetSizeInBytes(key) }
func Sub(key string) *Viper          { return global.Sub(key) }
func UnmarshalKey(key string, output any, options ...DecoderConfigOption) error {
	return global.UnmarshalKey(key, output, options...)
}
func Unmarshal(output any, options ...DecoderConfigOption) error {
	return global.Unmarshal(output, options...)
}
func UnmarshalExact(output any, options ...DecoderConfigOption) error {
	return global.UnmarshalExact(output, options...)
}
func IsSet(key string) bool                          { return global.IsSet(key) }
func InConfig(key string) bool                       { return global.InConfig(key) }
func RegisterAlias(alias, key string)                { global.RegisterAlias(alias, key) }
func BindEnv(input ...string) error                  { return global.BindEnv(input...) }
func MustBindEnv(input ...string)                    { global.MustBindEnv(input...) }
func AutomaticEnv()                                  { global.AutomaticEnv() }
func AllowEmptyEnv(allow bool)                       { global.AllowEmptyEnv(allow) }
func SetEnvPrefix(prefix string)                     { global.SetEnvPrefix(prefix) }
func GetEnvPrefix() string                           { return global.GetEnvPrefix() }
func SetEnvKeyReplacer(r *strings.Replacer)          { global.SetEnvKeyReplacer(r) }
func SetTypeByDefaultValue(enabled bool)              { global.SetTypeByDefaultValue(enabled) }
func BindPFlag(key string, flag *pflag.Flag) error   { return global.BindPFlag(key, flag) }
func BindPFlags(flags *pflag.FlagSet) error          { return global.BindPFlags(flags) }
func BindFlagValue(key string, flag FlagValue) error { return global.BindFlagValue(key, flag) }
func BindFlagValues(flags FlagValueSet) error        { return global.BindFlagValues(flags) }
func MergeConfigMap(values map[string]any) error     { return global.MergeConfigMap(values) }
func AddRemoteProvider(provider, endpoint, path string) error {
	return global.AddRemoteProvider(provider, endpoint, path)
}
func AddSecureRemoteProvider(provider, endpoint, path, secret string) error {
	return global.AddSecureRemoteProvider(provider, endpoint, path, secret)
}
func ReadRemoteConfig() error  { return global.ReadRemoteConfig() }
func WatchRemoteConfig() error { return global.WatchRemoteConfig() }
func SetConfigType(kind string)                      { global.SetConfigType(kind) }
func SetFs(filesystem afero.Fs)                      { global.SetFs(filesystem) }
func SetConfigFile(path string)                      { global.SetConfigFile(path) }
func SetConfigName(name string)                      { global.SetConfigName(name) }
func SetConfigPermissions(permissions os.FileMode)   { global.SetConfigPermissions(permissions) }
func AddConfigPath(path string)                      { global.AddConfigPath(path) }
func ConfigFileUsed() string                         { return global.ConfigFileUsed() }
func OnConfigChange(callback func(fsnotify.Event))    { global.OnConfigChange(callback) }
func WatchConfig()                                    { global.WatchConfig() }
func ReadInConfig() error                            { return global.ReadInConfig() }
func MergeInConfig() error                           { return global.MergeInConfig() }
func WriteConfig() error                             { return global.WriteConfig() }
func WriteConfigAs(path string) error                { return global.WriteConfigAs(path) }
func SafeWriteConfig() error                         { return global.SafeWriteConfig() }
func SafeWriteConfigAs(path string) error            { return global.SafeWriteConfigAs(path) }
func WriteConfigTo(writer io.Writer) error            { return global.WriteConfigTo(writer) }
func ReadConfig(reader io.Reader) error              { return global.ReadConfig(reader) }
func MergeConfig(reader io.Reader) error             { return global.MergeConfig(reader) }
func AllKeys() []string                              { return global.AllKeys() }
func AllSettings() map[string]any                    { return global.AllSettings() }
