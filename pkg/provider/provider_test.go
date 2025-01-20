package provider

import (
	"os"
	"testing"
	"time"

	flyutil "github.com/daytonaio/daytona-provider-fly/pkg/provider/util"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/models"
	"github.com/daytonaio/daytona/pkg/provider"
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

	targetReq *provider.TargetRequest
)

func TestCreatetarget(t *testing.T) {
	_, _ = flyProvider.CreateTarget(targetReq)

	_, err := flyutil.GetMachine(targetReq.Target, targetOptions)
	if err != nil {
		t.Fatalf("Error getting machine: %s", err)
	}
}

func TestDestroytarget(t *testing.T) {
	_, err := flyProvider.DestroyTarget(targetReq)
	if err != nil {
		t.Fatalf("Error destroying target: %s", err)
	}
	time.Sleep(3 * time.Second)

	_, err = flyutil.GetMachine(targetReq.Target, targetOptions)
	if err == nil {
		t.Fatalf("Error destroyed target still exists")
	}
}

func init() {
	_, err := flyProvider.Initialize(provider.InitializeProviderRequest{
		BasePath:           "/tmp/targets",
		DaytonaDownloadUrl: "https://download.daytona.io/daytona/install.sh",
		DaytonaVersion:     "latest",
		ServerUrl:          "",
		ApiUrl:             "",
		TargetLogsDir:      "/tmp/logs",
	})
	if err != nil {
		panic(err)
	}

	targetReq = &provider.TargetRequest{
		Target: &models.Target{
			Id:   "123",
			Name: "target",
		},
	}

}
