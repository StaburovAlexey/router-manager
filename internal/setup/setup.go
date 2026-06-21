package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"router-manager/internal/bootstrap"
	"router-manager/internal/config"
	"router-manager/internal/confirm"
	"router-manager/internal/foreign"
	"router-manager/internal/network"
	"router-manager/internal/nftables"
	"router-manager/internal/preflight"
	"router-manager/internal/reality"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
	"router-manager/internal/sshclient"
	"router-manager/internal/summary"
	"router-manager/internal/system"
	"router-manager/internal/wifi"
)

type Service struct {
	Paths  config.Paths
	Runner shell.Runner
	In     io.Reader
	Out    io.Writer
}

func (s Service) Run(ctx context.Context) error {
	if err := system.RequireRoot(); err != nil {
		return err
	}
	if s.In == nil {
		s.In = os.Stdin
	}
	if s.Out == nil {
		s.Out = os.Stdout
	}
	reader := bufio.NewReader(s.In)
	if err := bootstrap.SeedFiles(s.Paths); err != nil {
		return err
	}
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return err
	}

	printSetupIntro(s.Out, cfg)
	printStep(s.Out, 1, 5, "Проверка устройства")
	if err := printReadiness(ctx, s.Runner, s.Out); err != nil {
		return err
	}
	findings := preflight.Collect(ctx, s.Runner, s.Paths)
	if len(findings) > 0 {
		fmt.Fprintln(s.Out, preflight.Format(findings))
		if !confirm.AskYesNo(reader, s.Out, "Подтвердите перезапись") {
			return fmt.Errorf("настройка остановлена: перезапись существующих настроек не подтверждена")
		}
		if err := preflight.PrepareOverwrite(ctx, s.Runner); err != nil {
			return err
		}
	}

	printStep(s.Out, 2, 5, "SSH-доступ к серверам")
	fmt.Fprintln(s.Out, "Приложение может настроить SSH-доступ автоматически.")
	fmt.Fprintln(s.Out, "Для этого пароль от VPS вводится один раз, не сохраняется и используется только для добавления SSH-ключа.")

	currentWAN := describeAutoWAN(ctx, s.Runner, s.Out)
	cfg.MiniPC.WANInterface = "auto"

	printStep(s.Out, 3, 5, "Wi-Fi для раздачи")
	cfg.MiniPC.APInterface = chooseAPInterface(ctx, s.Runner, reader, s.Out, cfg.MiniPC.APInterface, currentWAN)
	if err := confirmAPInterfaceDisruption(ctx, s.Runner, reader, s.Out, cfg.MiniPC.APInterface, currentWAN); err != nil {
		return err
	}
	apCaps, err := wifi.InspectInterface(ctx, s.Runner, cfg.MiniPC.APInterface)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "Возможности выбранного Wi-Fi адаптера %s: %s\n", cfg.MiniPC.APInterface, wifi.FormatCapabilities(apCaps))

	cfg.MiniPC.SSID = ask(reader, s.Out, "SSID Wi-Fi сети", cfg.MiniPC.SSID)
	cfg.WiFi.Password = askPassword(reader, s.Out, "Пароль Wi-Fi")
	if confirm.AskYesNo(reader, s.Out, "Показать расширенные настройки сети?") {
		cfg.MiniPC.LANCIDR = ask(reader, s.Out, "LAN subnet", cfg.MiniPC.LANCIDR)
		cfg.MiniPC.LANGateway = ask(reader, s.Out, "LAN gateway", cfg.MiniPC.LANGateway)
		cfg.WiFi.Country = ask(reader, s.Out, "Regulatory domain", cfg.WiFi.Country)
	}
	if err := chooseWiFiSettings(ctx, s.Runner, &cfg, apCaps, reader, s.Out); err != nil {
		return err
	}

	printStep(s.Out, 4, 5, "Входной сервер")
	ssh := sshclient.Client{Runner: s.Runner}
	var ruTarget sshclient.Target
	for {
		cfg.RUServer.IP = askRequired(reader, s.Out, "IP входного сервера", cfg.RUServer.IP)
		cfg.RUServer.SSHUser = ask(reader, s.Out, "SSH user входного сервера", defaultString(cfg.RUServer.SSHUser, "root"))
		cfg.RUServer.SSHPort = askInt(reader, s.Out, "SSH port входного сервера", defaultInt(cfg.RUServer.SSHPort, 22))
		cfg.RUServer.TunnelPort = askInt(reader, s.Out, "порт подключения входного сервера", defaultInt(cfg.RUServer.TunnelPort, 443))

		ruTarget = sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}
		if err := ensureSSHAccess(ctx, ssh, ruTarget, "входной сервер", reader, s.Out); err != nil {
			if shouldReenterServerDetails(err, reader, s.Out, "входного сервера") {
				continue
			}
			return err
		}
		break
	}
	if findings := preflight.CollectRemote(ctx, ssh, ruTarget, "входной сервер"); len(findings) > 0 {
		fmt.Fprintln(s.Out, preflight.Format(findings))
		if !confirm.AskYesNo(reader, s.Out, "Подтвердите перезапись входного сервера") {
			return fmt.Errorf("настройка остановлена: перезапись входного сервера не подтверждена")
		}
	}
	if err := (singbox.Installer{}).EnsureRemoteInstalled(ctx, ssh, ruTarget); err != nil {
		return fmt.Errorf("не удалось установить или проверить sing-box на входном сервере: %w", err)
	}

	if err := ensureRealityRuntime(ctx, &cfg); err != nil {
		return err
	}
	ruPrivate, ruPublic, err := generateRemoteRealityKeypair(ctx, ssh, ruTarget)
	if err != nil {
		return err
	}
	cfg.Reality.PublicKey = ruPublic

	if err := config.Save(s.Paths, cfg); err != nil {
		return err
	}

	printStep(s.Out, 5, 5, "Выходные серверы и запуск")
	var addedForeign []config.ForeignServer
	for index := 0; ; index++ {
		if index == 0 {
			fmt.Fprintln(s.Out, "Добавление выходного сервера.")
		} else if !confirm.AskYesNo(reader, s.Out, "Добавить ещё один выходной сервер?") {
			break
		}
		added, err := configureForeign(ctx, s.Paths, s.Runner, ssh, reader, s.Out, cfg, ruPrivate, index)
		if err != nil {
			return err
		}
		addedForeign = append(addedForeign, added)
	}
	if len(addedForeign) == 0 {
		servers, err := config.LoadForeign(s.Paths)
		if err != nil {
			return err
		}
		if len(servers.Servers) == 0 {
			return fmt.Errorf("нужен хотя бы один выходной сервер")
		}
		if err := (foreign.Service{
			Paths:      s.Paths,
			Runner:     s.Runner,
			SSH:        ssh,
			SNI:        reality.Selector{},
			UUID:       cfg.Reality.UUID,
			PrivateKey: ruPrivate,
		}).RefreshRU(ctx, servers, ""); err != nil {
			return err
		}
	}

	if err := network.EnableIPv4Forwarding(ctx, s.Runner, s.Paths.SysctlConf); err != nil {
		return err
	}
	if err := wifi.ApplyAccessPoint(ctx, s.Runner, s.Paths, cfg); err != nil {
		return err
	}
	if err := nftables.Apply(ctx, s.Runner, s.Paths, cfg, "direct"); err != nil {
		return err
	}
	cfg.CurrentMode = "direct"
	if err := config.Save(s.Paths, cfg); err != nil {
		return err
	}

	link := reality.ClientLink(cfg.Reality.UUID, cfg.RUServer.IP, cfg.RUServer.TunnelPort, cfg.Reality.PublicKey, cfg.Reality.ShortID, reality.PreferredSNI, "router-manager-ru")
	if err := config.WriteSensitiveText(s.Paths.ClientLink, link+"\n"); err != nil {
		return err
	}
	text, err := summary.Save(s.Paths)
	if err != nil {
		return err
	}
	fmt.Fprintln(s.Out)
	for _, added := range addedForeign {
		fmt.Fprintf(s.Out, "Выходной сервер готов: %s, SNI: %s\n", added.Name, added.Reality.SNI)
	}
	fmt.Fprintln(s.Out, text)
	return nil
}

