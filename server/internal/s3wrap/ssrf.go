package s3wrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	errRedirectDenied  = errors.New("HTTP redirect denied (SSRF protection)")
	errEndpointBlocked = errors.New("endpoint host is blocked (link-local / cloud metadata)")
)

// ValidateEndpoint 校验账号 Endpoint / PublicEndpoint：禁止指向云元数据与链路本地地址。
// 私网 / 回环（MinIO、RustFS、局域网）仍允许，因自托管是主场景；SSRF 主防线是鉴权 + 禁重定向 + 禁 IMDS。
func ValidateEndpoint(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return nil
	}
	raw := endpoint
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if isBlockedHostname(host) {
		return fmt.Errorf("%w: %s", errEndpointBlocked, host)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// DNS 失败留给后续连接阶段；此处仅拦已解析到的危险地址。
		return nil
	}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			return fmt.Errorf("%w: %s -> %s", errEndpointBlocked, host, ipa.IP)
		}
	}
	return nil
}

func isBlockedHostname(host string) bool {
	switch host {
	case "metadata.google.internal", "metadata", "metadata.goog",
		"instance-data", "kubernetes.default", "kubernetes.default.svc":
		return true
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 常见云元数据（非链路本地段）：
	//   100.100.100.200 / 100.96.0.2 —— 阿里云 IMDS 及火山引擎内网元数据
	//   fd00:ec2::254 —— AWS IMDS IPv6 端点（unique-local，非 link-local）
	// 只拦精确地址；fd00::/8 整段放行以支持自托管 IPv6 ULA 主场景。
	blocked := []string{"100.100.100.200", "100.96.0.2", "fd00:ec2::254"}
	for _, s := range blocked {
		if ip.Equal(net.ParseIP(s)) {
			return true
		}
	}
	return false
}

// dialContextSSRF 在拨号前二次校验目标 IP，防止 DNS 重绑定绕过创建时校验。
func dialContextSSRF(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			last = fmt.Errorf("%w: %s", errEndpointBlocked, ipa.IP)
			continue
		}
		var target = net.JoinHostPort(ipa.IP.String(), port)
		conn, err := d.DialContext(ctx, network, target)
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = errEndpointBlocked
	}
	return nil, last
}

func checkRedirectDenied(_ *http.Request, _ []*http.Request) error {
	return errRedirectDenied
}
