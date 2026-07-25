// Command apimanifest inventories a pinned spf13/viper checkout.
package main

import (
	"encoding/csv"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type symbol struct{ pkg, kind, name, file string }

var compatible = map[string]bool{
	"Viper": true, "FlagValue": true, "FlagValueSet": true,
	"DecoderConfigOption": true, "DecodeHook": true,
	"Option": true, "StringReplacer": true,
	"Finder": true, "Finders": true, "WithFinder": true,
	"ExperimentalFinder": true, "ExperimentalBindStruct": true,
	"WithLogger": true,
	"Encoder": true, "Decoder": true, "Codec": true,
	"EncoderRegistry": true, "DecoderRegistry": true, "CodecRegistry": true,
	"DefaultCodecRegistry": true, "NewCodecRegistry": true,
	"DefaultCodecRegistry.RegisterCodec": true,
	"DefaultCodecRegistry.Encoder": true, "DefaultCodecRegistry.Decoder": true,
	"UnsupportedConfigError": true, "ConfigParseError": true,
	"ConfigFileAlreadyExistsError": true, "ConfigFileNotFoundError": true,
	"ConfigMarshalError": true, "SupportedExts": true,
	"RemoteResponse": true, "RemoteProvider": true, "RemoteConfig": true,
	"SupportedRemoteProviders": true,
	"UnsupportedRemoteProviderError": true, "UnsupportedRemoteProviderError.Error": true,
	"RemoteConfigError": true, "RemoteConfigError.Error": true,
	"UnsupportedConfigError.Error": true,
	"ConfigParseError.Error": true, "ConfigParseError.Unwrap": true,
	"ConfigFileAlreadyExistsError.Error": true,
	"ConfigFileNotFoundError.Error": true, "ConfigMarshalError.Error": true,
	"New": true, "Reset": true, "SetDefault": true, "Set": true,
	"NewWithOptions": true, "SetOptions": true,
	"KeyDelimiter": true, "EnvKeyReplacer": true, "WithDecodeHook": true,
	"WithEncoderRegistry": true, "WithDecoderRegistry": true,
	"WithCodecRegistry": true,
	"GetViper": true,
	"Debug": true, "DebugTo": true,
	"Get": true, "GetString": true, "GetBool": true, "GetInt": true,
	"GetInt32": true, "GetInt64": true,
	"GetUint": true, "GetUint8": true, "GetUint16": true,
	"GetUint32": true, "GetUint64": true,
	"GetFloat64": true, "GetTime": true, "GetDuration": true,
	"GetIntSlice": true, "GetStringSlice": true,
	"GetStringMap": true, "GetStringMapString": true,
	"GetStringMapStringSlice": true, "GetSizeInBytes": true,
	"IsSet": true, "InConfig": true,
	"Sub": true,
	"SetTypeByDefaultValue": true,
	"Unmarshal": true, "UnmarshalExact": true, "UnmarshalKey": true,
	"RegisterAlias": true, "BindEnv": true, "MustBindEnv": true,
	"AutomaticEnv": true, "AllowEmptyEnv": true, "SetEnvPrefix": true,
	"GetEnvPrefix": true, "SetEnvKeyReplacer": true, "BindPFlag": true,
	"BindPFlags": true, "BindFlagValue": true, "BindFlagValues": true,
	"MergeConfigMap": true, "SetConfigType": true, "ReadConfig": true,
	"MergeConfig": true,
	"AddRemoteProvider": true, "AddSecureRemoteProvider": true,
	"ReadRemoteConfig": true, "WatchRemoteConfig": true,
	"WatchRemoteConfigOnChannel": true,
	"SetFs": true, "SetConfigFile": true, "SetConfigName": true,
	"AddConfigPath": true, "ConfigFileUsed": true, "ReadInConfig": true,
	"MergeInConfig": true,
	"OnConfigChange": true, "WatchConfig": true,
	"SetConfigPermissions": true, "WriteConfig": true, "WriteConfigAs": true,
	"SafeWriteConfig": true, "SafeWriteConfigAs": true, "WriteConfigTo": true,
	"AllKeys": true, "AllSettings": true,
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: apimanifest UPSTREAM_ROOT OUTPUT.csv")
	}
	root, output := os.Args[1], os.Args[2]
	var symbols []symbol
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "internal" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_example.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		if rel == "." {
			rel = "root"
		}
		add := func(kind, name string) {
			if ast.IsExported(strings.TrimPrefix(name, "Viper.")) {
				symbols = append(symbols, symbol{rel, kind, name, filepath.Base(path)})
			}
		}
		for _, declaration := range file.Decls {
			switch d := declaration.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add("func", d.Name.Name)
				} else if ast.IsExported(d.Name.Name) {
					add("method", receiverName(d.Recv.List[0].Type)+"."+d.Name.Name)
				}
			case *ast.GenDecl:
				for _, specification := range d.Specs {
					switch spec := specification.(type) {
					case *ast.TypeSpec:
						add("type", spec.Name.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							add(strings.ToLower(d.Tok.String()), name.Name)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Slice(symbols, func(i, j int) bool {
		a, b := symbols[i], symbols[j]
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.kind < b.kind
	})
	f, err := os.Create(output)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"package", "kind", "symbol", "source", "status", "destination_or_reason"})
	seen := map[string]bool{}
	for _, item := range symbols {
		key := item.pkg + "\x00" + item.kind + "\x00" + item.name
		if seen[key] {
			continue
		}
		seen[key] = true
		status, reason := "deferred", "outside compatibility tier 1"
		base := strings.TrimPrefix(item.name, "Viper.")
		if item.pkg == "root" && (compatible[item.name] || compatible[base]) {
			status, reason = "compatible", "goforge.dev/gpviper"
		}
		if item.pkg == "remote" {
			status, reason = "deferred-remote", "effectful provider adapters stay outside semantic core"
		}
		if err := w.Write([]string{item.pkg, item.kind, item.name, item.file, status, reason}); err != nil {
			panic(err)
		}
	}
	if err := w.Error(); err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d unique exported declarations\n", len(seen))
}

func receiverName(expression ast.Expr) string {
	switch x := expression.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	}
	return "?"
}