func printStep(out io.Writer, current int, total int, title string) {
	fmt.Fprintf(out, "\nШаг %d из %d: %s\n\n", current, total, title)
}

func printSetupIntro(out io.Writer, cfg config.Config) {
	fmt.Fprintln(out)
	if cfg.MiniPC.APInterface != "" || cfg.RUServer.IP != "" || cfg.Reality.UUID != "" {
		fmt.Fprintln(out, "Продолжаю настройку. Уже сохранённые значения будут предложены по умолчанию.")
	} else {
		fmt.Fprintln(out, "Мастер настройки подготовит мини-ПК как Wi-Fi роутер.")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Перед началом проверьте:")
	fmt.Fprintln(out, "- мини-ПК подключён к интернету кабелем или отдельным адаптером")
	fmt.Fprintln(out, "- есть Wi-Fi адаптер для раздачи сети")
	fmt.Fprintln(out, "- подготовлены IP, SSH-пользователь и пароль или ключи для VPS")
	fmt.Fprintln(out, "- во время настройки Wi-Fi подключение к мини-ПК может временно пропасть")
	fmt.Fprintln(out)
}

func printReadiness(ctx context.Context, runner shell.Runner, out io.Writer) error {
	fmt.Fprintln(out, "Проверка готовности:")
	missing := missingCommands("ip", "iw", "nft", "systemctl", "ssh", "qrencode")
	if len(missing) == 0 {
		fmt.Fprintln(out, "- системные команды: OK")
	} else {
		return fmt.Errorf("не найдены системные команды: %s. Запустите sudo router-manager, чтобы выполнить подготовку системы", strings.Join(missing, ", "))
	}
	if err := network.HasInternet(ctx, runner); err != nil {
		fmt.Fprintln(out, "- интернет на мини-ПК: ошибка")
		return fmt.Errorf("на мини-ПК нет внешнего интернета: %w", err)
	}
	fmt.Fprintln(out, "- интернет на мини-ПК: OK")
	if infos, err := wifi.InterfaceInfos(ctx, runner); err != nil {
		fmt.Fprintf(out, "- Wi-Fi адаптеры: не удалось проверить (%v)\n", err)
	} else if len(infos) == 0 {
		fmt.Fprintln(out, "- Wi-Fi адаптеры: не найдены")
	} else {
		fmt.Fprintln(out, "- Wi-Fi адаптеры: найдены, выбор будет на следующем шаге")
	}
	return nil
}

func missingCommands(names ...string) []string {
	var missing []string
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

func ensureRealityRuntime(ctx context.Context, cfg *config.Config) error {
	var err error
	if cfg.Reality.UUID == "" {
		cfg.Reality.UUID, err = reality.NewUUID()
		if err != nil {
			return err
		}
	}
	if cfg.Reality.SNI == "" {
		cfg.Reality.SNI, err = (reality.Selector{}).Select(ctx)
		if err != nil {
			return err
		}
	}
	if cfg.Reality.ShortID == "" {
		cfg.Reality.ShortID, err = reality.RandomHex(4)
		if err != nil {
			return err
		}
	}
	return nil
}

func configureForeign(ctx context.Context, paths config.Paths, runner shell.Runner, ssh sshclient.Client, reader *bufio.Reader, out io.Writer, cfg config.Config, ruPrivate string, index int) (config.ForeignServer, error) {
	defaultName := "out-1"
	if index > 0 {
		defaultName = fmt.Sprintf("out-%d", index+1)
	}
	for {
		foreignServer := config.ForeignServer{
			Name:       ask(reader, out, "Имя выходного сервера", defaultName),
			IP:         askRequired(reader, out, "IP выходного сервера", ""),
			SSHUser:    ask(reader, out, "SSH user выходного сервера", "root"),
			SSHPort:    askInt(reader, out, "SSH port выходного сервера", 22),
			TunnelPort: askInt(reader, out, "порт подключения выходного сервера", 443),
		}
		foreignAction, existingForeign, err := resolveForeignConflict(paths, &foreignServer, reader, out)
		if err != nil {
			return foreignServer, err
		}
		added := foreignServer
		if foreignAction == "use" {
			added = existingForeign
			foreignServer = existingForeign
		}
		foreignTarget := sshclient.Target{User: foreignServer.SSHUser, IP: foreignServer.IP, Port: foreignServer.SSHPort}
		if err := ensureSSHAccess(ctx, ssh, foreignTarget, "выходной сервер", reader, out); err != nil {
			if shouldReenterServerDetails(err, reader, out, "выходного сервера") {
				continue
			}
			return foreignServer, err
		}
		if findings := preflight.CollectRemote(ctx, ssh, foreignTarget, "выходной сервер"); len(findings) > 0 && foreignAction != "use" {
			fmt.Fprintln(out, preflight.Format(findings))
			if !confirm.AskYesNo(reader, out, "Подтвердите перезапись выходного сервера") {
				return foreignServer, fmt.Errorf("настройка остановлена: перезапись выходного сервера не подтверждена")
			}
		}
		service := foreign.Service{
			Paths:      paths,
			Runner:     runner,
			SSH:        ssh,
			SNI:        reality.Selector{},
			UUID:       cfg.Reality.UUID,
			PrivateKey: ruPrivate,
		}
		if foreignAction == "replace" {
			if err := removeForeignLocal(paths, foreignServer.Name); err != nil {
				return foreignServer, err
			}
		}
		if foreignAction != "use" {
			added, err = service.Add(ctx, foreignServer)
			if err != nil {
				if shouldReenterServerDetails(err, reader, out, "выходного сервера") {
					continue
				}
				return foreignServer, err
			}
			return added, nil
		}
		servers, err := config.LoadForeign(paths)
		if err != nil {
			return added, err
		}
		if err := service.RefreshRU(ctx, servers, ""); err != nil {
			return added, err
		}
		return added, nil
	}
}

func generateRemoteRealityKeypair(ctx context.Context, ssh sshclient.Client, target sshclient.Target) (string, string, error) {
	out, err := ssh.Run(ctx, target, "sing-box generate reality-keypair")
	if err != nil {
		return "", "", fmt.Errorf("не удалось сгенерировать REALITY keypair на входном сервере: %w", err)
	}
	return parseKeypair(out)
}

func parseKeypair(out string) (string, string, error) {
	privateKey := ""
	publicKey := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PrivateKey:") {
			privateKey = strings.TrimSpace(strings.TrimPrefix(line, "PrivateKey:"))
		}
		if strings.HasPrefix(line, "PublicKey:") {
			publicKey = strings.TrimSpace(strings.TrimPrefix(line, "PublicKey:"))
		}
	}
	if privateKey == "" || publicKey == "" {
		return "", "", fmt.Errorf("sing-box generate reality-keypair вернул неожиданный формат")
	}
	return privateKey, publicKey, nil
}

