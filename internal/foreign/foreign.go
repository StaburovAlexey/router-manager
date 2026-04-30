package foreign

import (
	"context"
	"fmt"
	"strings"

	"vpn-router/internal/config"
	"vpn-router/internal/reality"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/sshclient"
)

type Service struct {
	Paths      config.Paths
	Runner     shell.Runner
	SSH        sshclient.Client
	SNI        reality.Selector
	UUID       string
	PrivateKey string
}

func (s Service) Add(ctx context.Context, server config.ForeignServer) (config.ForeignServer, error) {
	if server.Name == "" {
		return server, fmt.Errorf("имя foreign-сервера не может быть пустым")
	}
	servers, err := config.LoadForeign(s.Paths)
	if err != nil {
		return server, err
	}
	for _, existing := range servers.Servers {
		if existing.Name == server.Name {
			return server, fmt.Errorf("foreign-сервер %s уже существует", server.Name)
		}
	}
	target := sshclient.Target{User: server.SSHUser, IP: server.IP, Port: server.SSHPort}
	if err := s.SSH.Check(ctx, target); err != nil {
		return server, fmt.Errorf("SSH-доступ к foreign-серверу не работает: %w", err)
	}
	if server.Reality.SNI == "" {
		server.Reality.SNI, err = s.SNI.Select(ctx)
		if err != nil {
			return server, err
		}
	}
	keypair, err := s.remoteKeypair(ctx, target)
	if err != nil {
		return server, err
	}
	server.Reality.PublicKey = keypair.PublicKey
	shortID, err := reality.RandomHex(4)
	if err != nil {
		return server, err
	}
	server.Reality.ShortID = shortID
	if err := s.applyRemoteConfig(ctx, target, server, keypair.PrivateKey); err != nil {
		return server, err
	}
	servers.Servers = append(servers.Servers, server)
	if err := config.SaveForeign(s.Paths, servers); err != nil {
		return server, err
	}
	selected := s.currentRUSelected(ctx)
	if err := s.RefreshRU(ctx, servers, selected); err != nil {
		return server, err
	}
	return server, nil
}

func (s Service) Remove(ctx context.Context, name string, switchAuto bool) error {
	servers, err := config.LoadForeign(s.Paths)
	if err != nil {
		return err
	}
	selected := s.currentRUSelected(ctx)
	if selected == name && !switchAuto {
		return fmt.Errorf("foreign-сервер %s выбран на RU вручную. Повторите с --switch-auto", name)
	}
	filtered := servers.Servers[:0]
	found := false
	for _, server := range servers.Servers {
		if server.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, server)
	}
	if !found {
		return fmt.Errorf("foreign-сервер %s не найден", name)
	}
	servers.Servers = filtered
	if selected == name && switchAuto {
		selected = ""
	}
	if selected != "" && !containsServer(servers, selected) {
		selected = ""
	}
	if err := config.SaveForeign(s.Paths, servers); err != nil {
		return err
	}
	return s.RefreshRU(ctx, servers, selected)
}

func (s Service) Test(ctx context.Context, name string) error {
	server, err := s.find(name)
	if err != nil {
		return err
	}
	return s.SSH.Check(ctx, sshclient.Target{User: server.SSHUser, IP: server.IP, Port: server.SSHPort})
}

func (s Service) Cleanup(ctx context.Context, name string) error {
	server, err := s.find(name)
	if err != nil {
		return err
	}
	target := sshclient.Target{User: server.SSHUser, IP: server.IP, Port: server.SSHPort}
	_, err = s.SSH.Run(ctx, target, `systemctl disable --now sing-box || true; install -d -m 700 /etc/sing-box/backups; if [ -f /etc/sing-box/config.json ]; then mv /etc/sing-box/config.json /etc/sing-box/backups/config.$(date -u +%Y%m%dT%H%M%SZ).json; fi; rm -rf /etc/foreign-vpn`)
	return err
}

func (s Service) List() (config.ForeignServers, error) {
	return config.LoadForeign(s.Paths)
}

type keypair struct {
	PrivateKey string
	PublicKey  string
}

