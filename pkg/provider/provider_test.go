package provider

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	flyutil "github.com/daytonaio/daytona-provider-fly/pkg/provider/util"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/gitprovider"
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/workspace"
)

var (
	orgSlug       = os.Getenv("FLY_TEST_ORG_SLUG")
	authToken     = os.Getenv("FLY_TEST_ACCESS_TOKEN")
	optionsString string

	flyProvider   = &FlyProvider{}
	targetOptions = &types.TargetOptions{
		Region:    "lax",
		Size:      "shared-cpu-4x",
		DiskSize:  10,
		OrgSlug:   orgSlug,
		AuthToken: &authToken,
	}

	project1 = &workspace.Project{
		Name: "test",
		Repository: &gitprovider.GitRepository{
			Id:   "123",
			Url:  "https://github.com/daytonaio/daytona",
			Name: "daytona",
		},
		Image:       "daytonaio/workspace-project:latest",
		WorkspaceId: "123",
	}
)

func TestCreateProject(t *testing.T) {
	projectReq := &provider.ProjectRequest{
		TargetOptions: optionsString,
		Project:       project1,
	}

	_, err := flyProvider.CreateProject(projectReq)
	if err != nil {
		t.Fatalf("Error creating project: %s", err)
	}

	_, err = flyutil.GetMachine(project1, targetOptions)
	if err != nil {
		t.Fatalf("Error getting machine: %s", err)
	}
}

func TestProjectInfo(t *testing.T) {
	projectReq := &provider.ProjectRequest{
		TargetOptions: optionsString,
		Project:       project1,
	}

	projectInfo, err := flyProvider.GetProjectInfo(projectReq)
	if err != nil {
		t.Fatalf("Error getting workspace info: %s", err)
	}

	var projectMetadata types.ProjectMetadata
	err = json.Unmarshal([]byte(projectInfo.ProviderMetadata), &projectMetadata)
	if err != nil {
		t.Fatalf("Error unmarshalling project metadata: %s", err)
	}

	machine, err := flyutil.GetMachine(project1, targetOptions)
	if err != nil {
		t.Fatalf("Error getting machine: %s", err)
	}

	if projectMetadata.MachineId != machine.ID {
		t.Fatalf("Expected machine id %s, got %s", projectMetadata.MachineId, machine.ID)
	}

	if projectMetadata.VolumeId != machine.Config.Mounts[0].Volume {
		t.Fatalf("Expected volume id %s, got %s", projectMetadata.VolumeId, machine.Config.Mounts[0].Volume)
	}
}

func TestDestroyProject(t *testing.T) {
	projectReq := &provider.ProjectRequest{
		TargetOptions: optionsString,
		Project:       project1,
	}

	_, err := flyProvider.DestroyProject(projectReq)
	if err != nil {
		t.Fatalf("Error destroying project: %s", err)
	}
	time.Sleep(3 * time.Second)

	_, err = flyutil.GetMachine(project1, targetOptions)
	if err == nil {
		t.Fatalf("Error destroyed project still exists")
	}
}

func init() {
	_, err := flyProvider.Initialize(provider.InitializeProviderRequest{
		BasePath:           "/tmp/workspaces",
		DaytonaDownloadUrl: "https://download.daytona.io/daytona/install.sh",
		DaytonaVersion:     "latest",
		ServerUrl:          "",
		ApiUrl:             "",
		LogsDir:            "/tmp/logs",
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