func waitForSSH(ctx context.Context, ssh sshclient.Client, target sshclient.Target, label string, reader *bufio.Reader, out io.Writer) error {
	for {
		if err := ssh.Check(ctx, target); err == nil {
			fmt.Fprintf(out, "SSH-доступ к %s проверен.\n", label)
			return nil
		} else {
			fmt.Fprintf(out, "\nSSH-доступ к %s не работает.\n%v\n\n", label, err)
		}
		fmt.Fprintln(out, "Исправьте SSH-доступ в другой консоли, затем вернитесь сюда.")
		if confirm.AskYesNo(reader, out, "Повторить проверку SSH?") {
			continue
		}
		return fmt.Errorf("настройка остановлена: SSH-доступ к %s не настроен", label)
	}
}

func ensureSSHAccess(ctx context.Context, ssh sshclient.Client, target sshclient.Target, label string, reader *bufio.Reader, out io.Writer) error {
	if err := ssh.Check(ctx, target); err == nil {
		fmt.Fprintf(out, "SSH-доступ к %s уже настроен.\n", label)
		return nil
	}
	fmt.Fprintf(out, "\nSSH-ключ для %s пока не работает.\n", label)
	fmt.Fprintln(out, "Можно настроить его автоматически через пароль от VPS. Пароль не сохраняется.")
	if !confirm.AskYesNo(reader, out, "Настроить SSH-доступ автоматически?") {
		printManualSSHInstructions(out, target)
		return waitForSSH(ctx, ssh, target, label, reader, out)
	}
	hostKey, err := sshclient.ScanHostKey(ctx, target)
	if err != nil {
		return hostKeyScanError{label: label, err: err}
	}
	if err := sshclient.TrustHostKey(ctx, target, hostKey); err != nil {
		return fmt.Errorf("не удалось сохранить host key %s: %w", label, err)
	}
	fmt.Fprintf(out, "Host key для %s сохранён.\n", label)
	publicKey, err := sshclient.EnsureRootKeyPair(ctx)
	if err != nil {
		return fmt.Errorf("не удалось подготовить SSH-ключ root-пользователя мини-ПК: %w", err)
	}
	password := askSecret(reader, out, "SSH-пароль "+label)
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("настройка остановлена: SSH-пароль не введён")
	}
	if err := sshclient.InstallPublicKeyWithPassword(ctx, target, password, publicKey); err != nil {
		fmt.Fprintf(out, "\nАвтоматическая настройка SSH-ключа не удалась.\n%v\n\n", err)
		printManualSSHInstructions(out, target)
		return waitForSSH(ctx, ssh, target, label, reader, out)
	}
	return waitForSSH(ctx, ssh, target, label, reader, out)
}

