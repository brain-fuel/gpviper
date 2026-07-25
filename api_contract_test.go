package viper

import (
	"io"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// These assignments make tier-1 signature drift a compile-time failure.
var (
	_ func() *Viper                           = New
	_ func()                                  = Reset
	_ func(string, any)                       = SetDefault
	_ func(string, any)                       = Set
	_ func(string) any                        = Get
	_ func(string) string                     = GetString
	_ func(string) bool                       = GetBool
	_ func(string) int                        = GetInt
	_ func(string) int64                      = GetInt64
	_ func(string) float64                    = GetFloat64
	_ func(string) time.Duration              = GetDuration
	_ func(string) []string                   = GetStringSlice
	_ func(string) bool                       = IsSet
	_ func(string) bool                       = InConfig
	_ func(string, string)                    = RegisterAlias
	_ func(...string) error                   = BindEnv
	_ func(...string)                         = MustBindEnv
	_ func()                                  = AutomaticEnv
	_ func(bool)                              = AllowEmptyEnv
	_ func(string)                            = SetEnvPrefix
	_ func() string                           = GetEnvPrefix
	_ func(*strings.Replacer)                 = SetEnvKeyReplacer
	_ func(string, *pflag.Flag) error         = BindPFlag
	_ func(*pflag.FlagSet) error              = BindPFlags
	_ func(string, FlagValue) error           = BindFlagValue
	_ func(FlagValueSet) error                = BindFlagValues
	_ func(map[string]any) error              = MergeConfigMap
	_ func(string)                            = SetConfigType
	_ func(io.Reader) error                   = ReadConfig
	_ func() []string                         = AllKeys
	_ func() map[string]any                   = AllSettings
	_ func(*Viper, string) any                = (*Viper).Get
	_ func(*Viper, string) string             = (*Viper).GetString
	_ func(*Viper, string) bool               = (*Viper).GetBool
	_ func(*Viper, string) int                = (*Viper).GetInt
	_ func(*Viper, string) int64              = (*Viper).GetInt64
	_ func(*Viper, string) float64            = (*Viper).GetFloat64
	_ func(*Viper, string) time.Duration      = (*Viper).GetDuration
	_ func(*Viper, string) []string           = (*Viper).GetStringSlice
	_ func(*Viper, string, any)               = (*Viper).SetDefault
	_ func(*Viper, string, any)               = (*Viper).Set
	_ func(*Viper, ...string) error           = (*Viper).BindEnv
	_ func(*Viper, string, *pflag.Flag) error = (*Viper).BindPFlag
	_ func(*Viper, *pflag.FlagSet) error      = (*Viper).BindPFlags
	_ func(*Viper, map[string]any) error      = (*Viper).MergeConfigMap
	_ func(*Viper, io.Reader) error           = (*Viper).ReadConfig
)
