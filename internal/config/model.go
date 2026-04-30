package config

type Config struct {
	MiniPC      MiniPCConfig   `json:"mini_pc"`
	WiFi        WiFiConfig     `json:"wifi"`
	RUServer    RUServerConfig `json:"ru_server"`
	Reality     RealityRuntime `json:"reality,omitempty"`
	VPNPolicy   VPNPolicy      `json:"vpn_policy"`
	CurrentMode string         `json:"current_mode"`
}

type MiniPCConfig struct {
	WANInterface string `json:"wan_interface"`
	APInterface  string `json:"ap_interface"`
	SSID         string `json:"ssid"`
	LANCIDR      string `json:"lan_cidr"`
	LANGateway   string `json:"lan_gateway"`
}

type WiFiConfig struct {
	Country      string `json:"country"`
	Band         string `json:"band"`
	Channel      int    `json:"channel"`
	ChannelWidth int    `json:"channel_width"`
	HWMode       string `json:"hw_mode"`
	IEEE80211n   bool   `json:"ieee80211n"`
	IEEE80211ac  bool   `json:"ieee80211ac"`
	IEEE80211ax  bool   `json:"ieee80211ax,omitempty"`
	Password     string `json:"password,omitempty"`
}

type RUServerConfig struct {
	IP      string `json:"ip"`
	SSHUser string `json:"ssh_user"`
	SSHPort int    `json:"ssh_port"`
	VPNPort int    `json:"vpn_port"`
}

type VPNPolicy struct {
	Mode                string `json:"mode"`
	CustomDirectEnabled bool   `json:"custom_direct_enabled"`
}

type ForeignServers struct {
	Servers []ForeignServer `json:"servers"`
}

type ForeignServer struct {
	Name    string        `json:"name"`
	IP      string        `json:"ip"`
	SSHUser string        `json:"ssh_user"`
	SSHPort int           `json:"ssh_port"`
	VPNPort int           `json:"vpn_port"`
	Reality RealityConfig `json:"reality"`
}

type RealityConfig struct {
	SNI       string `json:"sni"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type RealityRuntime struct {
	UUID      string `json:"uuid,omitempty"`
	SNI       string `json:"sni,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
}

type RURemoteState struct {
	Mode            string `json:"mode"`
	SelectedForeign string `json:"selected_foreign"`
}

func DefaultConfig() Config {
	return Config{
		MiniPC: MiniPCConfig{
			SSID:       "MyVPNRouter",
			LANCIDR:    "10.77.0.0/24",
			LANGateway: "10.77.0.1",
		},
		WiFi: WiFiConfig{
			Country:      "RU",
			Band:         "5",
			Channel:      36,
			ChannelWidth: 80,
			HWMode:       "a",
			IEEE80211n:   true,
			IEEE80211ac:  true,
		},
		VPNPolicy: VPNPolicy{
			Mode:                "rule_set",
			CustomDirectEnabled: true,
		},
		CurrentMode: "direct",
	}
}