type hostKeyScanError struct {
	label string
	err   error
}

func (e hostKeyScanError) Error() string {
	return fmt.Sprintf("не удалось получить host key %s: %v", e.label, e.err)
}

func (e hostKeyScanError) Unwrap() error {
	return e.err
}

func shouldReenterServerDetails(err error, reader *bufio.Reader, out io.Writer, label string) bool {
	var scanErr hostKeyScanError
	if errors.As(err, &scanErr) {
		fmt.Fprintf(out, "\nНе удалось проверить SSH host key %s. Обычно это неверный IP, SSH-порт или недоступный сервер.\n", label)
		return confirm.AskYesNo(reader, out, "Ввести данные "+label+" заново?")
	}
	var portErr singbox.RemoteListenPortBusyError
	if errors.As(err, &portErr) {
		fmt.Fprintf(out, "\nПорт подключения %d уже занят для %s.\n", portErr.Port, label)
		fmt.Fprintln(out, "Выберите другой порт подключения или освободите порт вручную, если старый сервис больше не нужен.")
		return confirm.AskYesNo(reader, out, "Ввести данные "+label+" заново?")
	}
	return false
}

func printManualSSHInstructions(out io.Writer, target sshclient.Target) {
	if target.Port == 0 {
		target.Port = 22
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Ручной способ, если парольный SSH на VPS отключён:")
	fmt.Fprintln(out, "  sudo mkdir -p /root/.ssh")
	fmt.Fprintln(out, `  sudo ssh-keygen -t ed25519 -C "router-manager" -f /root/.ssh/id_ed25519`)
	fmt.Fprintf(out, "  sudo ssh-keyscan -H -p %d %s | sudo tee -a /root/.ssh/known_hosts >/dev/null\n", target.Port, target.IP)
	fmt.Fprintf(out, "  sudo ssh-copy-id -i /root/.ssh/id_ed25519.pub -p %d %s\n", target.Port, target.Addr())
	fmt.Fprintln(out)
}

func resolveForeignConflict(paths config.Paths, server *config.ForeignServer, reader *bufio.Reader, out io.Writer) (string, config.ForeignServer, error) {
	for {
		existing, found, err := findForeignLocal(paths, server.Name)
		if err != nil || !found {
			return "add", config.ForeignServer{}, err
		}
		fmt.Fprintf(out, "\nВыходной сервер с именем %q уже есть в конфиге.\n", server.Name)
		fmt.Fprintf(out, "  сохранённый: %s:%d, SSH %s:%d, SNI %s\n", existing.IP, existing.TunnelPort, existing.SSHUser, existing.SSHPort, showValue(existing.Reality.SNI))
		fmt.Fprintf(out, "  введённый:   %s:%d, SSH %s:%d\n", server.IP, server.TunnelPort, server.SSHUser, server.SSHPort)
		fmt.Fprintln(out, "Выберите действие:")
		fmt.Fprintln(out, "  1) использовать сохранённый выходной сервер и продолжить")
		fmt.Fprintln(out, "  2) заменить сохранённый сервер введёнными данными")
		fmt.Fprintln(out, "  3) ввести другое имя")
		answer := ask(reader, out, "Действие: 1/2/3", "1")
		switch answer {
		case "1", "use":
			return "use", existing, nil
		case "2", "replace":
			return "replace", existing, nil
		case "3", "rename":
			server.Name = ask(reader, out, "Имя выходного сервера", server.Name+"-2")
		default:
			fmt.Fprintln(out, "Введите 1, 2 или 3.")
		}
	}
}

func findForeignLocal(paths config.Paths, name string) (config.ForeignServer, bool, error) {
	servers, err := config.LoadForeign(paths)
	if err != nil {
		return config.ForeignServer{}, false, err
	}
	for _, server := range servers.Servers {
		if server.Name == name {
			return server, true, nil
		}
	}
	return config.ForeignServer{}, false, nil
}

func removeForeignLocal(paths config.Paths, name string) error {
	servers, err := config.LoadForeign(paths)
	if err != nil {
		return err
	}
	filtered := servers.Servers[:0]
	for _, server := range servers.Servers {
		if server.Name != name {
			filtered = append(filtered, server)
		}
	}
	servers.Servers = filtered
	return config.SaveForeign(paths, servers)
}

func showValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "не настроено"
	}
	return value
}

