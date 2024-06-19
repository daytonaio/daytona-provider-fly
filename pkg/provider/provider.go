package provider

import (
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/daytonaio/daytona-provider-fly/internal"
	logwriters "github.com/daytonaio/daytona-provider-fly/internal/log"
	flyutil "github.com/daytonaio/daytona-provider-fly/pkg/provider/util"
	"github.com/daytonaio/daytona-provider-fly/pkg/types"
	"github.com/daytonaio/daytona/pkg/logger"
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/superfly/fly-go"
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
}

// Initialize initializes the provider with the given configuration.
func (p *FlyProvider) Initialize(req provider.InitializeProviderRequest) (*util.Empty, error) {
	p.BasePath = &req.BasePath
	p.DaytonaDownloadUrl = &req.DaytonaDownloadUrl
	p.DaytonaVersion = &req.DaytonaVersion
	p.ServerUrl = &req.ServerUrl
	p.ApiUrl = &req.ApiUrl
	p.ApiPort = &req.ApiPort
	p.ServerPort = &req.ServerPort
	p.LogsDir = &req.LogsDir
	p.NetworkKey = &req.NetworkKey

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
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	if err := flyutil.CreateApp(workspaceReq.Workspace, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to create workspace: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Workspace created.\n"))

	return new(util.Empty), nil
}

func (p *FlyProvider) StartWorkspace(_ *provider.WorkspaceRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p *FlyProvider) StopWorkspace(_ *provider.WorkspaceRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p *FlyProvider) DestroyWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getWorkspaceLogWriter(workspaceReq.Workspace.Id)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(workspaceReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	if err := flyutil.DeleteApp(workspaceReq.Workspace, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to destroy workspace: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Workspace destroyed.\n"))

	return new(util.Empty), nil
}

func (p *FlyProvider) GetWorkspaceInfo(workspaceReq *provider.WorkspaceRequest) (*workspace.WorkspaceInfo, error) {
	workspaceInfo := &workspace.WorkspaceInfo{
		Name: workspaceReq.Workspace.Name,
	}

	var projectInfos []*workspace.ProjectInfo
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
	if p.DaytonaDownloadUrl == nil {
		return nil, errors.New("DaytonaDownloadUrl not set. Did you forget to call Initialize")
	}
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)

	targetOptions, err := types.ParseTargetOptions(projectReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	initScript := util.GetProjectStartScript(*p.DaytonaDownloadUrl, projectReq.Project.ApiKey)
	machine, err := flyutil.CreateMachine(projectReq.Project, targetOptions, projectReq.ContainerRegistry, logWriter, initScript)
	if err != nil {
		logWriter.Write([]byte("Failed to create machine: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Project created\n"))

	if err := checkMachineReady(projectReq, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to check machine status: " + err.Error() + "\n"))
		return nil, err
	}

	go func() {
		if err := flyutil.GetMachineLogs(projectReq.Project, targetOptions, machine.ID, logWriter); err != nil {
			logWriter.Write([]byte(err.Error()))
			defer cleanupFunc()
		}
	}()

	return new(util.Empty), nil
}

func (p *FlyProvider) StartProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(projectReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	if err := flyutil.StartMachine(projectReq.Project, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to start machine: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Project started.\n"))

	return new(util.Empty), nil
}

func (p *FlyProvider) StopProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(projectReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	if err := flyutil.StopMachine(projectReq.Project, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to stop machine: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Project stopped.\n"))

	return new(util.Empty), nil
}

func (p *FlyProvider) DestroyProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(projectReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	if err := flyutil.DeleteMachine(projectReq.Project, targetOptions); err != nil {
		logWriter.Write([]byte("Failed to destroy machine: " + err.Error() + "\n"))
		return nil, err
	}
	logWriter.Write([]byte("Project destroyed.\n"))

	return new(util.Empty), nil
}

func (p *FlyProvider) GetProjectInfo(projectReq *provider.ProjectRequest) (*workspace.ProjectInfo, error) {
	return p.getProjectInfo(projectReq)
}

func (p *FlyProvider) getProjectInfo(projectReq *provider.ProjectRequest) (*workspace.ProjectInfo, error) {
	logWriter, cleanupFunc := p.getProjectLogWriter(projectReq.Project.WorkspaceId, projectReq.Project.Name)
	defer cleanupFunc()

	targetOptions, err := types.ParseTargetOptions(projectReq.TargetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to parse target options: " + err.Error() + "\n"))
		return nil, err
	}

	machine, err := flyutil.GetMachine(projectReq.Project, targetOptions)
	if err != nil {
		logWriter.Write([]byte("Failed to get machine: " + err.Error() + "\n"))
		return nil, err
	}

	metadata := types.ProjectMetadata{
		MachineId: machine.ID,
		VolumeId:  machine.Config.Mounts[0].Volume,
	}

	jsonMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	return &workspace.ProjectInfo{
		Name:             projectReq.Project.Name,
		IsRunning:        machine.State == fly.MachineStateStarted,
		Created:          machine.CreatedAt,
		ProviderMetadata: string(jsonMetadata),
	}, nil
}

func (p *FlyProvider) getWorkspaceLogWriter(workspaceId string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.LogsDir != nil {
		loggerFactory := logger.NewLoggerFactory(*p.LogsDir)
		wsLogWriter := loggerFactory.CreateWorkspaceLogger(workspaceId)
		logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, wsLogWriter)
		cleanupFunc = func() { wsLogWriter.Close() }
	}

	return logWriter, cleanupFunc
}

func (p *FlyProvider) getProjectLogWriter(workspaceId string, projectName string) (io.Writer, func()) {
	logWriter := io.MultiWriter(&logwriters.InfoLogWriter{})
	cleanupFunc := func() {}

	if p.LogsDir != nil {
		loggerFactory := logger.NewLoggerFactory(*p.LogsDir)
		projectLogWriter := loggerFactory.CreateProjectLogger(workspaceId, projectName)
		logWriter = io.MultiWriter(&logwriters.InfoLogWriter{}, projectLogWriter)
		cleanupFunc = func() { projectLogWriter.Close() }
	}

	return logWriter, cleanupFunc
}

// checkMachineReady checks if the machine for the given workspace is ready to use.
// It polls the status periodically until the machine is ready or a timeout is reached.
func checkMachineReady(projectReq *provider.ProjectRequest, targetOptions *types.TargetOptions) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	end := time.Now().Add(5 * time.Minute)

	for {
		select {
		case <-ticker.C:
			if flyutil.IsMachineReady(projectReq.Project, targetOptions) {
				return nil
			}
		default:
			if time.Now().After(end) {
				return errors.New("machine was not ready within 5 minutes")
			}
		}
	}
}
