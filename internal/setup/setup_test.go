package setup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
	"router-manager/internal/wifi"
)

func TestAskRequiredRepeatsWhenValueAndDefaultAreEmpty(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n203.0.113.10\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP входного сервера", "")

	if value != "203.0.113.10" {
		t.Fatalf("askRequired() = %q, want %q", value, "203.0.113.10")
	}
	if got := strings.Count(out.String(), "IP входного сервера"); got < 3 {
		t.Fatalf("expected prompt, validation message, and retry prompt in output, got %d occurrences:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "IP входного сервера не может быть пустым.") {
		t.Fatalf("missing required message in output:\n%s", out.String())
	}
}

func TestAskRequiredKeepsDefaultOnEmptyInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP входного сервера", "203.0.113.10")

	if value != "203.0.113.10" {
		t.Fatalf("askRequired() = %q, want default", value)
	}
	if strings.Contains(out.String(), "не может быть пустым") {
		t.Fatalf("default should not trigger validation message:\n%s", out.String())
	}
}

func TestAskRequiredReturnsEnteredValue(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("198.51.100.20\n"))
	var out bytes.Buffer

	value := askRequired(reader, &out, "IP выходного сервера", "")

	if value != "198.51.100.20" {
		t.Fatalf("askRequired() = %q, want entered value", value)
	}
}

func TestShouldReenterServerDetailsForHostKeyScanError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("да\n"))
	var out bytes.Buffer
	err := hostKeyScanError{label: "входной сервер", err: errors.New("scan failed")}

	if !shouldReenterServerDetails(err, reader, &out, "входного сервера") {
		t.Fatal("expected yes answer to request re-entering server details")
	}
	for _, want := range []string{"Не удалось проверить SSH host key входного сервера", "Ввести данные входного сервера заново?"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestShouldReenterServerDetailsCanDecline(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("нет\n"))
	var out bytes.Buffer
	err := hostKeyScanError{label: "выходной сервер", err: errors.New("scan failed")}

	if shouldReenterServerDetails(err, reader, &out, "выходного сервера") {
		t.Fatal("expected no answer to decline re-entering server details")
	}
}

func TestShouldReenterServerDetailsIgnoresOtherErrors(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("да\n"))
	var out bytes.Buffer

	if shouldReenterServerDetails(errors.New("permission denied"), reader, &out, "входного сервера") {
		t.Fatal("non host key scan errors must not request server details again")
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output for ignored error:\n%s", out.String())
	}
}

func TestChooseWiFiSettingsAutoAppliesRecommendation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	caps := wifi.Capabilities{
		Supports24: true,
		Supports5:  true,
		Channels24: []int{1, 6, 11},
		Channels5:  []int{36, 40},
		Widths:     []int{20, 40, 80},
	}
	reader := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer
	runner := &shell.DryRunner{Outputs: map[string]string{"iw dev wlan0 scan": ""}}

	if err := chooseWiFiSettings(context.Background(), runner, &cfg, caps, reader, &out); err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Band != "5" || cfg.WiFi.Channel != 36 || cfg.WiFi.ChannelWidth != 80 {
		t.Fatalf("unexpected auto settings: %#v", cfg.WiFi)
	}
	if !strings.Contains(out.String(), "Автонастройка Wi-Fi: 5 GHz, канал 36, ширина 80 MHz") {
		t.Fatalf("missing auto summary:\n%s", out.String())
	}
}

func TestChooseWiFiSettingsManualDoesNotShowUnsupportedFiveGHz(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WiFi.Band = "5"
	caps := wifi.Capabilities{
		Supports24: true,
		Channels24: []int{1, 6},
		Widths:     []int{20},
	}
	reader := bufio.NewReader(strings.NewReader("manual\n6\n"))
	var out bytes.Buffer

	if err := chooseWiFiSettings(context.Background(), &shell.DryRunner{}, &cfg, caps, reader, &out); err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Band != "2.4" || cfg.WiFi.Channel != 6 || cfg.WiFi.ChannelWidth != 20 {
		t.Fatalf("unexpected manual settings: %#v", cfg.WiFi)
	}
	if strings.Contains(out.String(), "5 GHz") {
		t.Fatalf("manual setup must not show unsupported 5 GHz option:\n%s", out.String())
	}
}

func TestChooseWiFiSettingsManualRejectsUnsupportedChannel(t *testing.T) {
	cfg := config.DefaultConfig()
	caps := wifi.Capabilities{
		Supports24: true,
		Channels24: []int{1, 6},
		Widths:     []int{20},
	}
	reader := bufio.NewReader(strings.NewReader("manual\n11\n6\n"))
	var out bytes.Buffer

	if err := chooseWiFiSettings(context.Background(), &shell.DryRunner{}, &cfg, caps, reader, &out); err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Channel != 6 {
		t.Fatalf("channel = %d, want 6", cfg.WiFi.Channel)
	}
	if !strings.Contains(out.String(), "Доступно: 1, 6") {
		t.Fatalf("missing supported channel hint:\n%s", out.String())
	}
}

func TestChooseWiFiSettingsManualSupportsFiveGHzWhenAvailable(t *testing.T) {
	cfg := config.DefaultConfig()
	caps := wifi.Capabilities{
		Supports24: true,
		Supports5:  true,
		Channels24: []int{1, 6},
		Channels5:  []int{36, 40},
		Widths:     []int{20, 80},
	}
	reader := bufio.NewReader(strings.NewReader("manual\n5\n36\n80\n"))
	var out bytes.Buffer

	if err := chooseWiFiSettings(context.Background(), &shell.DryRunner{}, &cfg, caps, reader, &out); err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Band != "5" || cfg.WiFi.Channel != 36 || cfg.WiFi.ChannelWidth != 80 {
		t.Fatalf("unexpected manual 5 GHz settings: %#v", cfg.WiFi)
	}
	if !strings.Contains(out.String(), "Диапазон Wi-Fi (доступно: 2.4 или 5)") {
		t.Fatalf("missing supported band prompt:\n%s", out.String())
	}
}

func TestChooseAPInterfaceRequiresExplicitSelectionOnResume(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n2\n"))
	var out bytes.Buffer
	runner := &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#0
	Interface wlan0
		type managed
phy#1
	Interface wlan1
		type managed
`,
		"ip route show default": "default via 192.0.2.1 dev eth0",
		"ip -o link show": `
1: lo: <LOOPBACK> state UNKNOWN link/loopback 00:00:00:00:00:00
2: wlan0: <BROADCAST> state DOWN link/ether 00:11:22:33:44:55
3: wlan1: <BROADCAST> state DOWN link/ether 00:11:22:33:44:66
`,
	}}

	iface := chooseAPInterface(context.Background(), runner, reader, &out, "wlan0", "eth0")

	if iface != "wlan1" {
		t.Fatalf("interface = %q, want explicit second choice wlan1", iface)
	}
	if !strings.Contains(out.String(), "wlan0 текущий") {
		t.Fatalf("current adapter was not marked:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Нужно выбрать значение из списка.") {
		t.Fatalf("empty answer must not silently reuse current adapter:\n%s", out.String())
	}
}
