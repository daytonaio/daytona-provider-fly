package util

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/daytonaio/daytona-provider-fly/internal"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/containerregistry"
	"github.com/daytonaio/daytona/pkg/docker"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
)

const (
	registryServer = "registry.fly.io"
	registryUser   = "x"
)

// CreateApp creates a new app for the provided workspace.
func CreateApp(workspace *workspace.Workspace, opts *types.TargetOptions) error {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return err
	}

	err = flapsClient.CreateApp(context.Background(), appName, opts.OrgSlug)
	if err != nil {
		return err
	}

	return flapsClient.WaitForApp(context.Background(), appName)
}

// DeleteApp deletes the app associated with the provided workspace.
func DeleteApp(workspace *workspace.Workspace, opts *types.TargetOptions) error {
	appName := getResourceName(workspace.Id)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
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

// CreateMachine creates a new machine for the provided workspace.
func CreateMachine(project *workspace.Project, opts *types.TargetOptions, containerRegistry *containerregistry.ContainerRegistry, logWriter io.Writer, initScript string) (*fly.Machine, error) {
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return nil, err
	}

	image, err := deployImage(project, containerRegistry, logWriter, *opts.AuthToken)
	if err != nil {
		return nil, err
	}

	volume, err := flapsClient.CreateVolume(context.Background(), fly.CreateVolumeRequest{
		Name:   getVolumeName(project.Name),
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
		Name: getResourceName(project.Name),
		Config: &fly.MachineConfig{
			VMSize: opts.Size,
			Image:  image,
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
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return false
	}

	machineName := getResourceName(project.Name)
	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return false
	}

	knownStatus := []string{fly.MachineStateCreated, fly.MachineStateStarted, fly.MachineStateStopped}
	return slices.Contains(knownStatus, machine.State)
}

// StartMachine starts the machine for the provided workspace.
func StartMachine(project *workspace.Project, opts *types.TargetOptions) error {
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return err
	}

	err = flapsClient.WaitForApp(context.Background(), appName)
	if err != nil {
		return fmt.Errorf("there was an issue waiting for the app: %w", err)
	}

	machineName := getResourceName(project.Name)
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
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return err
	}

	machineName := getResourceName(project.Name)
	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return err
	}

	return flapsClient.Stop(context.Background(), fly.StopMachineInput{ID: machine.ID}, "")
}

// DeleteMachine deletes the machine for the provided workspace.
func DeleteMachine(project *workspace.Project, opts *types.TargetOptions) error {
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return err
	}

	machineName := getResourceName(project.Name)
	machine, err := findMachine(flapsClient, machineName)
	if err != nil {
		return err
	}

	return flapsClient.Destroy(context.Background(), fly.RemoveMachineInput{
		ID:   machine.ID,
		Kill: true,
	}, "")
}

// GetMachine returns the machine for the provided workspace.
func GetMachine(project *workspace.Project, opts *types.TargetOptions) (*fly.Machine, error) {
	appName := getResourceName(project.WorkspaceId)
	flapsClient, err := createFlapsClient(appName, *opts.AuthToken)
	if err != nil {
		return nil, err
	}

	machineName := getResourceName(project.Name)
	return findMachine(flapsClient, machineName)
}

// GetMachineLogs fetches app logs for a specified machine and writes the fetched log entries to the logger.
func GetMachineLogs(project *workspace.Project, opts *types.TargetOptions, machineId string, logger io.Writer) error {
	appName := getResourceName(project.WorkspaceId)

	fly.SetBaseURL("https://api.fly.io")
	client := fly.NewClientFromOptions(fly.ClientOptions{
		Tokens:  tokens.Parse(*opts.AuthToken),
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

// deployImage pulls, tags, and pushes the image from private registry to the Fly.io registry.
func deployImage(project *workspace.Project, containerRegistry *containerregistry.ContainerRegistry, logWriter io.Writer, authToken string) (string, error) {
	if containerRegistry == nil {
		return project.Image, nil
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}

	if isPublicImage(dockerClient, project.Image) {
		return project.Image, nil
	}
	logWriter.Write([]byte(fmt.Sprintf("Using private container registry: %s\n", containerRegistry.Server)))

	daytonaDockerClient := docker.NewDockerClient(docker.DockerClientConfig{ApiClient: dockerClient})
	machineName := getResourceName(project.Name)

	logWriter.Write([]byte(fmt.Sprintf("Pulling private containter image %s\n", machineName)))
	if err := daytonaDockerClient.PullImage(project.Image, containerRegistry, logWriter); err != nil {
		logWriter.Write([]byte(fmt.Sprintf("Error pulling private containter image: %s\n", err)))
		return "", err
	}

	imageTag := generateImageTag(project.Image)
	flyImageName := fmt.Sprintf("%s/%s:%s", registryServer, getResourceName(project.WorkspaceId), imageTag)
	if err := dockerClient.ImageTag(context.Background(), project.Image, flyImageName); err != nil {
		return "", err
	}

	logWriter.Write([]byte(fmt.Sprintf("Pushing %s to %s make it available to fly instances\n", flyImageName, registryServer)))
	flyRegistry := &containerregistry.ContainerRegistry{
		Server:   registryServer,
		Username: registryUser,
		Password: authToken,
	}
	if err := daytonaDockerClient.PushImage(flyImageName, flyRegistry, logWriter); err != nil {
		logWriter.Write([]byte(fmt.Sprintf("Failed to push image: %s\n", err)))
		return "", err
	}

	return flyImageName, nil
}

// isPublicImage checks if the given image is a public image.
func isPublicImage(dockerClient *client.Client, imageName string) bool {
	_, err := dockerClient.DistributionInspect(context.Background(), imageName, "")
	return err == nil
}

// generateImageTag generates a unique tag for the image based on its hash.
func generateImageTag(image string) string {
	hash := sha256.Sum256([]byte(image))
	return hex.EncodeToString(hash[:])
}
