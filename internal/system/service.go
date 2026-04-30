package system

import (
	"context"
	"fmt"
	"strings"

	"vpn-router/internal/shell"
)

func Systemctl(ctx context.Context, runner shell.Runner, action string, service string) error {
	return runner.Run(ctx, "systemctl", action, service)
}

func UnmaskEnable(ctx context.Context, runner shell.Runner, service string) error {
	if err := Systemctl(ctx, runner, "unmask", service); err != nil {
		return fmt.Errorf("не удалось снять mask со службы %s: %w", service, err)
	}
	if err := Systemctl(ctx, runner, "enable", service); err != nil {
		if strings.Contains(err.Error(), "is masked") {
			return fmt.Errorf("служба %s замаскирована. Попробуйте вручную: sudo systemctl unmask %s && sudo systemctl enable %s", service, service, service)
		}
		return err
	}
	return nil
}

func ServiceStatus(ctx context.Context, runner shell.Runner, service string) string {
	out, err := runner.Output(ctx, "systemctl", "is-active", service)
	if err != nil {
		return "inactive"
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "unknown"
	}
	return out
}

func RestartAndCheck(ctx context.Context, runner shell.Runner, service string) error {
	if err := Systemctl(ctx, runner, "restart", service); err != nil {
		return err
	}
	if status := ServiceStatus(ctx, runner, service); status != "active" {
		return fmt.Errorf("служба %s не active, текущий статус: %s", service, status)
	}
	return nil
}
