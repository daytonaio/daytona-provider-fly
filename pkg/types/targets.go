package types

import (
	"encoding/json"

	"github.com/daytonaio/daytona/pkg/provider"
)

type TargetOptions struct {
	RequiredString string  `json:"Required String"`
	OptionalString *string `json:"Optional String,omitempty"`
	OptionalInt    *int    `json:"Optional Int,omitempty"`
	FilePath       *string `json:"File Path"`
}

func GetTargetManifest() *provider.ProviderTargetManifest {
	return &provider.ProviderTargetManifest{
		"Required String": provider.ProviderTargetProperty{
			Type:         provider.ProviderTargetPropertyTypeString,
			DefaultValue: "default-required-string",
		},
		"Optional String": provider.ProviderTargetProperty{
			Type:        provider.ProviderTargetPropertyTypeString,
			InputMasked: true,
		},
		"Optional Int": provider.ProviderTargetProperty{
			Type: provider.ProviderTargetPropertyTypeInt,
		},
		"File Path": provider.ProviderTargetProperty{
			Type:              provider.ProviderTargetPropertyTypeFilePath,
			DefaultValue:      "~/.ssh",
			DisabledPredicate: "^default-target$",
		},
	}
}

func ParseTargetOptions(optionsJson string) (*TargetOptions, error) {
	var targetOptions TargetOptions
	err := json.Unmarshal([]byte(optionsJson), &targetOptions)
	if err != nil {
		return nil, err
	}

	return &targetOptions, nil
}