func chooseWiFiSettings(ctx context.Context, runner shell.Runner, cfg *config.Config, caps wifi.Capabilities, reader *bufio.Reader, out io.Writer) error {
	mode := chooseWiFiSetupMode(reader, out)
	if mode == "auto" {
		return chooseWiFiSettingsAuto(ctx, runner, cfg, caps, out)
	}
	return chooseWiFiSettingsManual(cfg, caps, reader, out)
}

func chooseWiFiSetupMode(reader *bufio.Reader, out io.Writer) string {
	for {
		fmt.Fprintln(out, "Режим настройки Wi-Fi:")
		fmt.Fprintln(out, "  1) auto - подобрать диапазон, канал и ширину автоматически")
		fmt.Fprintln(out, "  2) manual - выбрать параметры вручную")
		answer := strings.ToLower(ask(reader, out, "Режим Wi-Fi: auto/manual", "auto"))
		switch answer {
		case "1", "auto", "a", "авто", "автоматически":
			return "auto"
		case "2", "manual", "m", "ручной", "вручную":
			return "manual"
		default:
			fmt.Fprintln(out, "Введите auto или manual.")
		}
	}
}

func chooseWiFiSettingsAuto(ctx context.Context, runner shell.Runner, cfg *config.Config, caps wifi.Capabilities, out io.Writer) error {
	networks, _ := wifi.Scan(ctx, runner, cfg.MiniPC.APInterface)
	rec := wifi.Recommend(caps, networks)
	if err := wifi.ValidateSettings(rec.Band, rec.Channel, rec.ChannelWidth, caps); err != nil {
		rec = fallbackRecommendation(caps)
	}
	wifi.ApplyRecommendation(cfg, rec)
	if err := wifi.ValidateSettings(cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, caps); err != nil {
		return err
	}
	fmt.Fprintf(out, "Автонастройка Wi-Fi: %s GHz, канал %d, ширина %d MHz\n", cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth)
	return nil
}

