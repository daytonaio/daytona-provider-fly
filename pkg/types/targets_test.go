package types

import (
	"testing"
)

func TestGetTargetConfigManifest(t *testing.T) {
	targetConfigManifest := GetTargetConfigManifest()
	if targetConfigManifest == nil {
		t.Fatalf("Expected target config manifest but got nil")
	}

	fields := [5]string{"Region", "Size", "Disk Size", "Org Slug", "Auth Token"}
	for _, field := range fields {
		if _, ok := (*targetConfigManifest)[field]; !ok {
			t.Errorf("Expected field %s in target config manifest but it was not found", field)
		}
	}
}

func TestParseTargetOptions(t *testing.T) {
	cases := []struct {
		name              string
		jsonInput         string
		setAccessTokenEnv bool
		isValid           bool
	}{
		{
			name:              "Minimal valid input",
			jsonInput:         `{"Org Slug":"org","Auth Token":"token"}`,
			setAccessTokenEnv: false,
			isValid:           true,
		},
		{
			name:              "Missing auth token, but present in env",
			jsonInput:         `{"Org Slug":"org"}`,
			setAccessTokenEnv: true,
			isValid:           true,
		},
		{
			name:              "Missing auth token",
			jsonInput:         `{"Org Slug":"org"}`,
			setAccessTokenEnv: false,
			isValid:           false,
		},
		{
			name:              "Missing org slug",
			jsonInput:         `{"Auth Token":"token"}`,
			setAccessTokenEnv: false,
			isValid:           false,
		},
		{
			name:              "Empty input",
			jsonInput:         `{}`,
			setAccessTokenEnv: false,
			isValid:           false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.setAccessTokenEnv {
				t.Setenv("FLY_ACCESS_TOKEN", "token")
			}

			_, err := ParseTargetOptions(testCase.jsonInput)
			if testCase.isValid && err != nil {
				t.Errorf("Expected valid target options but got error: %s", err)
			} else if !testCase.isValid && err == nil {
				t.Errorf("Expected error for invalid target options but got none")
			}
		})
	}
}
