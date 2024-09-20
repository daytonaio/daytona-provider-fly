package types

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/daytonaio/daytona/pkg/provider"
)

type TargetOptions struct {
	Region    string `json:"Region"`
	Size      string `json:"Size"`
	DiskSize  int    `json:"Disk Size"`
	OrgSlug   string `json:"Org Slug"`
	AuthToken string `json:"Auth Token,omitempty"`
}

func GetTargetManifest() *provider.ProviderTargetManifest {
	return &provider.ProviderTargetManifest{
		"Region": provider.ProviderTargetProperty{
			Type:        provider.ProviderTargetPropertyTypeString,
			Description: "The region where the fly machine resides. If not specified, near region will be used.",
			Suggestions: regions,
		},
		"Size": provider.ProviderTargetProperty{
			Type:         provider.ProviderTargetPropertyTypeString,
			DefaultValue: "shared-cpu-4x",
			Description: "The size of the fly machine. Default is shared-cpu-4x. List of available sizes " +
				"https://fly.io/docs/about/pricing/#started-fly-machines",
		},
		"Disk Size": provider.ProviderTargetProperty{
			Type:         provider.ProviderTargetPropertyTypeInt,
			DefaultValue: "10",
			Description:  "The size of the disk in GB.",
		},
		"Org Slug": provider.ProviderTargetProperty{
			Type:        provider.ProviderTargetPropertyTypeString,
			Description: "The organization name to create the fly machine in.",
		},
		"Auth Token": provider.ProviderTargetProperty{
			Type:        provider.ProviderTargetPropertyTypeString,
			InputMasked: true,
			Description: "If empty, token will be fetched from the FLY_ACCESS_TOKEN environment variable.",
		},
	}
}

// ParseTargetOptions parses the target options from the JSON string.
func ParseTargetOptions(optionsJson string) (*TargetOptions, error) {
	var targetOptions TargetOptions
	err := json.Unmarshal([]byte(optionsJson), &targetOptions)
	if err != nil {
		return nil, err
	}

	if targetOptions.AuthToken == "" {
		// Fetch token from environment variable
		token, ok := os.LookupEnv("FLY_ACCESS_TOKEN")
		if ok {
			targetOptions.AuthToken = token
		}
	}

	if targetOptions.AuthToken == "" {
		return nil, fmt.Errorf("auth token not set in env/target options")
	}

	if targetOptions.OrgSlug == "" {
		return nil, fmt.Errorf("org slug not set in target options")
	}

	return &targetOptions, nil
}
