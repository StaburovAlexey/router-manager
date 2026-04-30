package wifi

import (
	"context"
	"strings"
	"testing"

	"vpn-router/internal/shell"
)

func TestParseCapabilities(t *testing.T) {
	text := `
Supported interface modes:
	 * managed
	 * AP
Band 1:
	Capabilities: 0x19ef
		HT20/HT40
	Frequencies:
		* 2412 MHz [1] (20.0 dBm)
		* 2437 MHz [6] (20.0 dBm)
		* 2462 MHz [11] (20.0 dBm)
Band 2:
	VHT Capabilities (0x0)
	Frequencies:
		* 5180 MHz [36] (23.0 dBm)
		* 5200 MHz [40] (23.0 dBm)
		* 5260 MHz [52] (disabled)
`
	caps := ParseCapabilities(text)
	if !caps.SupportsAP || !caps.Supports24 || !caps.Supports5 || !caps.SupportsHT || !caps.SupportsVHT {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if len(caps.Channels5) != 2 || caps.Channels5[0] != 36 || caps.Channels5[1] != 40 {
		t.Fatalf("unexpected 5GHz channels: %#v", caps.Channels5)
	}
}

func TestRecommendPrefersNonDFSFiveGHz(t *testing.T) {
	caps := Capabilities{
		Supports5: true,
		Channels5: []int{36, 40, 44, 48, 52},
		Widths:    []int{20, 40, 80},
	}
	rec := Recommend(caps, []Network{{Channel: 1}, {Channel: 6}})
	if rec.Band != "5" || rec.Channel != 36 || rec.ChannelWidth != 80 {
		t.Fatalf("unexpected recommendation: %#v", rec)
	}
}

func TestParseScan(t *testing.T) {
	text := `
BSS aa:bb(on wlan0)
	freq: 2412
	SSID: one
	DS Parameter set: channel 1
BSS cc:dd(on wlan0)
	freq: 5180
	SSID: five
	DS Parameter set: channel 36
`
	networks := ParseScan(text)
	if len(networks) != 2 {
		t.Fatalf("network count = %d", len(networks))
	}
	if networks[1].Band != "5" || networks[1].Channel != 36 {
		t.Fatalf("unexpected second network: %#v", networks[1])
	}
}

func TestParseInterfaceInfos(t *testing.T) {
	text := `
phy#0
	Interface wlan0
		ifindex 3
		addr aa:bb:cc:dd:ee:ff
		type managed
phy#1
	Interface wlan1
		ifindex 4
		addr 11:22:33:44:55:66
		type AP
`
	infos := ParseInterfaceInfos(text)
	if len(infos) != 2 {
		t.Fatalf("interface count = %d", len(infos))
	}
	if infos[0].Name != "wlan0" || infos[0].Phy != "0" || infos[0].Type != "managed" || infos[1].Name != "wlan1" || infos[1].Phy != "1" || infos[1].Type != "AP" {
		t.Fatalf("unexpected infos: %#v", infos)
	}
}

func TestParseCapabilitiesForPhyDoesNotMixAdapters(t *testing.T) {
	text := `
Wiphy phy1
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Capabilities: 0x19ef
			HT20/HT40
		Frequencies:
			* 2412.0 MHz [1] (20.0 dBm)
			* 2437.0 MHz [6] (20.0 dBm)
Wiphy phy0
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
	Band 2:
		VHT Capabilities (0x0)
		Frequencies:
			* 5180 MHz [36] (20.0 dBm)
`
	usb := ParseCapabilitiesForPhy(text, "1")
	if !usb.SupportsAP || !usb.Supports24 {
		t.Fatalf("unexpected usb caps: %#v", usb)
	}
	if len(usb.Channels24) != 2 || usb.Channels24[0] != 1 || usb.Channels24[1] != 6 {
		t.Fatalf("unexpected usb 2.4 channels: %#v", usb.Channels24)
	}
	if usb.Supports5 || len(usb.Channels5) != 0 {
		t.Fatalf("phy1 must not inherit phy0 5GHz caps: %#v", usb)
	}
	internal := ParseCapabilitiesForPhy(text, "0")
	if !internal.Supports5 || len(internal.Channels5) != 1 || internal.Channels5[0] != 36 {
		t.Fatalf("unexpected phy0 caps: %#v", internal)
	}
}

func TestValidateSettingsRejectsUnsupportedFiveGHz(t *testing.T) {
	caps := Capabilities{SupportsAP: true, Supports24: true, Channels24: []int{1, 6, 11}, Widths: []int{20}}
	if err := ValidateSettings("5", 36, 80, caps); err == nil {
		t.Fatal("expected unsupported 5GHz error")
	}
}

func TestParseCapabilitiesSkipsNoIRAndRadarChannels(t *testing.T) {
	text := `
Supported interface modes:
	 * managed
	 * AP
Band 1:
	Frequencies:
		* 2412 MHz [1] (20.0 dBm)
		* 2437 MHz [6] (20.0 dBm)
Band 2:
	VHT Capabilities (0x0)
	Frequencies:
		* 5180 MHz [36] (20.0 dBm) (no IR)
		* 5200 MHz [40] (20.0 dBm) (NO-IR)
		* 5220 MHz [44] (20.0 dBm) (radar detection)
		* 5745 MHz [149] (20.0 dBm)
`
	caps := ParseCapabilities(text)
	if !caps.Supports24 {
		t.Fatalf("expected 2.4 GHz support: %#v", caps)
	}
	if !caps.Supports5 || len(caps.Channels5) != 1 || caps.Channels5[0] != 149 {
		t.Fatalf("unexpected 5GHz channels: %#v", caps.Channels5)
	}
}

func TestInspectInterfaceRejectsP2PDevice(t *testing.T) {
	runner := &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#1
	Interface p2p-dev-wlan0
		type P2P-device
`,
	}}
	_, err := InspectInterface(context.Background(), runner, "p2p-dev-wlan0")
	if err == nil || !strings.Contains(err.Error(), "P2P-device") {
		t.Fatalf("expected P2P-device error, got %v", err)
	}
}
