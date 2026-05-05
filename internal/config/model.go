package config

type Config struct {
	MiniPC        MiniPCConfig   `json:"mini_pc"`
	WiFi          WiFiConfig     `json:"wifi"`
	RUServer      RUServerConfig `json:"ru_server"`
	Reality       RealityRuntime `json:"reality,omitempty"`
	RoutingPolicy RoutingPolicy  `json:"tunnel_policy"`
	Routing       RoutingConfig  `json:"routing"`
	CurrentMode   string         `json:"current_mode"`
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
	IP         string `json:"ip"`
	SSHUser    string `json:"ssh_user"`
	SSHPort    int    `json:"ssh_port"`
	TunnelPort int    `json:"tunnel_port"`
}

type RoutingPolicy struct {
	Mode                string `json:"mode"`
	CustomDirectEnabled bool   `json:"custom_direct_enabled"`
}

type RoutingConfig struct {
	DefaultRoute            string `json:"default_route"`
	RUDomainsDirectEnabled  bool   `json:"ru_domains_direct_enabled"`
	RUGeoIPDirectEnabled    bool   `json:"ru_geoip_direct_enabled"`
	GeoIPSource             string `json:"geoip_source"`
	GeoIPLastUpdate         string `json:"geoip_last_update,omitempty"`
	GeoIPLastError          string `json:"geoip_last_error,omitempty"`
	GeoIPUpdateTimerEnabled bool   `json:"geoip_update_timer_enabled"`
}

const (
	DefaultRouteVPN    = "vpn"
	DefaultRouteDirect = "direct"
	DefaultGeoIPSource = "https://www.ipdeny.com/ipblocks/data/aggregated/ru-aggregated.zone"
)

type ForeignServers struct {
	Servers []ForeignServer `json:"servers"`
}

type ForeignServer struct {
	Name       string        `json:"name"`
	IP         string        `json:"ip"`
	SSHUser    string        `json:"ssh_user"`
	SSHPort    int           `json:"ssh_port"`
	TunnelPort int           `json:"tunnel_port"`
	Reality    RealityConfig `json:"reality"`
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
			SSID:       "MyRouterManager",
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
		RoutingPolicy: RoutingPolicy{
			Mode:                "rule_set",
			CustomDirectEnabled: true,
		},
		Routing: RoutingConfig{
			DefaultRoute:            DefaultRouteDirect,
			RUDomainsDirectEnabled:  true,
			RUGeoIPDirectEnabled:    true,
			GeoIPSource:             DefaultGeoIPSource,
			GeoIPUpdateTimerEnabled: true,
		},
		CurrentMode: "direct",
	}
}

func NormalizeConfig(cfg Config) Config {
	if cfg.MiniPC.SSID == "" {
		def := DefaultConfig()
		if cfg.MiniPC.LANCIDR == "" {
			cfg.MiniPC.LANCIDR = def.MiniPC.LANCIDR
		}
		if cfg.MiniPC.LANGateway == "" {
			cfg.MiniPC.LANGateway = def.MiniPC.LANGateway
		}
	}
	if cfg.Routing.DefaultRoute == "" {
		switch cfg.CurrentMode {
		case "tunnel":
			cfg.Routing.DefaultRoute = DefaultRouteVPN
		case "selective", "direct":
			cfg.Routing.DefaultRoute = DefaultRouteDirect
		default:
			cfg.Routing.DefaultRoute = DefaultRouteDirect
		}
	}
	if cfg.Routing.DefaultRoute != DefaultRouteVPN {
		cfg.Routing.DefaultRoute = DefaultRouteDirect
	}
	if cfg.Routing.GeoIPSource == "" {
		cfg.Routing.GeoIPSource = DefaultGeoIPSource
	}
	if !cfg.Routing.RUDomainsDirectEnabled && !cfg.Routing.RUGeoIPDirectEnabled && !cfg.Routing.GeoIPUpdateTimerEnabled {
		cfg.Routing.RUDomainsDirectEnabled = true
		cfg.Routing.RUGeoIPDirectEnabled = true
		cfg.Routing.GeoIPUpdateTimerEnabled = true
	}
	cfg.CurrentMode = CurrentModeForDefaultRoute(cfg.Routing.DefaultRoute)
	return cfg
}

func CurrentModeForDefaultRoute(route string) string {
	if route == DefaultRouteVPN {
		return "tunnel"
	}
	return "selective"
}
