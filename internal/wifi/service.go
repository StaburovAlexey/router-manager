package wifi

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"router-manager/internal/config"
	"router-manager/internal/network"
	"router-manager/internal/shell"
	"router-manager/internal/system"
	"router-manager/templates"
)

type HostapdTemplateData struct {
	Country        string
	Interface      string
	SSID           string
	HWMode         string
	Channel        int
	IEEE80211n     bool
	IEEE80211ac    bool
	IEEE80211ax    bool
	VHTOperChWidth int
	Password       string
}

type DnsmasqTemplateData struct {
	APInterface string
	DHCPStart   string
	DHCPEnd     string
	LeaseTime   string
	Gateway     string
}

type SwitchResult struct {
	OldInterface   string
	NewInterface   string
	Recommendation Recommendation
}

func ValidateSSID(ssid string) error {
	if strings.TrimSpace(ssid) == "" {
		return fmt.Errorf("SSID Wi-Fi сети не может быть пустым")
	}
	if strings.ContainsAny(ssid, "\r\n") {
		return fmt.Errorf("SSID Wi-Fi сети не может содержать переносы строк")
	}
	if len([]byte(ssid)) > 32 {
		return fmt.Errorf("SSID Wi-Fi сети должен быть не длиннее 32 байт")
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 || len(password) > 63 {
		return fmt.Errorf("пароль Wi-Fi должен быть от 8 до 63 символов")
	}
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("пароль Wi-Fi не может содержать переносы строк")
	}
	return nil
}

func RenderHostapd(cfg config.Config) ([]byte, error) {
	width := 0
	if cfg.WiFi.ChannelWidth >= 80 {
		width = 1
	}
	return templates.Render("hostapd.conf.tmpl", HostapdTemplateData{
		Country:        cfg.WiFi.Country,
		Interface:      cfg.MiniPC.APInterface,
		SSID:           cfg.MiniPC.SSID,
		HWMode:         cfg.WiFi.HWMode,
		Channel:        cfg.WiFi.Channel,
		IEEE80211n:     cfg.WiFi.IEEE80211n,
		IEEE80211ac:    cfg.WiFi.IEEE80211ac,
		IEEE80211ax:    cfg.WiFi.IEEE80211ax,
		VHTOperChWidth: width,
		Password:       cfg.WiFi.Password,
	})
}

func RenderDnsmasq(cfg config.Config) ([]byte, error) {
	start, end, err := dhcpRange(cfg.MiniPC.LANCIDR)
	if err != nil {
		return nil, err
	}
	return templates.Render("dnsmasq.conf.tmpl", DnsmasqTemplateData{
		APInterface: cfg.MiniPC.APInterface,
		DHCPStart:   start,
		DHCPEnd:     end,
		LeaseTime:   "12h",
		Gateway:     cfg.MiniPC.LANGateway,
	})
}

func ApplyAccessPoint(ctx context.Context, runner shell.Runner, paths config.Paths, cfg config.Config) error {
	caps, err := InspectInterface(ctx, runner, cfg.MiniPC.APInterface)
	if err != nil {
		return err
	}
	if err := ValidateSettings(cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, caps); err != nil {
		return err
	}
	if err := PrepareRadio(ctx, runner, cfg.MiniPC.APInterface); err != nil {
		return err
	}
	hostapd, err := RenderHostapd(cfg)
	if err != nil {
		return err
	}
	dnsmasq, err := RenderDnsmasq(cfg)
	if err != nil {
		return err
	}
	if err := writeConfigWithBackup(paths.HostapdConf, paths.BackupsDir, hostapd, 0o600); err != nil {
		return err
	}
	if err := writeConfigWithBackup(paths.DnsmasqConf, paths.BackupsDir, dnsmasq, 0o600); err != nil {
		return err
	}
	if err := system.UnmaskEnable(ctx, runner, "hostapd"); err != nil {
		return err
	}
	if err := system.UnmaskEnable(ctx, runner, "dnsmasq"); err != nil {
		return err
	}
	if err := network.ConfigureLANInterface(ctx, runner, cfg); err != nil {
		return err
	}
	if err := system.RestartAndCheck(ctx, runner, "hostapd"); err != nil {
		return err
	}
	return system.RestartAndCheck(ctx, runner, "dnsmasq")
}

func Restart(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if err := PrepareRadio(ctx, runner, cfg.MiniPC.APInterface); err != nil {
		return err
	}
	if err := network.ConfigureLANInterface(ctx, runner, cfg); err != nil {
		return err
	}
	if err := system.RestartAndCheck(ctx, runner, "hostapd"); err != nil {
		return err
	}
	return system.RestartAndCheck(ctx, runner, "dnsmasq")
}

