package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"vpn-router/internal/bootstrap"
	"vpn-router/internal/config"
	"vpn-router/internal/diagnostics"
	"vpn-router/internal/foreign"
	"vpn-router/internal/modes"
	"vpn-router/internal/restore"
	"vpn-router/internal/ru"
	"vpn-router/internal/rules"
	"vpn-router/internal/setup"
	"vpn-router/internal/shell"
	"vpn-router/internal/sshclient"
	"vpn-router/internal/summary"
	"vpn-router/internal/system"
	"vpn-router/internal/tui"
	"vpn-router/internal/wifi"
)

type Options struct {
	Version string
	Paths   config.Paths
	Runner  shell.Runner
}

func NewRoot(opts Options) *cobra.Command {
	if opts.Runner == nil {
		opts.Runner = shell.RealRunner{}
	}
	ctx := context.Background()
	root := &cobra.Command{
		Use:           "vpn-router",
		Short:         "VPN Router Manager",
		Version:       opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shouldRunInitialSetup(opts.Paths) {
				fmt.Fprintln(cmd.OutOrStdout(), "Первый запуск: запускаю подготовку системы и настройку.")
				if err := (bootstrap.Service{Paths: opts.Paths, Runner: opts.Runner, Stdout: cmd.OutOrStdout()}).Run(ctx); err != nil {
					return err
				}
				return (setup.Service{Paths: opts.Paths, Runner: opts.Runner, In: os.Stdin, Out: cmd.OutOrStdout()}).Run(ctx)
			}
			return tui.Run(ctx, opts.Paths, opts.Runner)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	commands := []*cobra.Command{
		bootstrapCmd(ctx, opts),
		setupCmd(ctx, opts),
		vpnCmd(ctx, opts),
		directCmd(ctx, opts),
		statusCmd(ctx, opts),
		logsCmd(ctx, opts),
		infoCmd(opts),
		qrCmd(ctx, opts),
		restoreNetworkCmd(ctx, opts),
	}
	commands = append(commands, directRulesCommands(ctx, opts)...)
	root.AddCommand(commands...)
	root.AddCommand(ruCmd(ctx, opts), foreignCmd(ctx, opts), wifiCmd(ctx, opts))
	return root
}

func restoreNetworkCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "restore-network",
		Short: "Откатить локальные сетевые изменения vpn-router",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (restore.Service{Paths: opts.Paths, Runner: opts.Runner, Out: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func bootstrapCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:    "bootstrap",
		Short:  "Подготовить систему и установить vpn-router",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (bootstrap.Service{Paths: opts.Paths, Runner: opts.Runner, Stdout: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func setupCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:    "setup",
		Short:  "Пошаговая настройка мини-ПК, RU и foreign-серверов",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (setup.Service{Paths: opts.Paths, Runner: opts.Runner, In: os.Stdin, Out: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func vpnCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "vpn",
		Short: "Включить VPN-режим через rule_set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := modes.EnableVPN(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "VPN-режим включён.")
			return nil
		},
	}
}

func directCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "direct",
		Short: "Отключить VPN и включить прямой интернет",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := modes.EnableDirect(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Прямой интернет включён.")
			return nil
		},
	}
}

func statusCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Показать статус системы",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := diagnostics.Collect(ctx, opts.Runner, opts.Paths)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), diagnostics.Format(status))
			return nil
		},
	}
}

func logsCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Показать локальные логи",
		RunE: func(cmd *cobra.Command, args []string) error {
			logs, err := diagnostics.Logs(ctx, opts.Runner)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), logs)
			return nil
		},
	}
}

func infoCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Показать итоговую информацию",
		RunE: func(cmd *cobra.Command, args []string) error {
			if data, err := os.ReadFile(opts.Paths.InstallSummary); err == nil {
				fmt.Fprint(cmd.OutOrStdout(), string(data))
				return nil
			}
			text, err := summary.Generate(opts.Paths)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

func qrCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "qr",
		Short: "Показать QR-код VLESS/REALITY ссылки",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(opts.Paths.ClientLink)
			if err != nil {
				return fmt.Errorf("VLESS-ссылка не найдена: сначала выполните setup")
			}
			out, err := opts.Runner.Output(ctx, "qrencode", "-t", "ANSIUTF8", strings.TrimSpace(string(data)))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func directRulesCommands(ctx context.Context, opts Options) []*cobra.Command {
	add := &cobra.Command{
		Use:   "direct-add <domain|suffix|ip|cidr> <value>",
		Short: "Добавить правило прямого доступа без VPN",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if _, err := system.BackupFile(opts.Paths.CustomDirect, opts.Paths.BackupsDir); err != nil {
				return err
			}
			value, err := rules.Add(opts.Paths.CustomDirect, args[0], args[1])
			if err != nil {
				return err
			}
			if err := rules.AfterChange(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Правило добавлено: %s\n", value)
			return nil
		},
	}
	remove := &cobra.Command{
		Use:   "direct-remove <value>",
		Short: "Удалить правило прямого доступа",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if _, err := system.BackupFile(opts.Paths.CustomDirect, opts.Paths.BackupsDir); err != nil {
				return err
			}
			removed, err := rules.Remove(opts.Paths.CustomDirect, args[0])
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("правило не найдено: %s", args[0])
			}
			if err := rules.AfterChange(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Правило удалено.")
			return nil
		},
	}
	list := &cobra.Command{
		Use:   "direct-list",
		Short: "Показать правила прямого доступа",
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := rules.Load(opts.Paths.CustomDirect)
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(set, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	edit := &cobra.Command{
		Use:   "direct-edit",
		Short: "Открыть custom-direct.json в редакторе",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if _, err := system.BackupFile(opts.Paths.CustomDirect, opts.Paths.BackupsDir); err != nil {
				return err
			}
			if err := rules.Edit(ctx, opts.Paths.CustomDirect); err != nil {
				return err
			}
			if err := rules.AfterChange(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Правила сохранены.")
			return nil
		},
	}
	return []*cobra.Command{add, remove, list, edit}
}

func ruCmd(ctx context.Context, opts Options) *cobra.Command {
	svc := ru.Service{Paths: opts.Paths, Runner: opts.Runner, SSH: sshclient.Client{Runner: opts.Runner}}
	cmd := &cobra.Command{Use: "ru", Short: "Управление RU-сервером"}
	cmd.AddCommand(&cobra.Command{
		Use:   "auto",
		Short: "Включить auto-режим RU",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Auto(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "RU auto-режим включён.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "use <foreign-name>",
		Short: "Выбрать foreign-сервер вручную",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Use(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "RU переключён на %s.\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать foreign-серверы",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printForeignList(cmd, opts.Paths)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test <foreign-name>",
		Short: "Проверить foreign-сервер",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Test(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Проверка успешна.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Показать статус RU",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := svc.Status(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Показать логи RU",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := svc.Logs(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "rollback",
		Short: "Откатить RU-конфиг",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Rollback(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Откат RU выполнен.")
			return nil
		},
	})
	return cmd
}

func foreignCmd(ctx context.Context, opts Options) *cobra.Command {
	svc := foreign.Service{Paths: opts.Paths, Runner: opts.Runner, SSH: sshclient.Client{Runner: opts.Runner}}
	cmd := &cobra.Command{Use: "foreign", Short: "Управление foreign-серверами"}
	cmd.AddCommand(&cobra.Command{
		Use:   "add",
		Short: "Добавить foreign-сервер",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := askForeignServer(cmd.OutOrStdout())
			added, err := svc.Add(ctx, server)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Foreign-сервер добавлен: %s, SNI: %s\n", added.Name, added.Reality.SNI)
			return nil
		},
	})
	remove := &cobra.Command{
		Use:   "remove <foreign-name>",
		Short: "Удалить foreign-сервер из схемы",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switchAuto, _ := cmd.Flags().GetBool("switch-auto")
			if err := svc.Remove(ctx, args[0], switchAuto); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Foreign-сервер удалён.")
			return nil
		},
	}
	remove.Flags().Bool("switch-auto", false, "переключить RU в auto перед удалением")
	cmd.AddCommand(remove)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать foreign-серверы",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printForeignList(cmd, opts.Paths)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test <foreign-name>",
		Short: "Проверить SSH-доступ к foreign-серверу",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Test(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Проверка успешна.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "cleanup <foreign-name>",
		Short: "Очистить VPS foreign-сервера",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), "Введите YES для продолжения: ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(answer) != "YES" {
				fmt.Fprintln(cmd.OutOrStdout(), "Операция отменена.")
				return nil
			}
			if err := svc.Cleanup(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Очистка завершена.")
			return nil
		},
	})
	return cmd
}

func wifiCmd(ctx context.Context, opts Options) *cobra.Command {
	cmd := &cobra.Command{Use: "wifi", Short: "Настройки Wi-Fi роутера"}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Показать настройки Wi-Fi",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(opts.Paths)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "SSID: %s\nДиапазон: %s GHz\nКанал: %d\nШирина: %d MHz\nAP interface: %s\n",
				cfg.MiniPC.SSID, cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, cfg.MiniPC.APInterface)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "scan",
		Short: "Сканировать соседние Wi-Fi сети",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(opts.Paths)
			if err != nil {
				return err
			}
			networks, err := wifi.Scan(ctx, opts.Runner, cfg.MiniPC.APInterface)
			if err != nil {
				return err
			}
			count24, count5 := 0, 0
			for _, network := range networks {
				if network.Band == "5" {
					count5++
				} else {
					count24++
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Найдено сетей:\n  2.4 GHz: %d\n  5 GHz: %d\n", count24, count5)
			return nil
		},
	})
	cmd.AddCommand(wifiSetBandCmd(ctx, opts))
	cmd.AddCommand(wifiSetChannelCmd(ctx, opts))
	cmd.AddCommand(wifiSetWidthCmd(ctx, opts))
	cmd.AddCommand(&cobra.Command{
		Use:   "auto-channel",
		Short: "Автоподбор лучшего Wi-Fi канала",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			cfg, err := config.Load(opts.Paths)
			if err != nil {
				return err
			}
			caps, err := wifi.InspectInterface(ctx, opts.Runner, cfg.MiniPC.APInterface)
			if err != nil {
				return err
			}
			networks, _ := wifi.Scan(ctx, opts.Runner, cfg.MiniPC.APInterface)
			rec := wifi.Recommend(caps, networks)
			fmt.Fprintf(cmd.OutOrStdout(), "Рекомендация:\n  Диапазон: %s GHz\n  Канал: %d\n  Ширина: %d MHz\n\nПрименить настройки? [y/N] ", rec.Band, rec.Channel, rec.ChannelWidth)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Операция отменена.")
				return nil
			}
			if err := wifi.ConfigureBand(&cfg, rec.Band); err != nil {
				return err
			}
			cfg.WiFi.Channel = rec.Channel
			cfg.WiFi.ChannelWidth = rec.ChannelWidth
			if err := config.Save(opts.Paths, cfg); err != nil {
				return err
			}
			if err := wifi.ApplyAccessPoint(ctx, opts.Runner, opts.Paths, cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wi-Fi настройки применены.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Перезапустить Wi-Fi точку доступа",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if err := wifi.Restart(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wi-Fi точка доступа перезапущена.")
			return nil
		},
	})
	return cmd
}

func wifiSetBandCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "set-band <2.4|5>",
		Short: "Изменить диапазон Wi-Fi",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateWiFi(ctx, opts, cmd, func(cfg *config.Config) error {
				caps, err := wifi.InspectInterface(ctx, opts.Runner, cfg.MiniPC.APInterface)
				if err != nil {
					return err
				}
				if args[0] == "5" && !caps.Supports5 {
					return fmt.Errorf("выбранный AP-адаптер %s не поддерживает 5 GHz; доступно: %s", cfg.MiniPC.APInterface, wifi.FormatCapabilities(caps))
				}
				return wifi.ConfigureBand(cfg, args[0])
			})
		},
	}
}

func wifiSetChannelCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "set-channel <channel>",
		Short: "Изменить канал Wi-Fi",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			channel, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("канал должен быть числом")
			}
			return updateWiFi(ctx, opts, cmd, func(cfg *config.Config) error {
				cfg.WiFi.Channel = channel
				return nil
			})
		},
	}
}

func wifiSetWidthCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "set-width <20|40|80>",
		Short: "Изменить ширину канала Wi-Fi",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			width, err := strconv.Atoi(args[0])
			if err != nil || (width != 20 && width != 40 && width != 80) {
				return fmt.Errorf("ширина канала должна быть 20, 40 или 80")
			}
			return updateWiFi(ctx, opts, cmd, func(cfg *config.Config) error {
				cfg.WiFi.ChannelWidth = width
				return nil
			})
		},
	}
}

