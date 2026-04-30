package wifi

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vpn-router/internal/shell"
)

type Capabilities struct {
	SupportsAP  bool
	Supports24  bool
	Supports5   bool
	SupportsHT  bool
	SupportsVHT bool
	SupportsHE  bool
	Channels24  []int
	Channels5   []int
	Widths      []int
}

type Network struct {
	SSID    string
	Channel int
	Band    string
}

type Recommendation struct {
	Band         string
	Channel      int
	ChannelWidth int
}

type InterfaceInfo struct {
	Name string
	Phy  string
	Type string
	Addr string
}

func Inspect(ctx context.Context, runner shell.Runner) (Capabilities, error) {
	out, err := runner.Output(ctx, "iw", "list")
	if err != nil {
		return Capabilities{}, err
	}
	caps := ParseCapabilities(out)
	if !caps.SupportsAP {
		return caps, fmt.Errorf("Wi-Fi адаптер не поддерживает AP mode")
	}
	return caps, nil
}

func InspectInterface(ctx context.Context, runner shell.Runner, iface string) (Capabilities, error) {
	if strings.TrimSpace(iface) == "" {
		return Capabilities{}, fmt.Errorf("Wi-Fi interface не выбран")
	}
	infos, err := InterfaceInfos(ctx, runner)
	if err != nil {
		return Capabilities{}, err
	}
	phy := ""
	for _, info := range infos {
		if info.Name == iface {
			phy = info.Phy
			break
		}
	}
	if phy == "" {
		return Capabilities{}, fmt.Errorf("Wi-Fi interface %s не найден в iw dev", iface)
	}
	out, err := runner.Output(ctx, "iw", "list")
	if err != nil {
		return Capabilities{}, err
	}
	caps := ParseCapabilitiesForPhy(out, phy)
	if !caps.SupportsAP {
		return caps, fmt.Errorf("Wi-Fi адаптер %s не поддерживает AP mode", iface)
	}
	return caps, nil
}

func Interfaces(ctx context.Context, runner shell.Runner) ([]string, error) {
	infos, err := InterfaceInfos(ctx, runner)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(infos))
	for _, info := range infos {
		result = append(result, info.Name)
	}
	return result, nil
}

func InterfaceInfos(ctx context.Context, runner shell.Runner) ([]InterfaceInfo, error) {
	out, err := runner.Output(ctx, "iw", "dev")
	if err != nil {
		return nil, err
	}
	return ParseInterfaceInfos(out), nil
}

func ParseInterfaceInfos(text string) []InterfaceInfo {
	var infos []InterfaceInfo
	var current *InterfaceInfo
	currentPhy := ""
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "phy#") {
			currentPhy = strings.TrimPrefix(line, "phy#")
			continue
		}
		if strings.HasPrefix(line, "Interface ") {
			if current != nil {
				infos = append(infos, *current)
			}
			current = &InterfaceInfo{Name: strings.TrimSpace(strings.TrimPrefix(line, "Interface ")), Phy: currentPhy}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "type ") {
			current.Type = strings.TrimSpace(strings.TrimPrefix(line, "type "))
		}
		if strings.HasPrefix(line, "addr ") {
			current.Addr = strings.TrimSpace(strings.TrimPrefix(line, "addr "))
		}
	}
	if current != nil {
		infos = append(infos, *current)
	}
	return infos
}

func ParseCapabilities(text string) Capabilities {
	return parseCapabilities(text)
}

func ParseCapabilitiesForPhy(text string, phy string) Capabilities {
	section := wiphySection(text, phy)
	if section == "" {
		return Capabilities{}
	}
	return parseCapabilities(section)
}

func ValidateSettings(cfgBand string, channel int, width int, caps Capabilities) error {
	switch cfgBand {
	case "2.4":
		if !caps.Supports24 {
			return fmt.Errorf("выбранный Wi-Fi адаптер не поддерживает 2.4 GHz AP")
		}
		if !containsInt(caps.Channels24, channel) {
			return fmt.Errorf("канал %d недоступен на выбранном адаптере; доступные 2.4 GHz каналы: %s", channel, FormatInts(caps.Channels24))
		}
	case "5":
		if !caps.Supports5 {
			return fmt.Errorf("выбранный Wi-Fi адаптер не поддерживает 5 GHz AP; выберите 2.4 GHz")
		}
		if !containsInt(caps.Channels5, channel) {
			return fmt.Errorf("канал %d недоступен на выбранном адаптере; доступные 5 GHz каналы: %s", channel, FormatInts(caps.Channels5))
		}
	default:
		return fmt.Errorf("диапазон должен быть 2.4 или 5")
	}
	if !containsInt(caps.Widths, width) {
		return fmt.Errorf("ширина канала %d MHz недоступна; доступные ширины: %s", width, FormatInts(caps.Widths))
	}
	return nil
}