func chooseWiFiSettingsManual(cfg *config.Config, caps wifi.Capabilities, reader *bufio.Reader, out io.Writer) error {
	band, err := chooseSupportedBand(reader, out, cfg.WiFi.Band, caps)
	if err != nil {
		return err
	}
	if err := wifi.ConfigureBand(cfg, band); err != nil {
		return err
	}
	channel, err := askSupportedChannel(reader, out, cfg.WiFi.Band, cfg.WiFi.Channel, caps)
	if err != nil {
		return err
	}
	width, err := askSupportedWidth(reader, out, cfg.WiFi.ChannelWidth, caps)
	if err != nil {
		return err
	}
	cfg.WiFi.Channel = channel
	cfg.WiFi.ChannelWidth = width
	return wifi.ValidateSettings(cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, caps)
}

func chooseSupportedBand(reader *bufio.Reader, out io.Writer, def string, caps wifi.Capabilities) (string, error) {
	bands := supportedBands(caps)
	if len(bands) == 0 {
		return "", fmt.Errorf("выбранный Wi-Fi адаптер не показывает доступные AP-диапазоны")
	}
	if len(bands) == 1 {
		fmt.Fprintf(out, "Доступен только диапазон Wi-Fi: %s GHz\n", bands[0])
		return bands[0], nil
	}
	if !containsString(bands, def) {
		def = bands[0]
	}
	for {
		band := ask(reader, out, "Диапазон Wi-Fi (доступно: "+strings.Join(bands, " или ")+")", def)
		if containsString(bands, band) {
			return band, nil
		}
		fmt.Fprintf(out, "Введите один из доступных диапазонов: %s.\n", strings.Join(bands, ", "))
	}
}

func supportedBands(caps wifi.Capabilities) []string {
	var bands []string
	if caps.Supports24 && len(caps.Channels24) > 0 {
		bands = append(bands, "2.4")
	}
	if caps.Supports5 && len(caps.Channels5) > 0 {
		bands = append(bands, "5")
	}
	return bands
}

func fallbackRecommendation(caps wifi.Capabilities) wifi.Recommendation {
	if caps.Supports5 && len(caps.Channels5) > 0 {
		return wifi.Recommendation{Band: "5", Channel: preferredChannel(caps.Channels5, []int{36, 40, 44, 48}), ChannelWidth: bestManualWidth(caps, 80)}
	}
	return wifi.Recommendation{Band: "2.4", Channel: preferredChannel(caps.Channels24, []int{6, 1, 11}), ChannelWidth: bestManualWidth(caps, 40)}
}

func preferredChannel(allowed []int, preferred []int) int {
	for _, channel := range preferred {
		if containsInt(allowed, channel) {
			return channel
		}
	}
	if len(allowed) > 0 {
		return allowed[0]
	}
	return 0
}

func bestManualWidth(caps wifi.Capabilities, wanted int) int {
	if containsInt(caps.Widths, wanted) {
		return wanted
	}
	for _, fallback := range []int{40, 20} {
		if containsInt(caps.Widths, fallback) {
			return fallback
		}
	}
	return 20
}

func askSupportedChannel(reader *bufio.Reader, out io.Writer, band string, def int, caps wifi.Capabilities) (int, error) {
	allowed := caps.Channels24
	if band == "5" {
		allowed = caps.Channels5
	}
	if len(allowed) == 0 {
		return 0, fmt.Errorf("для диапазона %s GHz нет доступных AP-каналов", band)
	}
	if len(allowed) == 1 {
		fmt.Fprintf(out, "Доступен только канал Wi-Fi: %d\n", allowed[0])
		return allowed[0], nil
	}
	if !containsInt(allowed, def) && len(allowed) > 0 {
		def = allowed[0]
		if band == "2.4" && containsInt(allowed, 6) {
			def = 6
		}
		if band == "5" {
			for _, channel := range []int{36, 40, 44, 48} {
				if containsInt(allowed, channel) {
					def = channel
					break
				}
			}
		}
	}
	for {
		channel := askInt(reader, out, "Канал Wi-Fi (доступно: "+wifi.FormatInts(allowed)+")", def)
		if containsInt(allowed, channel) {
			return channel, nil
		}
		fmt.Fprintf(out, "Канал %d не поддерживается выбранным адаптером. Доступно: %s\n", channel, wifi.FormatInts(allowed))
	}
}

func askSupportedWidth(reader *bufio.Reader, out io.Writer, def int, caps wifi.Capabilities) (int, error) {
	if len(caps.Widths) == 0 {
		return 0, fmt.Errorf("выбранный Wi-Fi адаптер не показывает доступные ширины канала")
	}
	if len(caps.Widths) == 1 {
		fmt.Fprintf(out, "Доступна только ширина канала: %d MHz\n", caps.Widths[0])
		return caps.Widths[0], nil
	}
	if !containsInt(caps.Widths, def) && len(caps.Widths) > 0 {
		def = caps.Widths[0]
	}
	for {
		width := askInt(reader, out, "Ширина канала MHz (доступно: "+wifi.FormatInts(caps.Widths)+")", def)
		if containsInt(caps.Widths, width) {
			return width, nil
		}
		fmt.Fprintf(out, "Ширина %d MHz не поддерживается выбранным адаптером. Доступно: %s\n", width, wifi.FormatInts(caps.Widths))
	}
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func containsInt(values []int, value int) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func ask(reader *bufio.Reader, out io.Writer, label, def string) string {
	if def == "" {
		fmt.Fprintf(out, "%s: ", label)
	} else {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	}
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return def
	}
	return text
}

