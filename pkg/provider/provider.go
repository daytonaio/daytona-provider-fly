package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/daytonaio/daytona-provider-fly/internal"
	logwriters "github.com/daytonaio/daytona-provider-fly/internal/log"
	flyutil "github.com/daytonaio/daytona-provider-fly/pkg/provider/util"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/agent/ssh/config"
	"github.com/daytonaio/daytona/pkg/docker"
	"github.com/daytonaio/daytona/pkg/logs"
	"github.com/daytonaio/daytona/pkg/models"
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/ssh"
	"github.com/daytonaio/daytona/pkg/tailscale"
	"github.com/superfly/fly-go"
	"tailscale.com/tsnet"
)

type FlyProvider struct {
	BasePath           *string
	DaytonaDownloadUrl *string
	DaytonaVersion     *string
	ServerUrl          *string
	NetworkKey         *string
	ApiUrl             *string
	ApiKey             *string
	ApiPort            *uint32
	ServerPort         *uint32
	TargetLogsDir      *string
	WorkspaceLogsDir   *string
	tsnetConn          *tsnet.Server
}

// Initialize initializes the provider with the given configuration.
func (p *FlyProvider) Initialize(req provider.InitializeProviderRequest) (*util.Empty, error) {
	p.BasePath = &req.BasePath
	p.DaytonaDownloadUrl = &req.DaytonaDownloadUrl
	p.DaytonaVersion = &req.DaytonaVersion
	p.ServerUrl = &req.ServerUrl
	p.NetworkKey = &req.NetworkKey
	p.ApiUrl = &req.ApiUrl
	p.ApiKey = req.ApiKey
	p.ApiPort = &req.ApiPort
	p.ServerPort = &req.ServerPort
	p.TargetLogsDir = &req.TargetLogsDir
	p.WorkspaceLogsDir = &req.WorkspaceLogsDir

	return new(util.Empty), nil
}

// GetInfo returns the provider information.
func (p *FlyProvider) GetInfo() (models.ProviderInfo, error) {
	label := "Fly.io"

	return models.ProviderInfo{
		Label:                &label,
		Name:                 "fly-provider",
		Version:              internal.Version,
		TargetConfigManifest: *types.GetTargetConfigManifest(),
	}, nil
}

func (p *FlyProvider) GetPresetTargetConfigs() (*[]provider.TargetConfig, error) {
	return new([]provider.TargetConfig), nil
}

