package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultBaseDir = "/etc/vpn-router"
)

type Paths struct {
	BaseDir          string
	Config           string
	ForeignServers   string
	InstallSummary   string
	ClientLink       string
	RulesDir         string
	VPNInfraRules    string
	CustomDirect     string
	CustomProxy      string
	ModesDir         string
	VPNMode          string
	DirectMode       string
	BackupsDir       string
	HostapdConf      string
	DnsmasqConf      string
	SysctlConf       string
	NftablesMainConf string
	NftablesConf     string
	SingBoxLocalConf string
}

func DefaultPaths() Paths {
	base := os.Getenv("VPN_ROUTER_ETC")
	if base == "" {
		base = DefaultBaseDir
	}
	return NewPaths(base)
}

func NewPaths(base string) Paths {
	rules := filepath.Join(base, "rules")
	modes := filepath.Join(base, "modes")
	return Paths{
		BaseDir:          base,
		Config:           filepath.Join(base, "config.json"),
		ForeignServers:   filepath.Join(base, "foreign-servers.json"),
		InstallSummary:   filepath.Join(base, "install-summary.txt"),
		ClientLink:       filepath.Join(base, "client-link.txt"),
		RulesDir:         rules,
		VPNInfraRules:    filepath.Join(rules, "vpn-infra.json"),
		CustomDirect:     filepath.Join(rules, "custom-direct.json"),
		CustomProxy:      filepath.Join(rules, "custom-proxy.json"),
		ModesDir:         modes,
		VPNMode:          filepath.Join(modes, "vpn.json"),
		DirectMode:       filepath.Join(modes, "direct.json"),
		BackupsDir:       filepath.Join(base, "backups"),
		HostapdConf:      "/etc/hostapd/hostapd.conf",
		DnsmasqConf:      "/etc/dnsmasq.d/vpn-router.conf",
		SysctlConf:       "/etc/sysctl.d/99-vpn-router.conf",
		NftablesMainConf: "/etc/nftables.conf",
		NftablesConf:     "/etc/nftables.d/vpn-router.nft",
		SingBoxLocalConf: "/etc/sing-box/config.json",
	}
}
