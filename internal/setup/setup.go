package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"vpn-router/internal/bootstrap"
	"vpn-router/internal/config"
	"vpn-router/internal/confirm"
	"vpn-router/internal/foreign"
	"vpn-router/internal/network"
	"vpn-router/internal/nftables"
	"vpn-router/internal/preflight"
	"vpn-router/internal/reality"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/sshclient"
	"vpn-router/internal/summary"
	"vpn-router/internal/system"
	"vpn-router/internal/wifi"
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

	printStep(s.Out, 1, 5, "Проверка устройства")
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

	printStep(s.Out, 2, 5, "SSH-доступ к VPN-серверам")
	fmt.Fprintln(s.Out, "Для управления входным и выходными VPN-серверами нужен SSH-доступ по ключам.")
	fmt.Fprintln(s.Out, "Важно: настройка запущена через sudo, поэтому приложение проверяет SSH от root-пользователя мини-ПК.")
	fmt.Fprintln(s.Out, "Обычный `ssh root@SERVER_IP` от пользователя ubuntu не гарантирует, что `sudo ssh ...` тоже работает.")
	fmt.Fprintln(s.Out, `ssh-keygen -t ed25519 -C "vpn-router"`)
	fmt.Fprintln(s.Out, `ssh-copy-id -p 22 root@INBOUND_SERVER_IP`)
	fmt.Fprintln(s.Out, `ssh-copy-id -p 22 root@OUTBOUND_SERVER_IP`)
	fmt.Fprintln(s.Out, "Проверяйте доступ именно так:")
	fmt.Fprintln(s.Out, `sudo ssh -o BatchMode=yes -o ConnectTimeout=8 -p 22 root@INBOUND_SERVER_IP "echo ok"`)
	fmt.Fprintln(s.Out, `sudo ssh -o BatchMode=yes -o ConnectTimeout=8 -p 22 root@OUTBOUND_SERVER_IP "echo ok"`)
	fmt.Fprintln(s.Out, "Если будет Host key verification failed, добавьте host key в /root/.ssh/known_hosts:")
	fmt.Fprintln(s.Out, `sudo ssh-keyscan -H -p 22 INBOUND_SERVER_IP | sudo tee -a /root/.ssh/known_hosts >/dev/null`)
	fmt.Fprintln(s.Out, `sudo ssh-keyscan -H -p 22 OUTBOUND_SERVER_IP | sudo tee -a /root/.ssh/known_hosts >/dev/null`)
	if !confirm.AskYesNo(reader, s.Out, "SSH-ключи настроены и можно проверить доступ?") {
		return fmt.Errorf("настройка остановлена: SSH-ключи не подтверждены")
	}

	if err := network.HasInternet(ctx, s.Runner); err != nil {
		return err
	}
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
	if err := chooseWiFiSettings(&cfg, apCaps, reader, s.Out); err != nil {
		return err
	}

	printStep(s.Out, 4, 5, "Входной VPN-сервер")
	cfg.RUServer.IP = ask(reader, s.Out, "IP входного VPN-сервера", cfg.RUServer.IP)
	cfg.RUServer.SSHUser = ask(reader, s.Out, "SSH user входного сервера", defaultString(cfg.RUServer.SSHUser, "root"))
	cfg.RUServer.SSHPort = askInt(reader, s.Out, "SSH port входного сервера", defaultInt(cfg.RUServer.SSHPort, 22))
	cfg.RUServer.VPNPort = askInt(reader, s.Out, "VPN port входного сервера", defaultInt(cfg.RUServer.VPNPort, 443))

	ssh := sshclient.Client{Runner: s.Runner}
	ruTarget := sshclient.Target{User: cfg.RUServer.SSHUser, IP: cfg.RUServer.IP, Port: cfg.RUServer.SSHPort}
	if err := waitForSSH(ctx, ssh, ruTarget, "входной VPN-сервер", reader, s.Out); err != nil {
		return err
	}
	if findings := preflight.CollectRemote(ctx, ssh, ruTarget, "входной VPN-сервер"); len(findings) > 0 {
		fmt.Fprintln(s.Out, preflight.Format(findings))
		if !confirm.AskYesNo(reader, s.Out, "Подтвердите перезапись входного сервера") {
			return fmt.Errorf("настройка остановлена: перезапись входного сервера не подтверждена")
		}
	}
	if err := (singbox.Installer{}).EnsureRemoteInstalled(ctx, ssh, ruTarget); err != nil {
		return fmt.Errorf("не удалось установить или проверить sing-box на входном VPN-сервере: %w", err)
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

	printStep(s.Out, 5, 5, "Выходные VPN-серверы и запуск")
	var addedForeign []config.ForeignServer
	for index := 0; ; index++ {
		if index == 0 {
			fmt.Fprintln(s.Out, "Добавление выходного VPN-сервера.")
		} else if !confirm.AskYesNo(reader, s.Out, "Добавить ещё один выходной VPN-сервер?") {
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
			return fmt.Errorf("нужен хотя бы один выходной VPN-сервер")
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

	link := reality.ClientLink(cfg.Reality.UUID, cfg.RUServer.IP, cfg.RUServer.VPNPort, cfg.Reality.PublicKey, cfg.Reality.ShortID, cfg.Reality.SNI, "vpn-router-ru")
	if err := config.WriteSensitiveText(s.Paths.ClientLink, link+"\n"); err != nil {
		return err
	}
	text, err := summary.Save(s.Paths)
	if err != nil {
		return err
	}
	fmt.Fprintln(s.Out)
	for _, added := range addedForeign {
		fmt.Fprintf(s.Out, "Выходной VPN-сервер готов: %s, SNI: %s\n", added.Name, added.Reality.SNI)
	}
	fmt.Fprintln(s.Out, text)
	return nil
}

func printStep(out io.Writer, current int, total int, title string) {
	fmt.Fprintf(out, "\nШаг %d из %d: %s\n\n", current, total, title)
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
	foreignServer := config.ForeignServer{
		Name:    ask(reader, out, "Имя выходного сервера", defaultName),
		IP:      ask(reader, out, "IP выходного VPN-сервера", ""),
		SSHUser: ask(reader, out, "SSH user выходного сервера", "root"),
		SSHPort: askInt(reader, out, "SSH port выходного сервера", 22),
		VPNPort: askInt(reader, out, "VPN port выходного сервера", 443),
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
	if err := waitForSSH(ctx, ssh, foreignTarget, "выходной VPN-сервер", reader, out); err != nil {
		return foreignServer, err
	}
	if findings := preflight.CollectRemote(ctx, ssh, foreignTarget, "выходной VPN-сервер"); len(findings) > 0 && foreignAction != "use" {
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

func generateRemoteRealityKeypair(ctx context.Context, ssh sshclient.Client, target sshclient.Target) (string, string, error) {
	out, err := ssh.Run(ctx, target, "sing-box generate reality-keypair")
	if err != nil {
		return "", "", fmt.Errorf("не удалось сгенерировать REALITY keypair на входном VPN-сервере: %w", err)
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

func resolveForeignConflict(paths config.Paths, server *config.ForeignServer, reader *bufio.Reader, out io.Writer) (string, config.ForeignServer, error) {
	for {
		existing, found, err := findForeignLocal(paths, server.Name)
		if err != nil || !found {
			return "add", config.ForeignServer{}, err
		}
		fmt.Fprintf(out, "\nВыходной VPN-сервер с именем %q уже есть в конфиге.\n", server.Name)
		fmt.Fprintf(out, "  сохранённый: %s:%d, SSH %s:%d, SNI %s\n", existing.IP, existing.VPNPort, existing.SSHUser, existing.SSHPort, showValue(existing.Reality.SNI))
		fmt.Fprintf(out, "  введённый:   %s:%d, SSH %s:%d\n", server.IP, server.VPNPort, server.SSHUser, server.SSHPort)
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

func chooseWiFiSettings(cfg *config.Config, caps wifi.Capabilities, reader *bufio.Reader, out io.Writer) error {
	rec := wifi.DefaultSettings(caps)
	defaultBand := cfg.WiFi.Band
	if err := wifi.ValidateSettings(defaultBand, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, caps); err != nil {
		defaultBand = rec.Band
		cfg.WiFi.Channel = rec.Channel
		cfg.WiFi.ChannelWidth = rec.ChannelWidth
	}
	for {
		band := ask(reader, out, "Диапазон Wi-Fi: 2.4 или 5", defaultBand)
		if err := wifi.ConfigureBand(cfg, band); err != nil {
			fmt.Fprintln(out, err)
			continue
		}
		if band == "2.4" && !caps.Supports24 {
			fmt.Fprintln(out, "Выбранный адаптер не поддерживает 2.4 GHz. Доступно:", wifi.FormatCapabilities(caps))
			continue
		}
		if band == "5" && !caps.Supports5 {
			fmt.Fprintln(out, "Выбранный адаптер не поддерживает 5 GHz. Доступно:", wifi.FormatCapabilities(caps))
			continue
		}
		break
	}
	cfg.WiFi.Channel = askSupportedChannel(reader, out, cfg.WiFi.Band, cfg.WiFi.Channel, caps)
	cfg.WiFi.ChannelWidth = askSupportedWidth(reader, out, cfg.WiFi.ChannelWidth, caps)
	return wifi.ValidateSettings(cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, caps)
}

func askSupportedChannel(reader *bufio.Reader, out io.Writer, band string, def int, caps wifi.Capabilities) int {
	allowed := caps.Channels24
	if band == "5" {
		allowed = caps.Channels5
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
			return channel
		}
		fmt.Fprintf(out, "Канал %d не поддерживается выбранным адаптером. Доступно: %s\n", channel, wifi.FormatInts(allowed))
	}
}

func askSupportedWidth(reader *bufio.Reader, out io.Writer, def int, caps wifi.Capabilities) int {
	if !containsInt(caps.Widths, def) && len(caps.Widths) > 0 {
		def = caps.Widths[0]
	}
	for {
		width := askInt(reader, out, "Ширина канала MHz (доступно: "+wifi.FormatInts(caps.Widths)+")", def)
		if containsInt(caps.Widths, width) {
			return width
		}
		fmt.Fprintf(out, "Ширина %d MHz не поддерживается выбранным адаптером. Доступно: %s\n", width, wifi.FormatInts(caps.Widths))
	}
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
		return ask(reader, out, "Wi-Fi адаптер для раздачи интернета", current)
	}
	netInfo := interfaceInfoMap(ctx, runner)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Выберите Wi-Fi адаптер, который будет раздавать интернет.")
	fmt.Fprintln(out, "Этот адаптер будет использоваться только как точка доступа.")
	defaultName := current
	if defaultName != "" && (interfaceHasIPv4(netInfo[defaultName]) || isP2PInterface(interfaces, defaultName)) {
		defaultName = ""
	}
	hasUsable := false
	for i, iface := range interfaces {
		info := netInfo[iface.Name]
		if !isP2PType(iface.Type) {
			hasUsable = true
		}
		if defaultName == "" && iface.Name != wan && !interfaceHasIPv4(info) && !isP2PType(iface.Type) {
			defaultName = iface.Name
		}
		note := ""
		if iface.Name == wan {
			note = " не рекомендуется: выбран как WAN"
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
		return ask(reader, out, "Wi-Fi адаптер для раздачи интернета", current)
	}
	return chooseAPByNumberOrName(reader, out, defaultName, interfaces)
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

func chooseAPByNumberOrName(reader *bufio.Reader, out io.Writer, def string, interfaces []wifi.InterfaceInfo) string {
	names := wifiNames(interfaces)
	for {
		answer := chooseByNumberOrName(reader, out, "Wi-Fi адаптер для раздачи", def, names)
		if answer == "" {
			fmt.Fprintln(out, "Нужно выбрать Wi-Fi адаптер для раздачи из списка.")
			continue
		}
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
		if len(value) >= 8 && len(value) <= 63 {
			return value
		}
		fmt.Fprintln(out, "Пароль Wi-Fi должен быть от 8 до 63 символов.")
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
