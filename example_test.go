package viper_test

import (
	"fmt"

	"goforge.dev/gpviper"
)

func ExampleViper_Snapshot() {
	registry := viper.New()
	registry.SetDefault("http.port", 8080)
	registry.Set("http.port", 9090)

	// Publish once after loading. Request paths can now read without locks or
	// consulting environment variables and still inspect provenance.
	configuration := registry.Snapshot()
	port, _ := configuration.Get("http.port")
	fmt.Println(port.Value, port.Source == viper.SourceOverride)
	// Output: 9090 true
}
