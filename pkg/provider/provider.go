package provider

import (
	"encoding/json"
	"io"

	internal "github.com/daytonaio/daytona-provider-sample/internal"
	log_writers "github.com/daytonaio/daytona-provider-sample/internal/log"
	provider_types "github.com/daytonaio/daytona-provider-sample/pkg/types"

	"github.com/daytonaio/daytona/pkg/logger"
	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/workspace"
)

type SampleProvider struct {
	BasePath          *string
	ServerDownloadUrl *string
	ServerVersion     *string
	ServerUrl         *string
	ServerApiUrl      *string
	LogsDir           *string
	NetworkKey        *string
	OwnProperty       string
}

func (p *SampleProvider) Initialize(req provider.InitializeProviderRequest) (*util.Empty, error) {
	p.OwnProperty = "my-own-property"

	p.BasePath = &req.BasePath
	p.ServerDownloadUrl = &req.ServerDownloadUrl
	p.ServerVersion = &req.ServerVersion
	p.ServerUrl = &req.ServerUrl
	p.ServerApiUrl = &req.ServerApiUrl
	p.LogsDir = &req.LogsDir
	p.NetworkKey = &req.NetworkKey

	return new(util.Empty), nil
}

func (p SampleProvider) GetInfo() (provider.ProviderInfo, error) {
	return provider.ProviderInfo{
		Name:    "provider-sample",
		Version: internal.Version,
	}, nil
}

func (p SampleProvider) GetTargetManifest() (*provider.ProviderTargetManifest, error) {
	return provider_types.GetTargetManifest(), nil
}

func (p SampleProvider) GetDefaultTargets() (*[]provider.ProviderTarget, error) {
	info, err := p.GetInfo()
	if err != nil {
		return nil, err
	}

	defaultTargets := []provider.ProviderTarget{
		{
			Name:         "default-target",
			ProviderInfo: info,
			Options:      "{\n\t\"Required String\": \"default-required-string\"\n}",
		},
	}
	return &defaultTargets, nil
}

func (p SampleProvider) CreateWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	logWriter := io.MultiWriter(&log_writers.InfoLogWriter{})
	if p.LogsDir != nil {
		loggerFactory := logger.NewLoggerFactory(*p.LogsDir)
		wsLogWriter := loggerFactory.CreateWorkspaceLogger(workspaceReq.Workspace.Id)
		logWriter = io.MultiWriter(&log_writers.InfoLogWriter{}, wsLogWriter)
		defer wsLogWriter.Close()
	}

	logWriter.Write([]byte("Workspace created\n"))

	return new(util.Empty), nil
}

func (p SampleProvider) StartWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) StopWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) DestroyWorkspace(workspaceReq *provider.WorkspaceRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) GetWorkspaceInfo(workspaceReq *provider.WorkspaceRequest) (*workspace.WorkspaceInfo, error) {
	providerMetadata, err := p.getWorkspaceMetadata(workspaceReq)
	if err != nil {
		return nil, err
	}

	workspaceInfo := &workspace.WorkspaceInfo{
		Name:             workspaceReq.Workspace.Name,
		ProviderMetadata: providerMetadata,
	}

	projectInfos := []*workspace.ProjectInfo{}
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

func (p SampleProvider) CreateProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	logWriter := io.MultiWriter(&log_writers.InfoLogWriter{})
	if p.LogsDir != nil {
		loggerFactory := logger.NewLoggerFactory(*p.LogsDir)
		projectLogWriter := loggerFactory.CreateProjectLogger(projectReq.Project.WorkspaceId, projectReq.Project.Name)
		logWriter = io.MultiWriter(&log_writers.InfoLogWriter{}, projectLogWriter)
		defer projectLogWriter.Close()
	}

	logWriter.Write([]byte("Project created\n"))

	return new(util.Empty), nil
}

func (p SampleProvider) StartProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) StopProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) DestroyProject(projectReq *provider.ProjectRequest) (*util.Empty, error) {
	return new(util.Empty), nil
}

func (p SampleProvider) GetProjectInfo(projectReq *provider.ProjectRequest) (*workspace.ProjectInfo, error) {
	providerMetadata := provider_types.ProjectMetadata{
		Property: projectReq.Project.Name,
	}

	metadataString, err := json.Marshal(providerMetadata)
	if err != nil {
		return nil, err
	}

	projectInfo := &workspace.ProjectInfo{
		Name:             projectReq.Project.Name,
		IsRunning:        true,
		Created:          "Created at ...",
		ProviderMetadata: string(metadataString),
	}

	return projectInfo, nil
}

func (p SampleProvider) getWorkspaceMetadata(workspaceReq *provider.WorkspaceRequest) (string, error) {
	metadata := provider_types.WorkspaceMetadata{
		Property: workspaceReq.Workspace.Id,
	}

	jsonContent, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}

	return string(jsonContent), nil
}
