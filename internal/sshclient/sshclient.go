package sshclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"router-manager/internal/shell"
)

type Target struct {
	User string
	IP   string
	Port int
}

func (t Target) Addr() string {
	return fmt.Sprintf("%s@%s", t.User, t.IP)
}

type Client struct {
	Runner shell.Runner
}

func (c Client) Check(ctx context.Context, target Target) error {
	out, err := c.Run(ctx, target, "echo ok")
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("SSH вернул неожиданный ответ: %s", out)
	}
	return nil
}

func (c Client) Run(ctx context.Context, target Target, command string) (string, error) {
	if target.Port == 0 {
		target.Port = 22
	}
	out, err := c.Runner.Output(ctx,
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-p", strconv.Itoa(target.Port),
		target.Addr(),
		command,
	)
	if err != nil {
		return "", ExplainError(target, err)
	}
	return out, nil
}

func (c Client) RunInteractive(ctx context.Context, target Target, command string) error {
	if target.Port == 0 {
		target.Port = 22
	}
	if err := c.Runner.Run(ctx,
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-p", strconv.Itoa(target.Port),
		target.Addr(),
		command,
	); err != nil {
		return ExplainError(target, err)
	}
	return nil
}

func EnsureRootKeyPair(ctx context.Context) (string, error) {
	dir := "/root/.ssh"
	privateKey := filepath.Join(dir, "id_ed25519")
	publicKey := privateKey + ".pub"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("не удалось создать %s: %w", dir, err)
	}
	if _, err := os.Stat(privateKey); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-C", "router-manager", "-f", privateKey, "-N", "")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("ssh-keygen: %w: %s", err, strings.TrimSpace(string(out)))
		}
	} else if err != nil {
		return "", err
	}
	if _, err := os.Stat(publicKey); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "ssh-keygen", "-y", "-f", privateKey)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("не удалось восстановить public key из %s: %w", privateKey, err)
		}
		if err := os.WriteFile(publicKey, out, 0o644); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(privateKey, 0o600)
	_ = os.Chmod(publicKey, 0o644)
	data, err := os.ReadFile(publicKey)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func ScanHostKey(ctx context.Context, target Target) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh-keyscan", "-H", "-p", strconv.Itoa(port(target)), target.IP)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssh-keyscan -p %d %s: %w", port(target), target.IP, err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("ssh-keyscan не вернул host key для %s:%d", target.IP, port(target))
	}
	return text, nil
}

var rootSSHDir = "/root/.ssh"

func TrustHostKey(ctx context.Context, target Target, hostKey string) error {
	hostKey = strings.TrimSpace(hostKey)
	if hostKey == "" {
		return fmt.Errorf("host key пустой")
	}
	dir := rootSSHDir
	path := filepath.Join(dir, "known_hosts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := removeKnownHost(ctx, target, path); err != nil {
		return err
	}
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), hostKey) {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(file, hostKey+"\n"); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	return os.Chmod(path, 0o600)
}