func (p *FlyProvider) CreateTarget(targetReq *provider.TargetRequest) (*util.Empty, error) {
	if p.DaytonaDownloadUrl == nil {
		return nil, errors.New("DaytonaDownloadUrl not set. Did you forget to call Initialize")
	}
	logWriter, cleanupFunc := p.getTargetLogWriter(targetReq.Target.Id, targetReq.Target.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(targetReq.Target.TargetConfig.Options)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	initScript := fmt.Sprintf(`apk add --no-cache curl bash && \ 
	curl -sfL -H "Authorization: Bearer %s" %s | bash`,
		targetReq.Target.ApiKey,
		*p.DaytonaDownloadUrl,
	)

	machine, err := flyutil.CreateTarget(targetReq.Target, targetOptions, initScript)
	if err != nil {
		logWriter.Write([]byte("Failed to create target: " + err.Error() + "\n"))
		return nil, err
	}

	go func() {
		if err := flyutil.GetTargetLogs(targetReq.Target, targetOptions, machine.ID, logWriter); err != nil {
			logWriter.Write([]byte(err.Error()))
			defer cleanupFunc()
		}
	}()

	err = p.waitForDial(targetReq.Target.Id, 5*time.Minute)
	if err != nil {
		logWriter.Write([]byte("Failed to dial: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("target agent started.\n"))

	client, err := p.getDockerClient(targetReq.Target.Id)
	if err != nil {
		logWriter.Write([]byte("Failed to get client: " + err.Error() + "\n"))
		return nil, err
	}

	targetDir := p.getTargetDir(targetReq.Target.Id)
	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: targetReq.Target.Id,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), client.CreateTarget(targetReq.Target, targetDir, logWriter, sshClient)
}

func (p *FlyProvider) StartTarget(targetReq *provider.TargetRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getTargetLogWriter(targetReq.Target.Id, targetReq.Target.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(targetReq.Target.TargetConfig.Options)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.StartTarget(targetReq.Target, targetOptions)
}

func (p *FlyProvider) StopTarget(targetReq *provider.TargetRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getTargetLogWriter(targetReq.Target.Id, targetReq.Target.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(targetReq.Target.TargetConfig.Options)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.StopTarget(targetReq.Target, targetOptions)
}

func (p *FlyProvider) DestroyTarget(targetReq *provider.TargetRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getTargetLogWriter(targetReq.Target.Id, targetReq.Target.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(targetReq.Target.TargetConfig.Options)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.DeleteTarget(targetReq.Target, targetOptions)
}

func (p *FlyProvider) GetTargetProviderMetadata(targetReq *provider.TargetRequest) (string, error) {
	logWriter, cleanupFunc := p.getTargetLogWriter(targetReq.Target.Id, targetReq.Target.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(targetReq.Target.TargetConfig.Options)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return "", err
	}

	machine, err := flyutil.GetMachine(targetReq.Target, targetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to get machine: " + err.Error() + "\n"))
		return "", err

	}

	metadata := types.TargetMetadata{
		MachineId: machine.ID,
		VolumeId:  machine.Config.Mounts[0].Volume,
		IsRunning: machine.State == fly.MachineStateStarted,
		Created:   machine.CreatedAt,
	}

	jsonMetadata, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}

	return string(jsonMetadata), nil
}

func (p *FlyProvider) CreateWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id, workspaceReq.Workspace.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(workspaceReq.Workspace.TargetId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: workspaceReq.Workspace.TargetId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.CreateWorkspace(&docker.CreateWorkspaceOptions{
		Workspace:           workspaceReq.Workspace,
		WorkspaceDir:        p.getWorkspaceDir(workspaceReq),
		ContainerRegistries: workspaceReq.ContainerRegistries,
		BuilderImage:        workspaceReq.BuilderImage,
		LogWriter:           logWriter,
		Gpc:                 workspaceReq.GitProviderConfig,
		SshClient:           sshClient,
	})
}

func (p *FlyProvider) StartWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	if p.DaytonaDownloadUrl == nil {
		return nil, errors.New("DaytonaDownloadUrl not set. Did you forget to call Initialize")
	}
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id, workspaceReq.Workspace.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(workspaceReq.Workspace.TargetId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: workspaceReq.Workspace.TargetId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.StartWorkspace(&docker.CreateWorkspaceOptions{
		Workspace:           workspaceReq.Workspace,
		WorkspaceDir:        p.getWorkspaceDir(workspaceReq),
		ContainerRegistries: workspaceReq.ContainerRegistries,
		BuilderImage:        workspaceReq.BuilderImage,
		LogWriter:           logWriter,
		Gpc:                 workspaceReq.GitProviderConfig,
		SshClient:           sshClient,
	}, *p.DaytonaDownloadUrl)
}

func (p *FlyProvider) StopWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id, workspaceReq.Workspace.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(workspaceReq.Workspace.TargetId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), dockerClient.StopWorkspace(workspaceReq.Workspace, logWriter)
}

func (p *FlyProvider) DestroyWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id, workspaceReq.Workspace.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(workspaceReq.Workspace.TargetId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: workspaceReq.Workspace.TargetId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.DestroyWorkspace(workspaceReq.Workspace, p.getWorkspaceDir(workspaceReq), sshClient)
}

func (p *FlyProvider) GetWorkspaceProviderMetadata(workspaceReq *provider.WorkspaceRequest) (string, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id, workspaceReq.Workspace.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(workspaceReq.Workspace.Target.Id)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return "", err
	}

	return dockerClient.GetWorkspaceProviderMetadata(workspaceReq.Workspace)
}

func (p *FlyProvider) getTargetLogWriter(targetId, targetName string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.TargetLogsDir != nil {
		loggerFactory := logs.NewLoggerFactory(logs.LoggerFactoryConfig{
			LogsDir:     *p.TargetLogsDir,
			ApiUrl:      p.ApiUrl,
			ApiKey:      p.ApiKey,
			ApiBasePath: &logs.ApiBasePathTarget,
		})
		workspaceLogWriter, err := loggerFactory.CreateLogger(targetId, targetName, logs.LogSourceProvider)
		if err == nil {
			logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, workspaceLogWriter)
			cleanupFunc = func() { workspaceLogWriter.Close() }
		}
	}

	return logWriter, cleanupFunc
}

func (p *FlyProvider) getWorkspaceLogWriter(workspaceId, workspaceName string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.WorkspaceLogsDir != nil {
		loggerFactory := logs.NewLoggerFactory(logs.LoggerFactoryConfig{
			LogsDir:     *p.WorkspaceLogsDir,
			ApiUrl:      p.ApiUrl,
			ApiKey:      p.ApiKey,
			ApiBasePath: &logs.ApiBasePathWorkspace,
		})
		workspaceLogWriter, err := loggerFactory.CreateLogger(workspaceId, workspaceName, logs.LogSourceProvider)
		if err == nil {
			logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, workspaceLogWriter)
			cleanupFunc = func() { workspaceLogWriter.Close() }
		}
	}

	return logWriter, cleanupFunc
}

func (p *FlyProvider) getTargetDir(targetId string) string {
	return fmt.Sprintf("/tmp/%s", targetId)
}

func (p *FlyProvider) getWorkspaceDir(workspaceReq *provider.WorkspaceRequest) string {
	return path.Join(
		p.getTargetDir(workspaceReq.Workspace.TargetId),
		fmt.Sprintf("%s-%s", workspaceReq.Workspace.TargetId, workspaceReq.Workspace.Name),
	)
}

func (a *FlyProvider) CheckRequirements() (*[]provider.RequirementStatus, error) {
	results := []provider.RequirementStatus{}
	return &results, nil
}