func SwitchAccessPoint(ctx context.Context, runner shell.Runner, paths config.Paths, iface string) (SwitchResult, error) {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return SwitchResult{}, fmt.Errorf("Wi-Fi адаптер для раздачи не выбран")
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return SwitchResult{}, err
	}
	oldCfg := cfg
	caps, rec, err := RecommendedSettings(ctx, runner, iface)
	if err != nil {
		return SwitchResult{}, err
	}
	if err := ValidateSettings(rec.Band, rec.Channel, rec.ChannelWidth, caps); err != nil {
		return SwitchResult{}, err
	}
	cfg.MiniPC.APInterface = iface
	ApplyRecommendation(&cfg, rec)
	if err := config.Save(paths, cfg); err != nil {
		return SwitchResult{}, err
	}
	if err := ApplyAccessPoint(ctx, runner, paths, cfg); err != nil {
		_ = config.Save(paths, oldCfg)
		if strings.TrimSpace(oldCfg.MiniPC.APInterface) != "" {
			if restoreErr := ApplyAccessPoint(ctx, runner, paths, oldCfg); restoreErr != nil {
				return SwitchResult{}, fmt.Errorf("новый Wi-Fi адаптер %s не запустился: %w; откат на старый адаптер %s тоже не удался: %v", iface, err, oldCfg.MiniPC.APInterface, restoreErr)
			}
		}
		return SwitchResult{}, fmt.Errorf("новый Wi-Fi адаптер %s не запустился, настройки возвращены на %s: %w", iface, oldCfg.MiniPC.APInterface, err)
	}
	if oldCfg.MiniPC.APInterface != "" && oldCfg.MiniPC.APInterface != iface {
		ReleaseRadio(ctx, runner, oldCfg.MiniPC.APInterface)
	}
	return SwitchResult{OldInterface: oldCfg.MiniPC.APInterface, NewInterface: iface, Recommendation: rec}, nil
}

func RecommendedSettings(ctx context.Context, runner shell.Runner, iface string) (Capabilities, Recommendation, error) {
	caps, err := InspectInterface(ctx, runner, iface)
	if err != nil {
		return Capabilities{}, Recommendation{}, err
	}
	networks, _ := Scan(ctx, runner, iface)
	rec := Recommend(caps, networks)
	return caps, rec, nil
}

func ApplyRecommendation(cfg *config.Config, rec Recommendation) {
	_ = ConfigureBand(cfg, rec.Band)
	cfg.WiFi.Channel = rec.Channel
	cfg.WiFi.ChannelWidth = rec.ChannelWidth
}

func PrepareRadio(ctx context.Context, runner shell.Runner, iface string) error {
	if err := runner.Run(ctx, "rfkill", "unblock", "wifi"); err != nil {
		return fmt.Errorf("Wi-Fi заблокирован rfkill, и снять блокировку автоматически не удалось: %w", err)
	}
	if iface != "" {
		_ = runner.Run(ctx, "nmcli", "device", "set", iface, "managed", "no")
		_ = runner.Run(ctx, "ip", "link", "set", iface, "down")
		_ = runner.Run(ctx, "ip", "link", "set", iface, "up")
	}
	return nil
}

func ReleaseRadio(ctx context.Context, runner shell.Runner, iface string) {
	if strings.TrimSpace(iface) == "" {
		return
	}
	_ = runner.Run(ctx, "ip", "addr", "flush", "dev", iface)
	_ = runner.Run(ctx, "ip", "link", "set", iface, "down")
	_ = runner.Run(ctx, "nmcli", "device", "set", iface, "managed", "yes")
	_ = runner.Run(ctx, "ip", "link", "set", iface, "up")
}

func Scan(ctx context.Context, runner shell.Runner, iface string) ([]Network, error) {
	out, err := runner.Output(ctx, "iw", "dev", iface, "scan")
	if err != nil {
		return nil, err
	}
	return ParseScan(out), nil
}

func ParseScan(text string) []Network {
	var result []Network
	var current Network
	channelRE := regexp.MustCompile(`DS Parameter set: channel ([0-9]+)`)
	freqRE := regexp.MustCompile(`freq: ([0-9]+)`)
	flush := func() {
		if current.Channel != 0 {
			if current.Band == "" {
				if current.Channel <= 14 {
					current.Band = "2.4"
				} else {
					current.Band = "5"
				}
			}
			result = append(result, current)
		}
		current = Network{}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "BSS ") {
			flush()
			continue
		}
		if strings.HasPrefix(line, "SSID:") {
			current.SSID = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		}
		if match := channelRE.FindStringSubmatch(line); len(match) == 2 {
			current.Channel, _ = strconv.Atoi(match[1])
		}
		if match := freqRE.FindStringSubmatch(line); len(match) == 2 {
			freq, _ := strconv.Atoi(match[1])
			if freq >= 2400 && freq < 2500 {
				current.Band = "2.4"
			}
			if freq >= 5000 && freq < 5900 {
				current.Band = "5"
			}
		}
	}
	flush()
	return result
}

func ConfigureBand(cfg *config.Config, band string) error {
	switch band {
	case "2.4":
		cfg.WiFi.Band = "2.4"
		cfg.WiFi.HWMode = "g"
		cfg.WiFi.Channel = 6
		cfg.WiFi.ChannelWidth = 20
		cfg.WiFi.IEEE80211ac = false
	case "5":
		cfg.WiFi.Band = "5"
		cfg.WiFi.HWMode = "a"
		cfg.WiFi.Channel = 36
		cfg.WiFi.ChannelWidth = 80
		cfg.WiFi.IEEE80211ac = true
	default:
		return fmt.Errorf("диапазон должен быть 2.4 или 5")
	}
	cfg.WiFi.IEEE80211n = true
	return nil
}

func writeConfigWithBackup(path, backupsDir string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := system.BackupFile(path, backupsDir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func dhcpRange(cidr string) (string, string, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", "", fmt.Errorf("IPv6 LAN subnet пока не поддерживается для dnsmasq")
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones > 24 {
		return "", "", fmt.Errorf("LAN subnet должен быть IPv4 /24 или шире")
	}
	start := append(net.IP(nil), ip4...)
	end := append(net.IP(nil), ip4...)
	start[3] += 100
	end[3] += 200
	return start.String(), end.String(), nil
}
