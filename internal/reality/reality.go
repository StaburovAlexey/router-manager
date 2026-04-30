package reality

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

var SNICandidates = []string{
	"www.microsoft.com",
	"www.apple.com",
	"www.cloudflare.com",
	"www.amazon.com",
	"www.bing.com",
	"www.office.com",
	"www.mozilla.org",
}

type Selector struct {
	Timeout time.Duration
}

func (s Selector) Select(ctx context.Context) (string, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	var lastErr error
	for _, host := range SNICandidates {
		if err := checkSNI(ctx, host, timeout); err != nil {
			lastErr = err
			continue
		}
		return host, nil
	}
	return "", fmt.Errorf("не удалось подобрать рабочий SNI: %w", lastErr)
}

func ClientLink(uuid, host string, port int, publicKey, shortID, sni, name string) string {
	if name == "" {
		name = "vpn-router-ru"
	}
	return fmt.Sprintf(
		"vless://%s@%s:%d?security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp&flow=xtls-rprx-vision#%s",
		uuid,
		host,
		port,
		sni,
		publicKey,
		shortID,
		strings.ReplaceAll(name, " ", "-"),
	)
}

func NewUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func RandomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}

func checkSNI(ctx context.Context, host string, timeout time.Duration) error {
	resolver := net.Resolver{}
	if _, err := resolver.LookupHost(ctx, host); err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, "443"), &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.HandshakeContext(ctx)
}
