package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/daytonaio/daytona/pkg/agent/ssh/config"
	"github.com/daytonaio/daytona/pkg/docker"
	"github.com/daytonaio/daytona/pkg/tailscale"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
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

	localSockPath := filepath.Join(p.LocalSockDir, workspaceId, "docker-forward.sock")

	if _, err := os.Stat(filepath.Dir(localSockPath)); err != nil {
		err := os.MkdirAll(filepath.Dir(localSockPath), 0755)
		if err != nil {
			return nil, err
		}

		startedChan, errChan := tailscale.ForwardRemoteUnixSock(tailscale.ForwardConfig{
			Ctx:        context.Background(),
			TsnetConn:  tsnetConn,
			Hostname:   workspaceId,
			SshPort:    config.SSH_PORT,
			LocalSock:  localSockPath,
			RemoteSock: "/var/run/docker.sock",
		})

		go func() {
			err := <-errChan
			if err != nil {
				log.Error(err)
				startedChan <- false
				_ = os.Remove(localSockPath)
			}
		}()

		started := <-startedChan
		if !started {
			return nil, errors.New("failed to start SSH tunnel")
		}
	}

	cli, err := client.NewClientWithOpts(client.WithHost(fmt.Sprintf("unix://%s", localSockPath)), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return docker.NewDockerClient(docker.DockerClientConfig{
		ApiClient: cli,
	}), nil
}