func updateWiFi(ctx context.Context, opts Options, cmd *cobra.Command, mutate func(*config.Config) error) error {
	if err := system.RequireRoot(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Внимание: изменение настроек перезапустит Wi-Fi точку доступа.")
	fmt.Fprintln(cmd.OutOrStdout(), "Подключённые устройства временно потеряют соединение.")
	cfg, err := config.Load(opts.Paths)
	if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	if err := config.Save(opts.Paths, cfg); err != nil {
		return err
	}
	if err := wifi.ApplyAccessPoint(ctx, opts.Runner, opts.Paths, cfg); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Wi-Fi настройки применены.")
	return nil
}

func askForeignServer(out io.Writer) config.ForeignServer {
	reader := bufio.NewReader(os.Stdin)
	return config.ForeignServer{
		Name:    prompt(reader, out, "Имя сервера", "de-1"),
		IP:      prompt(reader, out, "IP", ""),
		SSHUser: prompt(reader, out, "SSH user", "root"),
		SSHPort: promptInt(reader, out, "SSH port", 22),
		VPNPort: promptInt(reader, out, "VPN port", 443),
	}
}

func shouldRunInitialSetup(paths config.Paths) bool {
	cfg, err := config.Load(paths)
	if err != nil {
		return false
	}
	if _, err := os.Stat(paths.Config); errors.Is(err, os.ErrNotExist) {
		return true
	}
	servers, err := config.LoadForeign(paths)
	if err != nil {
		return false
	}
	return cfg.MiniPC.APInterface == "" || cfg.RUServer.IP == "" || cfg.Reality.UUID == "" || len(servers.Servers) == 0
}

func printForeignList(cmd *cobra.Command, paths config.Paths) error {
	servers, err := config.LoadForeign(paths)
	if err != nil {
		return err
	}
	if len(servers.Servers) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Foreign-серверы не добавлены.")
		return nil
	}
	for _, server := range servers.Servers {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s:%d, SSH %s:%d, SNI %s\n", server.Name, server.IP, server.VPNPort, server.SSHUser, server.SSHPort, server.Reality.SNI)
	}
	return nil
}

func prompt(reader *bufio.Reader, out io.Writer, label, def string) string {
	if def == "" {
		fmt.Fprintf(out, "%s: ", label)
	} else {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	}
	value, _ := reader.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return def
	}
	return value
}

func promptInt(reader *bufio.Reader, out io.Writer, label string, def int) int {
	for {
		value := prompt(reader, out, label, strconv.Itoa(def))
		n, err := strconv.Atoi(value)
		if err == nil {
			return n
		}
		fmt.Fprintln(out, "Введите число.")
	}
}
