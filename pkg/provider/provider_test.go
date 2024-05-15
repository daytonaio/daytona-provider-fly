package provider_test

import (
	"encoding/json"
	"testing"

	"github.com/daytonaio/daytona/pkg/gitprovider"
	daytona_provider "github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/workspace"

	"github.com/daytonaio/daytona-provider-sample/pkg/provider"
	provider_types "github.com/daytonaio/daytona-provider-sample/pkg/types"
)

var sampleProvider = &provider.SampleProvider{}
var targetOptions = &provider_types.TargetOptions{
	RequiredString: "default-required-string",
}
var optionsString string

var project1 = &workspace.Project{
	Name: "test",
	Repository: &gitprovider.GitRepository{
		Id:   "123",
		Url:  "https://github.com/daytonaio/daytona",
		Name: "daytona",
	},
	WorkspaceId: "123",
	EnvVars: map[string]string{
		"DAYTONA_WS_ID":                     "123",
		"DAYTONA_WS_PROJECT_NAME":           "test",
		"DAYTONA_WS_PROJECT_REPOSITORY_URL": "https://github.com/daytonaio/daytona",
		"DAYTONA_SERVER_API_KEY":            "api-key-test",
		"DAYTONA_SERVER_VERSION":            "latest",
		"DAYTONA_SERVER_URL":                "http://localhost:3001",
		"DAYTONA_SERVER_API_URL":            "http://localhost:3000",
	},
}

var workspace1 = &workspace.Workspace{
	Id:     "123",
	Name:   "test",
	Target: "local",
	Projects: []*workspace.Project{
		project1,
	},
}

func TestCreateWorkspace(t *testing.T) {
	wsReq := &daytona_provider.WorkspaceRequest{
		TargetOptions: optionsString,
		Workspace:     workspace1,
	}

	_, err := sampleProvider.CreateWorkspace(wsReq)
	if err != nil {
		t.Errorf("Error creating workspace: %s", err)
	}
}

func TestGetWorkspaceInfo(t *testing.T) {
	wsReq := &daytona_provider.WorkspaceRequest{
		TargetOptions: optionsString,
		Workspace:     workspace1,
	}

	workspaceInfo, err := sampleProvider.GetWorkspaceInfo(wsReq)
	if err != nil || workspaceInfo == nil {
		t.Errorf("Error getting workspace info: %s", err)
	}

	var workspaceMetadata provider_types.WorkspaceMetadata
	err = json.Unmarshal([]byte(workspaceInfo.ProviderMetadata), &workspaceMetadata)
	if err != nil {
		t.Errorf("Error unmarshalling workspace metadata: %s", err)
	}

	if workspaceMetadata.Property != wsReq.Workspace.Id {
		t.Errorf("Expected network id %s, got %s", wsReq.Workspace.Id, workspaceMetadata.Property)
	}
}

func TestDestroyWorkspace(t *testing.T) {
	wsReq := &daytona_provider.WorkspaceRequest{
		TargetOptions: optionsString,
		Workspace:     workspace1,
	}

	_, err := sampleProvider.DestroyWorkspace(wsReq)
	if err != nil {
		t.Errorf("Error deleting workspace: %s", err)
	}
}

func TestCreateProject(t *testing.T) {
	TestCreateWorkspace(t)

	projectReq := &daytona_provider.ProjectRequest{
		TargetOptions: optionsString,
		Project:       project1,
	}

	_, err := sampleProvider.CreateProject(projectReq)
	if err != nil {
		t.Errorf("Error creating project: %s", err)
	}
}

func TestDestroyProject(t *testing.T) {
	projectReq := &daytona_provider.ProjectRequest{
		TargetOptions: optionsString,
		Project:       project1,
	}

	_, err := sampleProvider.DestroyProject(projectReq)
	if err != nil {
		t.Errorf("Error deleting project: %s", err)
	}

	TestDestroyWorkspace(t)
}

func init() {
	_, err := sampleProvider.Initialize(daytona_provider.InitializeProviderRequest{
		BasePath:          "/tmp/workspaces",
		ServerDownloadUrl: "https://download.daytona.io/daytona/install.sh",
		ServerVersion:     "latest",
		ServerUrl:         "",
		ServerApiUrl:      "",
		LogsDir:           "/tmp/logs",
	})
	if err != nil {
		panic(err)
	}

	opts, err := json.Marshal(targetOptions)
	if err != nil {
		panic(err)
	}

	optionsString = string(opts)
}
