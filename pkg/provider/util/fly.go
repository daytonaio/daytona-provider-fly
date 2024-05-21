package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/daytonaio/daytona-provider-fly/internal"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/workspace"
	log "github.com/sirupsen/logrus"
	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
)

// CreateMachine creates a new machine for the provided workspace.
func CreateMachine(project *workspace.Project, opts *types.TargetOptions, initScript string) (*fly.Machine, error) {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return nil, err
	}

	err = flapsClient.CreateApp(context.Background(), machineName, opts.OrgSlug)
	if err != nil {
		return nil, err
	}

	err = flapsClient.WaitForApp(context.Background(), machineName)
	if err != nil {
		return nil, err
	}

	volume, err := flapsClient.CreateVolume(context.Background(), fly.CreateVolumeRequest{
		Name:   getVolumeName(project.WorkspaceId),
		SizeGb: &opts.DiskSize,
		Region: opts.Region,
	})
	if err != nil {
		return nil, err
	}

	envVars := map[string]string{}
	for key, value := range project.EnvVars {
		envVars[key] = value
	}

	return flapsClient.Launch(context.Background(), fly.LaunchMachineInput{
		Name: machineName,
		Config: &fly.MachineConfig{
			VMSize: opts.Size,
			Image:  project.Image,
			Mounts: []fly.MachineMount{
				{
					Name:   volume.Name,
					Volume: volume.ID,
					Path:   fmt.Sprintf("/home/%s", project.User),
					SizeGb: opts.DiskSize,
				},
			},
			Env: envVars,
			Init: fly.MachineInit{
				Entrypoint: []string{"bash", "-c", initScript},
			},
		},
		Region: opts.Region,
	})
}

// IsMachineReady checks if the machine for the given workspace is ready to use.
// It returns true if the machine is in a known state (created, started, or stopped),
// otherwise it returns false.
func IsMachineReady(project *workspace.Project, opts *types.TargetOptions) bool {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return false
	}

	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return false
	}

	knownStatus := []string{fly.MachineStateCreated, fly.MachineStateStarted, fly.MachineStateStopped}
	return slices.Contains(knownStatus, machine.State)
}

// StartMachine starts the machine for the provided workspace.
func StartMachine(project *workspace.Project, opts *types.TargetOptions) error {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return err
	}

	err = flapsClient.WaitForApp(context.Background(), machineName)
	if err != nil {
		return fmt.Errorf("there was an issue waiting for the app: %w", err)
	}

	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return err
	}

	// Start the machine if it is stopped
	if machine.State == fly.MachineStateStopped {
		_, err = flapsClient.Start(context.Background(), machine.ID, "")
		if err != nil {
			return err
		}
	}

	return nil
}

// StopMachine stops the machine for the provided workspace.
func StopMachine(project *workspace.Project, opts *types.TargetOptions) error {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return err
	}

	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return err
	}

	return flapsClient.Stop(context.Background(), fly.StopMachineInput{ID: machine.ID}, "")
}

// DeleteMachine deletes the machine for the provided workspace.
func DeleteMachine(project *workspace.Project, opts *types.TargetOptions) error {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return err
	}

	// TODO: use delete method from flaps client when implemented in sdk
	path := fmt.Sprintf("/apps/%s", machineName)
	req, err := flapsClient.NewRequest(context.Background(), http.MethodDelete, path, nil, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected error while deleting the app status code: %d", resp.StatusCode)
	}

	return nil
}

// GetMachine returns the machine for the provided workspace.
func GetMachine(project *workspace.Project, opts *types.TargetOptions) (*fly.Machine, error) {
	machineName := getMachineName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(machineName, *opts.AuthToken)
	if err != nil {
		return nil, err
	}

	return findMachine(flapsClient, machineName)
}

// GetMachineLogs fetches app logs for a specified machine and writes the fetched log entries to the logger.
func GetMachineLogs(project *workspace.Project, opts *types.TargetOptions, machineId string, logger io.Writer) error {
	machineName := getMachineName(project.WorkspaceId)

	fly.SetBaseURL("https://api.fly.io")
	client := fly.NewClientFromOptions(fly.ClientOptions{
		Tokens:  tokens.Parse(*opts.AuthToken),
		Name:    machineName,
		Version: internal.Version,
	})

	outLog := make(chan string)
	go func() {
		for entry := range outLog {
			logger.Write([]byte(entry))
		}
	}()

	return pollLogs(outLog, client, machineName, opts.Region, machineId)
}

// createFlapsClient creates a new flaps client.
func createFlapsClient(machineName string, accessToken string) (*flaps.Client, error) {
	return flaps.NewWithOptions(context.Background(), flaps.NewClientOpts{
		AppName: machineName,
		Tokens:  tokens.Parse(accessToken),
		Logger:  log.New(),
	})
}

// findMachine finds the machine with the provided name.
func findMachine(flapsClient *flaps.Client, machineName string) (*fly.Machine, error) {
	machineList, err := flapsClient.List(context.Background(), "")
	if err != nil {
		return nil, err
	}

	for _, m := range machineList {
		if m.Name == machineName {
			return m, nil
		}
	}

	return nil, fmt.Errorf("machine %s not found", machineName)
}

// getMachineName generates a machine name for the provided workspace.
func getMachineName(workspaceId string) string {
	return fmt.Sprintf("daytona-%s", workspaceId)
}

// getVolumeName generates a volume name for the provided workspace.
func getVolumeName(workspaceId string) string {
	name := fmt.Sprintf("daytona_%s", workspaceId)
	if len(name) > 30 {
		return name[:30]
	}
	return name
}

// pollLogs fetches app logs for a specified app name, region, and machine ID using the provided fly.Client.
// It sends the fetched log entries to the out channel.
// It continues fetching logs indefinitely until an error occurs.
func pollLogs(out chan<- string, client *fly.Client, appName, region, machineId string) error {
	var (
		prevToken string
		nextToken string
	)

	for {
		entries, token, err := client.GetAppLogs(context.Background(), appName, nextToken, region, machineId)
		if err != nil {
			return err
		}

		// Adds a delay in fetching logs when current log entries have been fully fetched.
		// This is done to reduce pressure on the server and give time for new logs to accumulate.
		if token == prevToken {
			time.Sleep(10 * time.Second)
		}

		prevToken = token
		if token != "" {
			nextToken = token
		}

		for _, entry := range entries {
			logMessage := fmt.Sprintf("%s app[%s] %s [%s] %s \n",
				entry.Timestamp,
				entry.Instance,
				entry.Region,
				entry.Level,
				entry.Message,
			)
			out <- logMessage
		}
	}
}
