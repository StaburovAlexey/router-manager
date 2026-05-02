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

	"router-manager/internal/bootstrap"
	"router-manager/internal/config"
	"router-manager/internal/confirm"
	"router-manager/internal/diagnostics"
	"router-manager/internal/foreign"
	"router-manager/internal/modes"
	"router-manager/internal/restore"
	"router-manager/internal/ru"
	"router-manager/internal/rules"
	"router-manager/internal/selfupdate"
	"router-manager/internal/setup"
	"router-manager/internal/shell"
	"router-manager/internal/sshclient"
	"router-manager/internal/summary"
	"router-manager/internal/system"
	"router-manager/internal/tui"
	"router-manager/internal/uninstall"
	"router-manager/internal/wifi"
)

type Options struct {
	Version      string
	Paths        config.Paths
	Runner       shell.Runner
	RunBootstrap func(context.Context, io.Writer) error
	RunSetup     func(context.Context, io.Reader, io.Writer) error
}

func NewRoot(opts Options) *cobra.Command {
	if opts.Runner == nil {
		opts.Runner = shell.RealRunner{}
	}
	if opts.RunBootstrap == nil {
		opts.RunBootstrap = func(ctx context.Context, out io.Writer) error {
			return (bootstrap.Service{Paths: opts.Paths, Runner: opts.Runner, Stdout: out}).Run(ctx)
		}
	}
	if opts.RunSetup == nil {
		opts.RunSetup = func(ctx context.Context, in io.Reader, out io.Writer) error {
			return (setup.Service{Paths: opts.Paths, Runner: opts.Runner, In: in, Out: out}).Run(ctx)
		}
	}
	ctx := context.Background()
	root := &cobra.Command{
		Use:           "router-manager",
		Short:         "Router Manager",
		Version:       opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shouldRunInitialSetup(opts.Paths) {
				if _, err := os.Stat(opts.Paths.Config); errors.Is(err, os.ErrNotExist) {
					fmt.Fprintln(cmd.OutOrStdout(), "Первый запуск: подготовлю систему и открою мастер настройки.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Настройка ещё не завершена: продолжу мастер с уже сохранёнными значениями.")
				}
				if err := opts.RunBootstrap(ctx, cmd.OutOrStdout()); err != nil {
					return bootstrapSetupError(err)
				}
				return opts.RunSetup(ctx, os.Stdin, cmd.OutOrStdout())
			}
			return tui.Run(ctx, opts.Paths, opts.Runner, opts.Version)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	commands := []*cobra.Command{
		bootstrapCmd(ctx, opts),
		setupCmd(ctx, opts),
		tunnelCmd(ctx, opts),
		directCmd(ctx, opts),
		statusCmd(ctx, opts),
		reportCmd(ctx, opts),
		logsCmd(ctx, opts),
		infoCmd(opts),
		qrCmd(ctx, opts),
		updateCmd(ctx, opts),
		restoreNetworkCmd(ctx, opts),
		uninstallCmd(ctx, opts),
	}
	commands = append(commands, directRulesCommands(ctx, opts)...)
	root.AddCommand(commands...)
	root.AddCommand(ruCmd(ctx, opts), foreignCmd(ctx, opts), wifiCmd(ctx, opts))
	return root
}

func bootstrapSetupError(err error) error {
	return fmt.Errorf(
		"подготовка системы не завершена. "+
			"Часть зависимостей или файлов могла уже быть создана; это нормально для повторного запуска. "+
			"Исправьте причину ошибки и снова выполните sudo router-manager. "+
			"Если нужна ручная очистка локальных данных приложения и команда router-manager уже доступна, используйте sudo router-manager uninstall --keep-deps: %w",
		err,
	)
}

func updateCmd(ctx context.Context, opts Options) *cobra.Command {
	var yes bool
	var force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Обновить приложение из последнего GitHub Release",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if !yes {
				reader := bufio.NewReader(os.Stdin)
				if !confirm.AskYesNo(reader, cmd.OutOrStdout(), "Скачать и установить последний релиз router-manager?") {
					fmt.Fprintln(cmd.OutOrStdout(), "Операция отменена.")
					return nil
				}
			}
			return (selfupdate.Service{
				Paths:          opts.Paths,
				CurrentVersion: opts.Version,
				Force:          force,
				Out:            cmd.OutOrStdout(),
			}).Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "не спрашивать подтверждение")
	cmd.Flags().BoolVar(&force, "force", false, "переустановить даже если версия совпадает")
	return cmd
}

func restoreNetworkCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "restore-network",
		Short: "Откатить локальные сетевые изменения router-manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (restore.Service{Paths: opts.Paths, Runner: opts.Runner, Out: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func uninstallCmd(ctx context.Context, opts Options) *cobra.Command {
	var yes bool
	var keepDeps bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Полностью удалить router-manager с устройства",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if !yes {
				reader := bufio.NewReader(os.Stdin)
				fmt.Fprintln(cmd.OutOrStdout(), "Будут удалены локальные настройки, данные, бинарник router-manager и прикладные зависимости.")
				fmt.Fprintln(cmd.OutOrStdout(), "Удалённые серверы не изменяются.")
				if !confirm.AskYesNo(reader, cmd.OutOrStdout(), "Полностью удалить router-manager с этого устройства?") {
					fmt.Fprintln(cmd.OutOrStdout(), "Операция отменена.")
					return nil
				}
			}
			return (uninstall.Service{
				Paths:              opts.Paths,
				Runner:             opts.Runner,
				Out:                cmd.OutOrStdout(),
				RemoveDependencies: !keepDeps,
			}).Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "не спрашивать подтверждение")
	cmd.Flags().BoolVar(&keepDeps, "keep-deps", false, "не удалять зависимости")
	return cmd
}

func bootstrapCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:    "bootstrap",
		Short:  "Подготовить систему и установить router-manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (bootstrap.Service{Paths: opts.Paths, Runner: opts.Runner, Stdout: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func setupCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:    "setup",
		Short:  "Пошаговая настройка устройства и серверов",
		RunE: func(cmd *cobra.Command, args []string) error {
			return (setup.Service{Paths: opts.Paths, Runner: opts.Runner, In: os.Stdin, Out: cmd.OutOrStdout()}).Run(ctx)
		},
	}
}

func tunnelCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "tunnel",
		Short: "Включить маршрут через сервер",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := modes.EnableTunnel(ctx, opts.Runner, opts.Paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Режим маршрутизации включён.")
			return nil
		},
	}
}

func directCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "direct",
		Short: "Отключить маршрут через сервер и включить прямой интернет",
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

func reportCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Собрать отчёт диагностики без секретов",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := diagnostics.Report(ctx, opts.Runner, opts.Paths)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
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
		Short: "Показать QR-код для подключения",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(opts.Paths.ClientLink)
			if err != nil {
				return fmt.Errorf("QR-ссылка не найдена: сначала запустите sudo router-manager")
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
		Use:   "direct-add <site|domain|suffix|ip|cidr> <value>",
		Short: "Добавить сайт или адрес прямого доступа",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			if _, err := system.BackupFile(opts.Paths.CustomDirect, opts.Paths.BackupsDir); err != nil {
				return err
			}
			value := ""
			var err error
			switch args[0] {
			case "site", "auto":
				_, value, err = rules.AddAuto(opts.Paths.CustomDirect, args[1])
			default:
				value, err = rules.Add(opts.Paths.CustomDirect, args[0], args[1])
			}
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
		Short: "Показать сайты и адреса прямого доступа",
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := rules.Load(opts.Paths.CustomDirect)
			if err != nil {
				return err
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			if !asJSON {
				fmt.Fprintln(cmd.OutOrStdout(), rules.FormatHuman(set))
				return nil
			}
			data, _ := json.MarshalIndent(set, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	list.Flags().Bool("json", false, "показать исходный JSON rule-set")
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
	cmd := &cobra.Command{Use: "ru", Short: "Управление входным сервером"}
	cmd.AddCommand(&cobra.Command{
		Use:   "auto",
		Short: "Автоматически выбирать выходной сервер",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Auto(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Автоматический выбор выходного сервера включён.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "use <server-name>",
		Short: "Выбрать выходной сервер вручную",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Use(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Входной сервер переключён на выходной сервер %s.\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать выходные серверы",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printForeignList(cmd, opts.Paths)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test <server-name>",
		Short: "Проверить выходной сервер",
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
		Short: "Показать статус входного сервера",
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
		Short: "Показать логи входного сервера",
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
		Short: "Откатить конфиг входного сервера",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := svc.Rollback(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Откат входного сервера выполнен.")
			return nil
		},
	})
	return cmd
}

func foreignCmd(ctx context.Context, opts Options) *cobra.Command {
	svc := foreign.Service{Paths: opts.Paths, Runner: opts.Runner, SSH: sshclient.Client{Runner: opts.Runner}}
	cmd := &cobra.Command{Use: "foreign", Short: "Управление выходными серверами"}
	cmd.AddCommand(&cobra.Command{
		Use:   "add",
		Short: "Добавить выходной сервер",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := askForeignServer(cmd.OutOrStdout())
			added, err := svc.Add(ctx, server)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Выходной сервер добавлен: %s, SNI: %s\n", added.Name, added.Reality.SNI)
			return nil
		},
	})
	remove := &cobra.Command{
		Use:   "remove <server-name>",
		Short: "Удалить выходной сервер из схемы",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switchAuto, _ := cmd.Flags().GetBool("switch-auto")
			if err := svc.Remove(ctx, args[0], switchAuto); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Выходной сервер удалён.")
			return nil
		},
	}
	remove.Flags().Bool("switch-auto", false, "переключить входной сервер в auto перед удалением")
	cmd.AddCommand(remove)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать выходные серверы",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printForeignList(cmd, opts.Paths)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test <server-name>",
		Short: "Проверить SSH-доступ к выходному серверу",
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
		Use:   "cleanup <server-name>",
		Short: "Очистить VPS выходного сервера",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)
			if !confirm.AskYesNo(reader, cmd.OutOrStdout(), "Очистить VPS выходного сервера?") {
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
			fmt.Fprintf(cmd.OutOrStdout(), "SSID: %s\nДиапазон: %s GHz\nКанал: %d\nШирина: %d MHz\nWi-Fi адаптер для раздачи: %s\n",
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
	cmd.AddCommand(wifiAdaptersCmd(ctx, opts))
	cmd.AddCommand(wifiSetAdapterCmd(ctx, opts))
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
			fmt.Fprintf(cmd.OutOrStdout(), "Рекомендация:\n  Диапазон: %s GHz\n  Канал: %d\n  Ширина: %d MHz\n\n", rec.Band, rec.Channel, rec.ChannelWidth)
			reader := bufio.NewReader(os.Stdin)
			if !confirm.AskYesNo(reader, cmd.OutOrStdout(), "Применить настройки?") {
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

func wifiAdaptersCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "adapters",
		Short: "Показать Wi-Fi адаптеры для раздачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.Load(opts.Paths)
			infos, err := wifi.InterfaceInfos(ctx, opts.Runner)
			if err != nil {
				return err
			}
			if len(infos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Wi-Fi адаптеры не найдены.")
				return nil
			}
			for _, info := range infos {
				current := ""
				if info.Name == cfg.MiniPC.APInterface {
					current = " (текущий)"
				}
				caps, rec, err := wifi.RecommendedSettings(ctx, opts.Runner, info.Name)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s%s: не подходит: %v\n", info.Name, current, err)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s: %s; рекомендация: %s GHz, канал %d, ширина %d MHz\n",
					info.Name, current, wifi.FormatCapabilities(caps), rec.Band, rec.Channel, rec.ChannelWidth)
			}
			return nil
		},
	}
}

func wifiSetAdapterCmd(ctx context.Context, opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "set-adapter <interface>",
		Short: "Сменить Wi-Fi адаптер для раздачи и подобрать настройки",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := system.RequireRoot(); err != nil {
				return err
			}
			result, err := wifi.SwitchAccessPoint(ctx, opts.Runner, opts.Paths, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wi-Fi адаптер для раздачи изменён: %s\n", result.NewInterface)
			if result.OldInterface != "" && result.OldInterface != result.NewInterface {
				fmt.Fprintf(cmd.OutOrStdout(), "Старый адаптер возвращён в NetworkManager: %s\n", result.OldInterface)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Автонастройка: %s GHz, канал %d, ширина %d MHz\n", result.Recommendation.Band, result.Recommendation.Channel, result.Recommendation.ChannelWidth)
			return nil
		},
	}
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
		Name:       prompt(reader, out, "Имя выходного сервера", "out-1"),
		IP:         prompt(reader, out, "IP выходного сервера", ""),
		SSHUser:    prompt(reader, out, "SSH user выходного сервера", "root"),
		SSHPort:    promptInt(reader, out, "SSH port выходного сервера", 22),
		TunnelPort: promptInt(reader, out, "порт подключения выходного сервера", 443),
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
		fmt.Fprintln(cmd.OutOrStdout(), "Выходные серверы не добавлены.")
		return nil
	}
	for _, server := range servers.Servers {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s:%d, SSH %s:%d, SNI %s\n", server.Name, server.IP, server.TunnelPort, server.SSHUser, server.SSHPort, server.Reality.SNI)
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
