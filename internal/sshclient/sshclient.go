package sshclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"vpn-router/internal/shell"
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
		return fmt.Errorf("%s\n\nПроблема: сервер не принял SSH-ключ root-пользователя мини-ПК.\nПриложение работает через sudo, поэтому проверка идёт как root на мини-ПК, а не как текущий пользователь.\n\nВариант 1: создать отдельный root-ключ для vpn-router и добавить его на сервер:\n  sudo mkdir -p /root/.ssh\n  sudo ssh-keygen -t ed25519 -C \"vpn-router\" -f /root/.ssh/id_ed25519\n  sudo ssh-copy-id -i /root/.ssh/id_ed25519.pub -p %d %s\n\nВариант 2: если ваш пользовательский ключ уже добавлен на сервер, скопировать его root-пользователю мини-ПК:\n  sudo mkdir -p /root/.ssh\n  sudo cp ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub /root/.ssh/\n  sudo chmod 700 /root/.ssh\n  sudo chmod 600 /root/.ssh/id_ed25519\n  sudo chmod 644 /root/.ssh/id_ed25519.pub\n\nЕсли сервер не принимает пароль и ssh-copy-id невозможен, добавьте содержимое /root/.ssh/id_ed25519.pub в ~/.ssh/authorized_keys пользователя %s на сервере через консоль провайдера.\n\nПроверка:\n  sudo ssh -o BatchMode=yes -o ConnectTimeout=8 -p %d %s \"echo ok\"", base, port(target), target.Addr(), target.User, port(target), target.Addr())
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
