package foreign

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/sshclient"
)

func TestValidSelectedForeignFallsBackToAutoWhenMissing(t *testing.T) {
	servers := config.ForeignServers{Servers: []config.ForeignServer{{Name: "1"}}}
	if got := validSelectedForeign(servers, "finland u1 host"); got != "" {
		t.Fatalf("selected = %q, want auto fallback", got)
	}
}

func TestValidSelectedForeignKeepsExistingSelection(t *testing.T) {
	servers := config.ForeignServers{Servers: []config.ForeignServer{{Name: "1"}}}
	if got := validSelectedForeign(servers, "1"); got != "1" {
		t.Fatalf("selected = %q, want %q", got, "1")
	}
}

func TestAddReportsPartialSuccessWhenRURefreshFails(t *testing.T) {
	t.Setenv("ROUTER_MANAGER_SING_BOX_VERSION", "1.12.0")
	paths := config.NewPaths(t.TempDir())
	paths.SingBoxLocalConf = filepath.Join(paths.BaseDir, "sing-box.json")
	cfg := config.DefaultConfig()
	cfg.RUServer.IP = "203.0.113.10"
	cfg.RUServer.SSHUser = "root"
	cfg.RUServer.SSHPort = 22
	cfg.RUServer.TunnelPort = 443
	cfg.Reality.UUID = "11111111-1111-4111-8111-111111111111"
	cfg.Reality.SNI = "ru.example.com"
	cfg.Reality.ShortID = "ru-short"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &partialRefreshRunner{}
	svc := Service{
		Paths:      paths,
		SSH:        sshclient.Client{Runner: runner},
		PrivateKey: "ru-private",
	}
	server := config.ForeignServer{
		Name:       "de-1",
		IP:         "198.51.100.20",
		SSHUser:    "root",
		SSHPort:    22,
		TunnelPort: 443,
		Reality: config.RealityConfig{
			SNI: "de.example.com",
		},
	}

	_, err := svc.Add(context.Background(), server)
	if err == nil {
		t.Fatal("expected partial refresh error")
	}
	for _, want := range []string{"de-1", "добавлен и сохранён", "входной сервер не обновлён", "ru refresh failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not contain %q: %v", want, err)
		}
	}
	servers, loadErr := config.LoadForeign(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(servers.Servers) != 1 || servers.Servers[0].Name != "de-1" {
		t.Fatalf("foreign server was not saved: %#v", servers)
	}
}

type partialRefreshRunner struct {
	calls []string
}

func (r *partialRefreshRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil
}

func (r *partialRefreshRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	command := ""
	target := ""
	if len(args) >= 2 {
		command = args[len(args)-1]
		target = args[len(args)-2]
	}
	switch {
	case command == "echo ok":
		return "ok", nil
	case command == "cat /etc/ru-tunnel/state.json 2>/dev/null || true":
		return `{"mode":"auto","selected_foreign":""}`, nil
	case command == "sing-box generate reality-keypair":
		return "PrivateKey: foreign-private\nPublicKey: foreign-public", nil
	case target == "root@203.0.113.10" && strings.Contains(command, "systemctl restart sing-box"):
		return "", errors.New("ru refresh failed")
	default:
		return "", nil
	}
}