func removeKnownHost(ctx context.Context, target Target, knownHosts string) error {
	if _, err := os.Stat(knownHosts); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	for _, host := range knownHostPatterns(target) {
		cmd := exec.CommandContext(ctx, "ssh-keygen", "-R", host, "-f", knownHosts)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ssh-keygen -R %s -f %s: %w: %s", host, knownHosts, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func knownHostPatterns(target Target) []string {
	p := port(target)
	bracketed := fmt.Sprintf("[%s]:%d", target.IP, p)
	if p == 22 {
		return []string{target.IP, bracketed}
	}
	return []string{bracketed}
}

func InstallPublicKeyWithPassword(ctx context.Context, target Target, password string, publicKey string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("SSH-пароль пустой")
	}
	if _, err := exec.LookPath("sshpass"); err != nil {
		return fmt.Errorf("sshpass не найден. Установите зависимость: sudo apt install sshpass")
	}
	encodedKey := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(publicKey)))
	remoteCommand := fmt.Sprintf(`umask 077; mkdir -p ~/.ssh; touch ~/.ssh/authorized_keys; key="$(printf %%s %q | base64 -d)"; grep -qxF "$key" ~/.ssh/authorized_keys || printf '%%s\n' "$key" >> ~/.ssh/authorized_keys; chmod 700 ~/.ssh; chmod 600 ~/.ssh/authorized_keys`, encodedKey)
	cmd := exec.CommandContext(ctx,
		"sshpass", "-e",
		"ssh",
		"-o", "PreferredAuthentications=password",
		"-o", "PubkeyAuthentication=no",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "ConnectTimeout=8",
		"-p", strconv.Itoa(port(target)),
		target.Addr(),
		remoteCommand,
	)
	cmd.Env = append(os.Environ(), "SSHPASS="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("не удалось добавить SSH-ключ на сервер %s: %w: %s", target.Addr(), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ExplainError(target Target, err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	base := fmt.Sprintf("%v", err)
	switch {
	case strings.Contains(text, "Host key verification failed"):
		return fmt.Errorf("%s\n\nПроблема: host key сервера не подтверждён для root-пользователя на мини-ПК.\nПриложение запущено через sudo, поэтому SSH проверяется от root и использует /root/.ssh/known_hosts.\n\nВыполните на мини-ПК:\n  sudo mkdir -p /root/.ssh\n  sudo ssh-keyscan -H -p %d %s | sudo tee -a /root/.ssh/known_hosts >/dev/null\n  sudo chmod 700 /root/.ssh\n  sudo chmod 600 /root/.ssh/known_hosts\n\nПроверка:\n  sudo ssh -o BatchMode=yes -o ConnectTimeout=8 -p %d %s \"echo ok\"", base, port(target), target.IP, port(target), target.Addr())
	case strings.Contains(text, "Permission denied"):
		return fmt.Errorf("%s\n\nПроблема: сервер не принял SSH-ключ root-пользователя мини-ПК.\nПриложение работает через sudo, поэтому проверка идёт как root на мини-ПК, а не как текущий пользователь.\n\nВариант 1: создать отдельный root-ключ для router-manager и добавить его на сервер:\n  sudo mkdir -p /root/.ssh\n  sudo ssh-keygen -t ed25519 -C \"router-manager\" -f /root/.ssh/id_ed25519\n  sudo ssh-copy-id -i /root/.ssh/id_ed25519.pub -p %d %s\n\nВариант 2: если ваш пользовательский ключ уже добавлен на сервер, скопировать его root-пользователю мини-ПК:\n  sudo mkdir -p /root/.ssh\n  sudo cp ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub /root/.ssh/\n  sudo chmod 700 /root/.ssh\n  sudo chmod 600 /root/.ssh/id_ed25519\n  sudo chmod 644 /root/.ssh/id_ed25519.pub\n\nЕсли сервер не принимает пароль и ssh-copy-id невозможен, добавьте содержимое /root/.ssh/id_ed25519.pub в ~/.ssh/authorized_keys пользователя %s на сервере через консоль провайдера.\n\nПроверка:\n  sudo ssh -o BatchMode=yes -o ConnectTimeout=8 -p %d %s \"echo ok\"", base, port(target), target.Addr(), target.User, port(target), target.Addr())
	case strings.Contains(text, "Could not resolve hostname"):
		return fmt.Errorf("%s\n\nПроблема: имя или IP сервера не резолвится. Проверьте адрес сервера и DNS на мини-ПК.", base)
	case strings.Contains(text, "Connection timed out") || strings.Contains(text, "No route to host"):
		return fmt.Errorf("%s\n\nПроблема: сервер недоступен по сети. Проверьте IP, порт SSH, firewall и интернет на мини-ПК.", base)
	case strings.Contains(text, "Connection refused"):
		return fmt.Errorf("%s\n\nПроблема: SSH-порт доступен, но соединение отклонено. Проверьте порт SSH и службу sshd на сервере.", base)
	default:
		return err
	}
}

func port(target Target) int {
	if target.Port == 0 {
		return 22
	}
	return target.Port
}
