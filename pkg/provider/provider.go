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
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/ssh"
	"github.com/daytonaio/daytona/pkg/tailscale"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/daytonaio/daytona/pkg/workspace/project"
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
	ApiPort            *uint32
	ServerPort         *uint32
	LogsDir            *string
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
	p.ApiPort = &req.ApiPort
	p.ServerPort = &req.ServerPort
	p.LogsDir = &req.LogsDir

	return new(util.Empty), nil
}

// GetInfo returns the provider information.
func (p *FlyProvider) GetInfo() (provider.ProviderInfo, error) {
	return provider.ProviderInfo{
		Name:    "fly-provider",
		Version: internal.Version,
	}, nil
}

func (p *FlyProvider) GetTargetManifest() (*provider.ProviderTargetManifest, error) {
	return types.GetTargetManifest(), nil
}

func (p *FlyProvider) GetDefaultTargets() (*[]provider.ProviderTarget, error) {
	return new([]provider.ProviderTarget), nil
}

func (p *FlyProvider) CreateWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	if p.DaytonaDownloadUrl == nil {
		return nil, errors.New("DaytonaDownloadUrl not set. Did you forget to call Initialize")
	}
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	initScript := fmt.Sprintf(`apk add --no-cache curl bash && \ 
	curl -sfL -H "Authorization: Bearer %s" %s | bash`,
		workspaceReq.Workspace.ApiKey,
		*p.DaytonaDownloadUrl,
	)

	machine, err := flyutil.CreateWorkspace(workspaceReq.Workspace, targetOptions, initScript)
	if err != nil {
		logWriter.Write([]byte("Failed to create workspace: " + err.Error() + "\n"))
		return nil, err
	}

	go func() {
		if err := flyutil.GetWorkspaceLogs(workspaceReq.Workspace, targetOptions, machine.ID, logWriter); err != nil {
			logWriter.Write([]byte(err.Error()))
			defer cleanupFunc()
		}
	}()

	err = p.waitForDial(workspaceReq.Workspace.Id, 5*time.Minute)
	if err != nil {
		logWriter.Write([]byte("Failed to dial: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Workspace agent started.\n"))

	client, err := p.getDockerClient(workspaceReq.Workspace.Id)
	if err != nil {
		logWriter.Write([]byte("Failed to get client: " + err.Error() + "\n"))
		return nil, err
	}

	workspaceDir := p.getWorkspaceDir(workspaceReq.Workspace.Id)
	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: workspaceReq.Workspace.Id,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), client.CreateWorkspace(workspaceReq.Workspace, workspaceDir, logWriter, sshClient)
}

func (p *FlyProvider) StartWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.StartWorkspace(workspaceReq.Workspace, targetOptions)
}

func (p *FlyProvider) StopWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.StopWorkspace(workspaceReq.Workspace, targetOptions)
}

func (p *FlyProvider) DestroyWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), flyutil.DeleteWorkspace(workspaceReq.Workspace, targetOptions)
}

func (p *FlyProvider) GetWorkspaceInfo(workspaceReq *provider.WorkspaceRequest) (*workspace.WorkspaceInfo, error) {
	workspaceInfo, err := p.getWorkspaceInfo(workspaceReq)
	if err != nil {
		return nil, err
	}

	var projectInfos []*project.ProjectInfo
	for _, project := range workspaceReq.Workspace.Projects {
		projectInfo, err := p.GetProjectInfo(&provider.ProjectRequest{
			TargetOptions: workspaceReq.TargetOptions,
			Project:       project,
		})
		if err != nil {
			return nil, err
		}
		projectInfos = append(projectInfos, projectInfo)
	}
	workspaceInfo.Projects = projectInfos

	return workspaceInfo, nil
}

func (p *FlyProvider) CreateProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(projectReq.Project.WorkspaceId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: projectReq.Project.WorkspaceId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.CreateProject(&docker.CreateProjectOptions{
		Project:    projectReq.Project,
		ProjectDir: p.getProjectDir(projectReq),
		Cr:         projectReq.ContainerRegistry,
		LogWriter:  logWriter,
		Gpc:        projectReq.GitProviderConfig,
		SshClient:  sshClient,
	})
}