func (s Service) remoteKeypair(ctx context.Context, target sshclient.Target) (keypair, error) {
	if err := (singbox.Installer{}).EnsureRemoteInstalled(ctx, s.SSH, target); err != nil {
		return keypair{}, fmt.Errorf("не удалось установить или проверить sing-box на foreign-сервере: %w", err)
	}
	out, err := s.SSH.Run(ctx, target, "sing-box generate reality-keypair")
	if err != nil {
		return keypair{}, fmt.Errorf("не удалось сгенерировать REALITY keypair на foreign-сервере: %w", err)
	}
	var kp keypair
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PrivateKey:") {
			kp.PrivateKey = strings.TrimSpace(strings.TrimPrefix(line, "PrivateKey:"))
		}
		if strings.HasPrefix(line, "PublicKey:") {
			kp.PublicKey = strings.TrimSpace(strings.TrimPrefix(line, "PublicKey:"))
		}
	}
	if kp.PrivateKey == "" || kp.PublicKey == "" {
		return kp, fmt.Errorf("sing-box generate reality-keypair вернул неожиданный формат")
	}
	return kp, nil
}

func (s Service) applyRemoteConfig(ctx context.Context, target sshclient.Target, server config.ForeignServer, privateKey string) error {
	uuid := s.UUID
	if uuid == "" {
		cfg, err := config.Load(s.Paths)
		if err != nil {
			return err
		}
		uuid = cfg.Reality.UUID
	}
	if uuid == "" {
		return fmt.Errorf("REALITY UUID не настроен: сначала выполните setup")
	}
	data, err := singbox.RenderForeign(server, uuid, privateKey)
	if err != nil {
		return err
	}
	return singbox.ApplyRemoteConfig(ctx, s.SSH, target, data)
}

func (s Service) RefreshRU(ctx context.Context, servers config.ForeignServers, selected string) error {
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return err
	}
	if cfg.RUServer.IP == "" {
		return nil
	}
	target := sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}
	privateKey := s.PrivateKey
	if privateKey == "" {
		privateKey, err = singbox.RemoteRealityPrivateKey(ctx, s.SSH, target)
		if err != nil {
			return err
		}
	}
	selected = validSelectedForeign(servers, selected)
	return singbox.ApplyRemoteRU(ctx, s.SSH, target, cfg, privateKey, servers, selected)
}

func (s Service) find(name string) (config.ForeignServer, error) {
	servers, err := config.LoadForeign(s.Paths)
	if err != nil {
		return config.ForeignServer{}, err
	}
	for _, server := range servers.Servers {
		if server.Name == name {
			return server, nil
		}
	}
	return config.ForeignServer{}, fmt.Errorf("foreign-сервер %s не найден", name)
}

func (s Service) ruTarget() sshclient.Target {
	cfg, _ := config.Load(s.Paths)
	return sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}
}

func (s Service) currentRUSelected(ctx context.Context) string {
	cfg, err := config.Load(s.Paths)
	if err != nil || cfg.RUServer.IP == "" {
		return ""
	}
	selected, _ := selectedForeignOnRU(ctx, s.SSH, sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort})
	return selected
}

func containsServer(servers config.ForeignServers, name string) bool {
	for _, server := range servers.Servers {
		if server.Name == name {
			return true
		}
	}
	return false
}

func validSelectedForeign(servers config.ForeignServers, selected string) string {
	if selected == "" || containsServer(servers, selected) {
		return selected
	}
	return ""
}

func selectedForeignOnRU(ctx context.Context, ssh sshclient.Client, target sshclient.Target) (string, error) {
	out, err := ssh.Run(ctx, target, "cat /etc/ru-vpn/state.json 2>/dev/null || true")
	if err != nil {
		return "", err
	}
	if !strings.Contains(out, `"mode":"manual"`) && !strings.Contains(out, `"mode": "manual"`) {
		return "", nil
	}
	for _, marker := range []string{`"selected_foreign":"`, `"selected_foreign": "`} {
		idx := strings.Index(out, marker)
		if idx >= 0 {
			rest := out[idx+len(marker):]
			end := strings.Index(rest, `"`)
			if end >= 0 {
				return rest[:end], nil
			}
		}
	}
	return "", nil
}
