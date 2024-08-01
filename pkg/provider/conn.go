package provider

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/daytonaio/daytona/pkg/agent/ssh/config"
	"github.com/daytonaio/daytona/pkg/docker"
	"github.com/daytonaio/daytona/pkg/tailscale"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"tailscale.com/tsnet"
)

func (p *FlyProvider) getTsnetConn() (*tsnet.Server, error) {
	if p.tsnetConn == nil {
		tsnetConn, err := tailscale.GetConnection(&tailscale.TsnetConnConfig{
			AuthKey:    *p.NetworkKey,
			ControlURL: *p.ServerUrl,
			Dir:        filepath.Join(*p.BasePath, "tsnet", uuid.NewString()),
			Logf:       func(format string, args ...any) {},
			Hostname:   fmt.Sprintf("fly-provider-%s", uuid.NewString()),
		})
		if err != nil {
			return nil, err
		}
		p.tsnetConn = tsnetConn
	}

	return p.tsnetConn, nil
}

func (p *FlyProvider) waitForDial(workspaceId string, dialTimeout time.Duration) error {
	tsnetConn, err := p.getTsnetConn()
	if err != nil {
		return err
	}

	dialStartTime := time.Now()
	for {
		if time.Since(dialStartTime) > dialTimeout {
			return fmt.Errorf("timeout: dialing timed out after %f minutes", dialTimeout.Minutes())
		}

		dialConn, err := tsnetConn.Dial(context.Background(), "tcp", fmt.Sprintf("%s:%d", workspaceId, config.SSH_PORT))
		if err == nil {
			dialConn.Close()
			return nil
		}

		time.Sleep(time.Second)
	}
}

func (p *FlyProvider) getDockerClient(workspaceId string) (docker.IDockerClient, error) {
	tsnetConn, err := p.getTsnetConn()
	if err != nil {
		return nil, err
	}

	remoteHost := fmt.Sprintf("http://%s:2375", workspaceId)
	cli, err := client.NewClientWithOpts(client.WithDialContext(tsnetConn.Dial), client.WithHost(remoteHost), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return docker.NewDockerClient(docker.DockerClientConfig{
		ApiClient: cli,
	}), nil
}
