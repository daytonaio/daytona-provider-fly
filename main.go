package main

import (
	"os"

	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/provider/manager"
	"github.com/hashicorp/go-hclog"
	hc_plugin "github.com/hashicorp/go-plugin"

	p "github.com/daytonaio/daytona-provider-sample/pkg/provider"
)

func main() {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})
	hc_plugin.Serve(&hc_plugin.ServeConfig{
		HandshakeConfig: manager.ProviderHandshakeConfig,
		Plugins: map[string]hc_plugin.Plugin{
			"provider-sample": &provider.ProviderPlugin{Impl: &p.SampleProvider{}},
		},
		Logger: logger,
	})
}
