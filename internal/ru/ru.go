package ru

import (
	"context"
	"fmt"

	"router-manager/internal/config"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
	"router-manager/internal/sshclient"
)

type Service struct {
	Paths  config.Paths
	Runner shell.Runner
	SSH    sshclient.Client
}

func (s Service) Auto(ctx context.Context) error {
	return s.apply(ctx, "")
}

func (s Service) Use(ctx context.Context, name string) error {
	if _, err := s.findForeign(name); err != nil {
		return err
	}
	return s.apply(ctx, name)
}

func (s Service) Test(ctx context.Context, name string) error {
	server, err := s.findForeign(name)
	if err != nil {
		return err
	}
	return s.SSH.Check(ctx, sshclient.Target{User: server.SSHUser, IP: server.IP, Port: server.SSHPort})
}

func (s Service) Status(ctx context.Context) (string, error) {
	target, err := s.target()
	if err != nil {
		return "", err
	}
	return s.SSH.Run(ctx, target, "cat /etc/ru-tunnel/state.json 2>/dev/null || echo '{\"mode\":\"unknown\",\"selected_foreign\":\"\"}'")
}

func (s Service) Logs(ctx context.Context) (string, error) {
	target, err := s.target()
	if err != nil {
		return "", err
	}
	return s.SSH.Run(ctx, target, "journalctl -u sing-box -n 200 --no-pager")
}

func (s Service) Rollback(ctx context.Context) error {
	target, err := s.target()
	if err != nil {
		return err
	}
	_, err = s.SSH.Run(ctx, target, `latest=$(ls -1t /etc/sing-box/backups/config.*.json 2>/dev/null | head -1); test -n "$latest"; cp "$latest" /etc/sing-box/config.json; sing-box check -c /etc/sing-box/config.json; systemctl restart sing-box`)
	return err
}

func (s Service) List() (config.ForeignServers, error) {
	return config.LoadForeign(s.Paths)
}

func (s Service) apply(ctx context.Context, selected string) error {
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return err
	}
	if cfg.RUServer.IP == "" {
		return fmt.Errorf("входной сервер не настроен")
	}
	servers, err := config.LoadForeign(s.Paths)
	if err != nil {
		return err
	}
	target := sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}
	privateKey, err := singbox.RemoteRealityPrivateKey(ctx, s.SSH, target)
	if err != nil {
		return err
	}
	return singbox.ApplyRemoteRU(ctx, s.SSH, target, cfg, privateKey, servers, selected)
}

func (s Service) target() (sshclient.Target, error) {
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return sshclient.Target{}, err
	}
	if cfg.RUServer.IP == "" {
		return sshclient.Target{}, fmt.Errorf("входной сервер не настроен")
	}
	return sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}, nil
}

func (s Service) findForeign(name string) (config.ForeignServer, error) {
	servers, err := config.LoadForeign(s.Paths)
	if err != nil {
		return config.ForeignServer{}, err
	}
	for _, server := range servers.Servers {
		if server.Name == name {
			return server, nil
		}
	}
	return config.ForeignServer{}, fmt.Errorf("выходной сервер %s не найден", name)
}
