package provider

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	flyutil "github.com/daytonaio/daytona-provider-fly/pkg/provider/util"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/workspace"
)

var (
	orgSlug   = os.Getenv("FLY_TEST_ORG_SLUG")
	authToken = os.Getenv("FLY_TEST_ACCESS_TOKEN")

	flyProvider   = &FlyProvider{}
	targetOptions = &types.TargetOptions{
		Region:    "lax",
		Size:      "shared-cpu-4x",
		DiskSize:  10,
		OrgSlug:   orgSlug,
		AuthToken: authToken,
	}

	workspaceReq *provider.WorkspaceRequest
)

func TestCreateWorkspace(t *testing.T) {
	_, _ = flyProvider.CreateWorkspace(workspaceReq)

	_, err := flyutil.GetMachine(workspaceReq.Workspace, targetOptions)
	if err != nil {
		t.Fatalf("Error getting machine: %s", err)
	}
}

func TestWorkspaceInfo(t *testing.T) {
	workspaceInfo, err := flyProvider.GetWorkspaceInfo(workspaceReq)
	if err != nil {
		t.Fatalf("Error getting workspace info: %s", err)
	}

	var workspaceMetadata types.WorkspaceMetadata
	err = json.Unmarshal([]byte(workspaceInfo.ProviderMetadata), &workspaceMetadata)
	if err != nil {
		t.Fatalf("Error unmarshalling workspace metadata: %s", err)
	}

	machine, err := flyutil.GetMachine(workspaceReq.Workspace, targetOptions)
	if err != nil {
		t.Fatalf("Error getting machine: %s", err)
	}

	if workspaceMetadata.MachineId != machine.ID {
		t.Fatalf("Expected machine id %s, got %s", workspaceMetadata.MachineId, machine.ID)
	}

	if workspaceMetadata.VolumeId != machine.Config.Mounts[0].Volume {
		t.Fatalf("Expected volume id %s, got %s", workspaceMetadata.VolumeId, machine.Config.Mounts[0].Volume)
	}
}

func TestDestroyWorkspace(t *testing.T) {
	_, err := flyProvider.DestroyWorkspace(workspaceReq)
	if err != nil {
		t.Fatalf("Error destroying workspace: %s", err)
	}
	time.Sleep(3 * time.Second)

	_, err = flyutil.GetMachine(workspaceReq.Workspace, targetOptions)
	if err == nil {
		t.Fatalf("Error destroyed workspace still exists")
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

	workspaceReq = &provider.WorkspaceRequest{
		TargetOptions: string(opts),
		Workspace: &workspace.Workspace{
			Id:   "123",
			Name: "workspace",
		},
	}

}