func askRequired(reader *bufio.Reader, out io.Writer, label, def string) string {
	for {
		value := ask(reader, out, label, def)
		if strings.TrimSpace(value) != "" {
			return value
		}
		fmt.Fprintf(out, "%s не может быть пустым.\n", label)
	}
}

func askSecret(reader *bufio.Reader, out io.Writer, label string) string {
	if term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintf(out, "%s: ", label)
		data, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(out)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ask(reader, out, label, "")
}

func chooseWANInterface(ctx context.Context, runner shell.Runner, reader *bufio.Reader, out io.Writer, current string) string {
	interfaces, err := network.Interfaces(ctx, runner)
	if err != nil || len(interfaces) == 0 {
		if wan, wanErr := network.DefaultWANInterface(ctx, runner); wanErr == nil {
			return ask(reader, out, "Интерфейс входящего интернета", defaultString(current, wan))
		}
		return ask(reader, out, "Интерфейс входящего интернета", current)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Выберите интерфейс входящего интернета (WAN).")
	fmt.Fprintln(out, "Обычно это Ethernet-порт, куда подключён кабель с интернетом.")
	defaultName := current
	for i, iface := range interfaces {
		if defaultName == "" && iface.IsDefault {
			defaultName = iface.Name
		}
		marker := ""
		if iface.IsDefault {
			marker = " рекомендуется, default route"
		}
		ip := "без IPv4"
		if len(iface.IPv4) > 0 {
			ip = strings.Join(iface.IPv4, ", ")
		}
		fmt.Fprintf(out, "  %d) %s  state=%s  ip=%s%s\n", i+1, iface.Name, iface.State, ip, marker)
	}
	return chooseByNumberOrName(reader, out, "Интерфейс входящего интернета", defaultName, interfaceNames(interfaces))
}

func describeAutoWAN(ctx context.Context, runner shell.Runner, out io.Writer) string {
	wan, err := network.DefaultWANInterface(ctx, runner)
	fmt.Fprintln(out)
	if err != nil || wan == "" {
		fmt.Fprintln(out, "Входящий интернет будет определяться автоматически по текущему default route при включении режима.")
		return ""
	}
	fmt.Fprintf(out, "Входящий интернет будет определяться автоматически. Сейчас default route: %s\n", wan)
	return wan
}

func chooseAPInterface(ctx context.Context, runner shell.Runner, reader *bufio.Reader, out io.Writer, current string, wan string) string {
	interfaces, err := wifi.InterfaceInfos(ctx, runner)
	if err != nil || len(interfaces) == 0 {
		return askExplicit(reader, out, "Wi-Fi адаптер для раздачи интернета", current)
	}
	netInfo := interfaceInfoMap(ctx, runner)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Выберите Wi-Fi адаптер, который будет раздавать интернет.")
	fmt.Fprintln(out, "Этот адаптер будет использоваться только как точка доступа.")
	hasUsable := false
	for i, iface := range interfaces {
		info := netInfo[iface.Name]
		if !isP2PType(iface.Type) {
			hasUsable = true
		}
		note := ""
		if iface.Name == current {
			note += " текущий"
		}
		if iface.Name == wan {
			note += " не рекомендуется: выбран как WAN"
		}
		if interfaceHasIPv4(info) {
			note += " ВНИМАНИЕ: сейчас имеет IP " + strings.Join(info.IPv4, ", ")
		}
		if isP2PType(iface.Type) {
			note += " не подходит: P2P-device не используется hostapd"
		} else if iface.Type != "" {
			note += " type=" + iface.Type
		}
		fmt.Fprintf(out, "  %d) %s%s\n", i+1, iface.Name, note)
	}
	if !hasUsable {
		fmt.Fprintln(out, "В списке нет подходящего Wi-Fi интерфейса для точки доступа. P2P-device не подходит для hostapd.")
		return askExplicit(reader, out, "Wi-Fi адаптер для раздачи интернета", current)
	}
	return chooseAPByNumberOrName(reader, out, interfaces)
}

func confirmAPInterfaceDisruption(ctx context.Context, runner shell.Runner, reader *bufio.Reader, out io.Writer, apInterface string, wanInterface string) error {
	netInfo := interfaceInfoMap(ctx, runner)
	info := netInfo[apInterface]
	if apInterface == "" || (!interfaceHasIPv4(info) && apInterface != wanInterface) {
		return nil
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Внимание: интерфейс %s будет переведён в режим точки доступа.\n", apInterface)
	if apInterface == wanInterface {
		fmt.Fprintln(out, "Этот интерфейс выбран как WAN. Так делать нельзя: устройство потеряет входящий интернет.")
	}
	if interfaceHasIPv4(info) {
		fmt.Fprintf(out, "Сейчас на нём есть IP: %s\n", strings.Join(info.IPv4, ", "))
		fmt.Fprintln(out, "Если вы подключены к устройству через этот Wi-Fi интерфейс, SSH-сессия оборвётся.")
	}
	fmt.Fprintln(out, "Рекомендуется выбрать отдельный USB Wi-Fi адаптер без IP-адреса для раздачи.")
	if !confirm.AskYesNo(reader, out, "Продолжить с этим Wi-Fi адаптером?") {
		return fmt.Errorf("настройка остановлена: Wi-Fi адаптер для раздачи не подтверждён")
	}
	return nil
}

func interfaceInfoMap(ctx context.Context, runner shell.Runner) map[string]network.InterfaceInfo {
	result := map[string]network.InterfaceInfo{}
	interfaces, err := network.Interfaces(ctx, runner)
	if err != nil {
		return result
	}
	for _, iface := range interfaces {
		result[iface.Name] = iface
	}
	return result
}

func interfaceHasIPv4(info network.InterfaceInfo) bool {
	return len(info.IPv4) > 0
}

func chooseByNumberOrName(reader *bufio.Reader, out io.Writer, label string, def string, names []string) string {
	for {
		answer := ask(reader, out, label+" (номер или имя)", def)
		if answer == "" {
			return def
		}
		if n, err := strconv.Atoi(answer); err == nil {
			if n >= 1 && n <= len(names) {
				return names[n-1]
			}
			fmt.Fprintf(out, "Введите номер от 1 до %d или имя интерфейса.\n", len(names))
			continue
		}
		for _, name := range names {
			if answer == name {
				return answer
			}
		}
		fmt.Fprintln(out, "Такого интерфейса нет в списке. Введите номер или имя из списка.")
	}
}

func chooseByNumberOrNameRequired(reader *bufio.Reader, out io.Writer, label string, names []string) string {
	for {
		fmt.Fprintf(out, "%s (номер или имя): ", label)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "" {
			fmt.Fprintln(out, "Нужно выбрать значение из списка.")
			continue
		}
		if n, err := strconv.Atoi(answer); err == nil {
			if n >= 1 && n <= len(names) {
				return names[n-1]
			}
			fmt.Fprintf(out, "Введите номер от 1 до %d или имя интерфейса.\n", len(names))
			continue
		}
		for _, name := range names {
			if answer == name {
				return answer
			}
		}
		fmt.Fprintln(out, "Такого интерфейса нет в списке. Введите номер или имя из списка.")
	}
}

func askExplicit(reader *bufio.Reader, out io.Writer, label, current string) string {
	for {
		if current == "" {
			fmt.Fprintf(out, "%s: ", label)
		} else {
			fmt.Fprintf(out, "%s (текущий: %s): ", label, current)
		}
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer != "" {
			return answer
		}
		fmt.Fprintln(out, "Нужно выбрать Wi-Fi адаптер для раздачи.")
	}
}

func interfaceNames(interfaces []network.InterfaceInfo) []string {
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		names = append(names, iface.Name)
	}
	return names
}

func wifiNames(interfaces []wifi.InterfaceInfo) []string {
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		names = append(names, iface.Name)
	}
	return names
}