func (p *FlyProvider) StartProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	if p.DaytonaDownloadUrl == nil {
		return nil, errors.New("DaytonaDownloadUrl not set. Did you forget to call Initialize")
	}
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(projectReq.Project.WorkspaceId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: projectReq.Project.WorkspaceId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.StartProject(&docker.CreateProjectOptions{
		Project:    projectReq.Project,
		ProjectDir: p.getProjectDir(projectReq),
		Cr:         projectReq.ContainerRegistry,
		LogWriter:  logWriter,
		Gpc:        projectReq.GitProviderConfig,
		SshClient:  sshClient,
	}, *p.DaytonaDownloadUrl)
}

func (p *FlyProvider) StopProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(projectReq.Project.WorkspaceId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	return new(util.Empty), dockerClient.StopProject(projectReq.Project, logWriter)
}

func (p *FlyProvider) DestroyProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(projectReq.Project.WorkspaceId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	sshClient, err := tailscale.NewSshClient(p.tsnetConn, &ssh.SessionConfig{
		Hostname: projectReq.Project.WorkspaceId,
		Port:     config.SSH_PORT,
	})
	if err != nil {
		logWriter.Write([]byte("Failed to create ssh client: " + err.Error() + "\n"))
		return new(util.Empty), err
	}
	defer sshClient.Close()

	return new(util.Empty), dockerClient.DestroyProject(projectReq.Project, p.getProjectDir(projectReq), sshClient)
}

func (p *FlyProvider) GetProjectInfo(projectReq *provider.ProjectRequest) (*project.ProjectInfo, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	dockerClient, err := p.getDockerClient(projectReq.Project.WorkspaceId)
	if err != nil {
		logWriter.Write([]byte("Failed to get docker client: " + err.Error() + "\n"))
		return nil, err
	}

	return dockerClient.GetProjectInfo(projectReq.Project)
}

func (p *FlyProvider) getWorkspaceInfo(workspaceReq *provider.WorkspaceRequest) (*workspace.WorkspaceInfo, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	machine, err := flyutil.GetMachine(workspaceReq.Workspace, targetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to get machine: " + err.Error() + "\n"))
		return nil, err

	}

	metadata := types.WorkspaceMetadata{
		MachineId: machine.ID,
		VolumeId:  machine.Config.Mounts[0].Volume,
		IsRunning: machine.State == fly.MachineStateStarted,
		Created:   machine.CreatedAt,
	}
	jsonMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	return &workspace.WorkspaceInfo{
		Name:             workspaceReq.Workspace.Name,
		ProviderMetadata: string(jsonMetadata),
	}, nil
}

func (p *FlyProvider) getWorkspaceLogWriter(workspaceId string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.LogsDir != nil {
		loggerFactory := logs.NewLoggerFactory(*p.LogsDir)
		wsLogWriter := loggerFactory.CreateWorkspaceLogger(workspaceId, logs.LogSourceProvider)
		logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, wsLogWriter)
		cleanupFunc = func() { wsLogWriter.Close() }
	}

	return logWriter, cleanupFunc
}

func (p *FlyProvider) getProjectLogWriter(workspaceId string, projectName string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.LogsDir != nil {
		loggerFactory := logs.NewLoggerFactory(*p.LogsDir)
		projectLogWriter := loggerFactory.CreateProjectLogger(workspaceId, projectName, logs.LogSourceProvider)
		logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, projectLogWriter)
		cleanupFunc = func() { projectLogWriter.Close() }
	}

	return logWriter, cleanupFunc
}

func (p *FlyProvider) getWorkspaceDir(workspaceId string) string {
	return fmt.Sprintf("/tmp/%s", workspaceId)
}

func (p *FlyProvider) getProjectDir(projectReq *provider.ProjectRequest) string {
	return path.Join(
		p.getWorkspaceDir(projectReq.Project.WorkspaceId),
		fmt.Sprintf("%s-%s", projectReq.Project.WorkspaceId, projectReq.Project.Name),
	)
}
