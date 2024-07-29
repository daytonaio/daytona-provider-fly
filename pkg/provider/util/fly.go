package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/daytonaio/daytona-provider-fly/internal"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/workspace"
	log "github.com/sirupsen/logrus"
	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
)

// CreateWorkspace creates a new fly.io app for the provided workspace.
func CreateWorkspace(workspace *workspace.Workspace, opts *types.TargetOptions, initScript string) (*fly.Machine, error) {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return nil, err
	}

	err = flapsClient.CreateApp(context.Background(), appName, opts.OrgSlug)
	if err != nil {
		return nil, err
	}

	err = flapsClient.WaitForApp(context.Background(), appName)
	if err != nil {
		return nil, err
	}

	machine, err := createMachine(workspace, opts, initScript)
	if err != nil {
		return nil, err
	}

	err = flapsClient.Wait(context.Background(), machine, fly.MachineStateStarted, time.Minute*5)
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// StartWorkspace starts the machine for the provided workspace.
func StartWorkspace(workspace *workspace.Workspace, opts *types.TargetOptions) error {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return err
	}

	err = flapsClient.WaitForApp(context.Background(), appName)
	if err != nil {
		return fmt.Errorf("there was an issue waiting for the app: %w", err)
	}

	machineName := getResourceName(workspace.Id)
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

// StopWorkspace stops the machine for the provided workspace.
func StopWorkspace(workspace *workspace.Workspace, opts *types.TargetOptions) error {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return err
	}

	machineName := getResourceName(workspace.Id)
	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return err
	}

	return flapsClient.Stop(context.Background(), fly.StopMachineInput{ID: machine.ID}, "")
}

// DeleteWorkspace deletes the app associated with the provided workspace.
func DeleteWorkspace(workspace *workspace.Workspace, opts *types.TargetOptions) error {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return err
	}

	// TODO: use delete method from flaps client when implemented in sdk
	path := fmt.Sprintf("/apps/%s", appName)
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

// createMachine creates a new machine for the provided workspace.
func createMachine(workspace *workspace.Workspace, opts *types.TargetOptions, initScript string) (*fly.Machine, error) {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return nil, err
	}

	volume, err := flapsClient.CreateVolume(context.Background(), fly.CreateVolumeRequest{
		Name:   getVolumeName(workspace.Id),
		SizeGb: &opts.DiskSize,
		Region: opts.Region,
	})
	if err != nil {
		return nil, err
	}

	script := fmt.Sprintf(`#!/bin/sh
# Start Docker daemon
dockerd-entrypoint.sh &

# Wait for Docker to be ready
while ! docker info > /dev/null 2>&1; do
    echo "Waiting for Docker to start..."
    sleep 1
done

# Create daytona user and add to docker group
adduser -D -G docker daytona

# Download and install daytona agent
%s

# Switch to daytona user and run Daytona agent
su daytona -c "daytona agent --host"
`, initScript)

	return flapsClient.Launch(context.Background(), fly.LaunchMachineInput{
		Name: getResourceName(workspace.Id),
		Config: &fly.MachineConfig{
			VMSize: opts.Size,
			Image:  "docker:dind",
			Mounts: []fly.MachineMount{
				{
					Name:   volume.Name,
					Volume: volume.ID,
					Path:   "/var/lib/docker",
					SizeGb: opts.DiskSize,
				},
			},
			Init: fly.MachineInit{
				Entrypoint: []string{"/bin/sh", "-c", script},
			},
			Env: workspace.EnvVars,
		},
		Region: opts.Region,
	})
}

// GetMachine returns the machine for the provided workspace.
func GetMachine(workspace *workspace.Workspace, opts *types.TargetOptions) (*fly.Machine, error) {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, opts.AuthToken)
	if err != nil {
		return nil, err
	}

	machineName := getResourceName(workspace.Id)
	return findMachine(flapsClient, machineName)
}

// GetWorkspaceLogs fetches app logs for a specified workspace machine and writes the fetched log entries to the logger.
func GetWorkspaceLogs(workspace *workspace.Workspace, opts *types.TargetOptions, machineId string, logger io.Writer) error {
	appName := getResourceName(workspace.Id)

	fly.SetBaseURL("https://api.fly.io")
	client := fly.NewClientFromOptions(fly.ClientOptions{
		Tokens:  tokens.Parse(opts.AuthToken),
		Name:    appName,
		Version: internal.Version,
	})

	outLog := make(chan string)
	go func() {
		for entry := range outLog {
			logger.Write([]byte(entry))
		}
	}()

	return pollLogs(outLog, client, appName, opts.Region, machineId)
}

// createFlapsClient creates a new flaps client.
func createFlapsClient(appName string, accessToken string) (*flaps.Client, error) {
	return flaps.NewWithOptions(context.Background(), flaps.NewClientOpts{
		AppName: appName,
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

// getResourceName generates a machine name for the provided workspace.
func getResourceName(identifier string) string {
	return fmt.Sprintf("daytona-%s", identifier)
}

// getVolumeName generates a volume name for the provided workspace.
func getVolumeName(name string) string {
	name = fmt.Sprintf("daytona_%s", name)
	regex := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	formatted := regex.ReplaceAllString(name, "")

	if len(formatted) > 30 {
		return formatted[:30]
	}

	return formatted
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