func chooseAPByNumberOrName(reader *bufio.Reader, out io.Writer, interfaces []wifi.InterfaceInfo) string {
	names := wifiNames(interfaces)
	for {
		answer := chooseByNumberOrNameRequired(reader, out, "Wi-Fi адаптер для раздачи", names)
		if isP2PInterface(interfaces, answer) {
			fmt.Fprintf(out, "%s является P2P-device и не подходит для точки доступа hostapd. Выберите обычный Wi-Fi интерфейс, например wlan0/wlp*/wlx* type=managed или type=AP.\n", answer)
			continue
		}
		return answer
	}
}

func isP2PInterface(interfaces []wifi.InterfaceInfo, name string) bool {
	for _, iface := range interfaces {
		if iface.Name == name {
			return isP2PType(iface.Type)
		}
	}
	return false
}

func isP2PType(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "P2P-device")
}

func askPassword(reader *bufio.Reader, out io.Writer, label string) string {
	for {
		value := ask(reader, out, label, "")
		if err := wifi.ValidatePassword(value); err == nil {
			return value
		} else {
			fmt.Fprintln(out, err)
		}
	}
}

func askInt(reader *bufio.Reader, out io.Writer, label string, def int) int {
	for {
		value := ask(reader, out, label, strconv.Itoa(def))
		n, err := strconv.Atoi(value)
		if err == nil {
			return n
		}
		fmt.Fprintln(out, "Введите число.")
	}
}

func defaultString(value, def string) string {
	if value == "" {
		return def
	}
	return value
}

func defaultInt(value, def int) int {
	if value == 0 {
		return def
	}
	return value
}