func DefaultSettings(caps Capabilities) Recommendation {
	return Recommend(caps, nil)
}

func FormatCapabilities(caps Capabilities) string {
	var parts []string
	if caps.Supports24 {
		parts = append(parts, "2.4 GHz каналы: "+FormatInts(caps.Channels24))
	}
	if caps.Supports5 {
		parts = append(parts, "5 GHz каналы: "+FormatInts(caps.Channels5))
	}
	parts = append(parts, "ширины: "+FormatInts(caps.Widths)+" MHz")
	if caps.SupportsVHT {
		parts = append(parts, "802.11ac")
	}
	if caps.SupportsHE {
		parts = append(parts, "802.11ax")
	}
	return strings.Join(parts, "; ")
}

func FormatInts(values []int) string {
	if len(values) == 0 {
		return "нет"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}

func parseCapabilities(text string) Capabilities {
	var caps Capabilities
	channelRE := regexp.MustCompile(`\* ([0-9]+)(?:\.[0-9]+)? MHz \[([0-9]+)\]`)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "* AP":
			caps.SupportsAP = true
		case strings.Contains(line, "HT20") || strings.Contains(line, "HT40"):
			caps.SupportsHT = true
			addWidth(&caps, 20)
			if strings.Contains(line, "HT40") {
				addWidth(&caps, 40)
			}
		case strings.Contains(line, "VHT Capabilities"):
			caps.SupportsVHT = true
			addWidth(&caps, 80)
		case strings.Contains(line, "HE Iftypes") || strings.Contains(line, "HE PHY Capabilities"):
			caps.SupportsHE = true
		}
		if match := channelRE.FindStringSubmatch(line); len(match) == 3 && !strings.Contains(line, "disabled") {
			freq, _ := strconv.Atoi(match[1])
			channel, _ := strconv.Atoi(match[2])
			if freq >= 2400 && freq < 2500 {
				caps.Supports24 = true
				caps.Channels24 = appendUnique(caps.Channels24, channel)
			}
			if freq >= 5000 && freq < 5900 {
				caps.Supports5 = true
				caps.Channels5 = appendUnique(caps.Channels5, channel)
			}
		}
	}
	sort.Ints(caps.Channels24)
	sort.Ints(caps.Channels5)
	sort.Ints(caps.Widths)
	if len(caps.Widths) == 0 {
		caps.Widths = []int{20}
	}
	return caps
}

func wiphySection(text string, phy string) string {
	phy = strings.TrimPrefix(strings.TrimSpace(phy), "phy")
	var b strings.Builder
	inSection := false
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Wiphy ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "Wiphy "))
			name = strings.TrimPrefix(name, "phy")
			if inSection {
				break
			}
			inSection = name == phy
			if inSection {
				b.WriteString(line)
				b.WriteByte('\n')
			}
			continue
		}
		if inSection {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func Recommend(caps Capabilities, networks []Network) Recommendation {
	score := map[int]int{}
	for _, network := range networks {
		score[network.Channel]++
	}
	if caps.Supports5 {
		for _, channel := range []int{36, 40, 44, 48} {
			if containsInt(caps.Channels5, channel) {
				return Recommendation{Band: "5", Channel: channel, ChannelWidth: bestWidth(caps, 80)}
			}
		}
	}
	best := 1
	bestScore := int(^uint(0) >> 1)
	for _, channel := range []int{1, 6, 11} {
		if !containsInt(caps.Channels24, channel) {
			continue
		}
		if score[channel] < bestScore {
			best = channel
			bestScore = score[channel]
		}
	}
	return Recommendation{Band: "2.4", Channel: best, ChannelWidth: bestWidth(caps, 40)}
}

func addWidth(caps *Capabilities, width int) {
	caps.Widths = appendUnique(caps.Widths, width)
}

func appendUnique(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func containsInt(values []int, value int) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func bestWidth(caps Capabilities, wanted int) int {
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
