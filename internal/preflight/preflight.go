package preflight

import (
	"context"
	"fmt"
	"os"
	"strings"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/sshclient"
	"vpn-router/internal/system"
)

type Finding struct {
	Title  string
	Detail string
}

func Collect(ctx context.Context, runner shell.Runner, paths config.Paths) []Finding {
	var findings []Finding
	findings = appendFileFinding(findings, paths.SingBoxLocalConf, "Локальный sing-box config")
	findings = appendFileFinding(findings, paths.HostapdConf, "hostapd config")
	findings = appendFileFinding(findings, paths.DnsmasqConf, "dnsmasq config vpn-router")
	findings = appendFileFinding(findings, paths.NftablesConf, "nftables config vpn-router")
	findings = appendFileFinding(findings, paths.SysctlConf, "sysctl config vpn-router")

	if out, err := runner.Output(ctx, "ip", "link", "show", "tun0"); err == nil && strings.TrimSpace(out) != "" {
		findings = append(findings, Finding{
			Title:  "TUN interface tun0 уже существует",
			Detail: "Локальный sing-box использует interface_name=tun0; существующий интерфейс может конфликтовать.",
		})
	}
	if out, err := runner.Output(ctx, "nft", "list", "table", "inet", "vpn_router"); err == nil && strings.TrimSpace(out) != "" {
		findings = append(findings, Finding{
			Title:  "nftables table inet vpn_router уже существует",
			Detail: "При применении режима таблица будет уничтожена и создана заново из шаблона vpn-router.",
		})
	}
	for _, service := range []string{singbox.Service, "hostapd", "dnsmasq", "nftables"} {
		if status := system.ServiceStatus(ctx, runner, service); status == "active" {
			findings = append(findings, Finding{
				Title:  fmt.Sprintf("Служба %s уже active", service),
				Detail: "vpn-router будет управлять этой службой и может перезапустить её со своими конфигами.",
			})
		}
	}
	return findings
}

func CollectRemote(ctx context.Context, ssh sshclient.Client, target sshclient.Target, label string) []Finding {
	var findings []Finding
	if out, err := ssh.Run(ctx, target, "test -f /etc/sing-box/config.json && echo exists || true"); err == nil && strings.TrimSpace(out) == "exists" {
		findings = append(findings, Finding{
			Title:  fmt.Sprintf("%s: /etc/sing-box/config.json уже существует", label),
			Detail: "vpn-router сделает backup в /etc/sing-box/backups и заменит активный sing-box config.",
		})
	}
	if out, err := ssh.Run(ctx, target, "systemctl is-active sing-box 2>/dev/null || true"); err == nil && strings.TrimSpace(out) == "active" {
		findings = append(findings, Finding{
			Title:  fmt.Sprintf("%s: sing-box.service уже active", label),
			Detail: "vpn-router будет управлять этой службой и перезапустит её со своим config.",
		})
	}
	return findings
}

func Format(findings []Finding) string {
	if len(findings) == 0 {
		return "Конфликтующие существующие настройки не найдены."
	}
	var b strings.Builder
	fmt.Fprintln(&b, "Найдены существующие настройки, которые могут конфликтовать:")
	for _, finding := range findings {
		fmt.Fprintf(&b, "- %s\n  %s\n", finding.Title, finding.Detail)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Если продолжить, vpn-router сделает backup управляемых файлов и перезапишет активные настройки своими конфигами.")
	fmt.Fprintln(&b, "Для продолжения введите да. Для отмены введите нет.")
	return b.String()
}

func PrepareOverwrite(ctx context.Context, runner shell.Runner) error {
	_ = runner.Run(ctx, "systemctl", "stop", singbox.Service)
	_ = runner.Run(ctx, "ip", "link", "delete", "tun0")
	return nil
}

func appendFileFinding(findings []Finding, path string, title string) []Finding {
	if path == "" {
		return findings
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return findings
	}
	if err != nil {
		findings = append(findings, Finding{
			Title:  title,
			Detail: fmt.Sprintf("%s существует, но не удалось прочитать metadata: %v", path, err),
		})
		return findings
	}
	if info.IsDir() {
		return findings
	}
	findings = append(findings, Finding{
		Title:  title,
		Detail: fmt.Sprintf("%s уже существует и будет заменён конфигом vpn-router.", path),
	})
	return findings
}
