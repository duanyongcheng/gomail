package mailnet

import (
	"crypto/tls"
	"encoding/pem"
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	for _, host := range []string{"mail.example", "localhost", "mail.example.", "127.0.0.1", "::1", "2001:db8::1"} {
		if err := (Endpoint{Host: host, Port: 443, Security: "tls"}).Validate(); err != nil {
			t.Errorf("valid host %q: %v", host, err)
		}
	}
	for _, host := range []string{"", "https://mail.example", "user@mail.example", "mail.example/a", "mail.example:443", "mail example", "mail\nexample", "[::1]", "[::1", "::1]", "a..b", "-a.b", "a-.b", "a_b", "999.1.1.1", strings.Repeat("a", 64) + ".com", "a%zone"} {
		if err := (Endpoint{Host: host, Port: 443, Security: "tls"}).Validate(); err == nil {
			t.Errorf("invalid host accepted: %q", host)
		}
		if _, err := ResolveProxy(&Proxy{Scheme: "http", Host: host, Port: 8080}, ProxySetting{}); err == nil {
			t.Errorf("invalid proxy host accepted: %q", host)
		}
	}
	for _, port := range []int{-1, 0, 65536} {
		if err := (Endpoint{Host: "localhost", Port: port, Security: "tls"}).Validate(); err == nil {
			t.Errorf("invalid port accepted: %d", port)
		}
	}
	for _, security := range []string{"", "plain", "TLS", "optional"} {
		if _, err := (Endpoint{Host: "localhost", Port: 443, Security: security}).TLSConfig(); err == nil {
			t.Errorf("invalid security accepted: %q", security)
		}
	}
	for _, address := range []string{"localhost:0", "localhost:65536", "localhost:+443", "localhost:https", "[localhost]:443", "user@host:443", "[::1:443", "localhost:443/path"} {
		// Bracketed DNS names are rejected below, as brackets are IPv6 syntax only.
		if err := validateAddress(address); err == nil {
			t.Errorf("invalid target accepted: %q", address)
		}
	}
}

func TestProxyResolution(t *testing.T) {
	global := &Proxy{Scheme: "socks5", Host: "global.example", Port: 1080}
	custom := &Proxy{Scheme: "http", Host: "account.example", Port: 8080}
	for _, mode := range []string{"", "inherit"} {
		resolved, err := ResolveProxy(global, ProxySetting{Mode: mode})
		if err != nil || resolved == global || *resolved != *global {
			t.Fatalf("inherit: %v %v", resolved, err)
		}
		resolved.Host = "changed"
		if global.Host != "global.example" {
			t.Fatal("global policy aliased")
		}
	}
	resolved, err := ResolveProxy(global, ProxySetting{Mode: "custom", Proxy: custom})
	if err != nil || resolved == custom || *resolved != *custom {
		t.Fatalf("custom: %v %v", resolved, err)
	}
	resolved.Port = 1
	if custom.Port != 8080 {
		t.Fatal("custom policy aliased")
	}
	for _, mode := range []string{"", "inherit", "disabled"} {
		if p, err := ResolveProxy(nil, ProxySetting{Mode: mode}); err != nil || p != nil {
			t.Fatalf("direct: %v %v", p, err)
		}
	}
	if p, err := ResolveProxy(&Proxy{}, ProxySetting{Mode: "disabled"}); err != nil || p != nil {
		t.Fatal("disabled must ignore global policy")
	}
	for _, setting := range []ProxySetting{{Mode: "unknown"}, {Mode: "custom"}, {Mode: "custom", Proxy: &Proxy{}}, {Mode: "inherit", Proxy: custom}, {Mode: "disabled", Proxy: custom}, {Proxy: custom}, {Mode: "custom", Proxy: &Proxy{Scheme: "https", Host: "localhost", Port: 443}}} {
		if _, err := ResolveProxy(global, setting); err == nil {
			t.Errorf("invalid setting accepted: %#v", setting)
		}
	}
	if _, err := ResolveProxy(&Proxy{}, ProxySetting{}); err == nil {
		t.Fatal("invalid inherited policy accepted")
	}
}

func TestRootPEMAndConfigIsolation(t *testing.T) {
	fixture := certificates(t)
	e := Endpoint{Host: "mail.test", Port: 443, Security: "starttls", RootCAPEM: fixture.root}
	a, err := e.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a.RootCAs == b.RootCAs || a.ServerName != "mail.test" || a.MinVersion != tls.VersionTLS12 || a.InsecureSkipVerify {
		t.Fatal("TLS configuration contract violated")
	}
	a.ServerName = "changed"
	if b.ServerName != "mail.test" {
		t.Fatal("TLS config aliases")
	}
	defaultConfig, err := (Endpoint{Host: "localhost", Port: 443, Security: "tls"}).TLSConfig()
	if err != nil || defaultConfig.RootCAs != nil {
		t.Fatal("default should use system roots")
	}
	for _, raw := range []string{" ", "garbage", "garbage\n" + fixture.root, fixture.root + "garbage", "-----BEGIN CERTIFICATE-----\nbad\n-----END CERTIFICATE-----", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("secret")})), string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.server.Certificate[0]}))} {
		e.RootCAPEM = raw
		if _, err := e.TLSConfig(); err == nil {
			t.Errorf("malformed/non-CA PEM accepted")
		}
	}
}

func TestRootPEMRejectsSkippedBlocksAndBoundsInput(t *testing.T) {
	root := certificates(t).root
	for name, raw := range map[string]string{
		"malformed then valid":    "-----BEGIN CERTIFICATE-----\nbad\n-----END CERTIFICATE-----\n" + root,
		"unterminated then valid": "-----BEGIN CERTIFICATE-----\nbad\n" + root,
		"nested begin":            "-----BEGIN CERTIFICATE-----\n" + root,
		"missing block separator": strings.TrimSuffix(root, "\n") + root,
		"junk on footer line":     strings.TrimSuffix(root, "\n") + "junk\n" + root,
		"oversized whitespace":    strings.Repeat(" ", maxRootPEMBytes+1),
		"oversized bundle":        root + strings.Repeat(" ", maxRootPEMBytes),
		"too many certificates":   strings.Repeat(root, maxRootCertificates+1),
	} {
		t.Run(name, func(t *testing.T) {
			e := Endpoint{Host: "mail.test", Port: 443, Security: "tls", RootCAPEM: raw}
			if err := e.Validate(); err == nil {
				t.Fatal("invalid CA input accepted by Validate")
			}
			if _, err := e.TLSConfig(); err == nil {
				t.Fatal("invalid CA input accepted by TLSConfig")
			}
		})
	}
	for _, raw := range []string{
		strings.TrimSuffix(root, "\n"),
		root + strings.Repeat(" ", maxRootPEMBytes-len(root)),
		strings.Repeat(root, maxRootCertificates),
		strings.ReplaceAll(root, "\n", "\r\n"),
	} {
		if _, err := (Endpoint{Host: "mail.test", Port: 443, Security: "tls", RootCAPEM: raw}).TLSConfig(); err != nil {
			t.Fatalf("valid bounded CA input rejected: %v", err)
		}
	}
}

func TestOversizedAddressInputs(t *testing.T) {
	host := strings.Repeat("a", 1<<20)
	if validHost(host) {
		t.Fatal("oversized host accepted")
	}
	for _, address := range []string{host + ":443", "localhost:" + strings.Repeat("0", 1<<20) + "443"} {
		if err := validateAddress(address); err == nil {
			t.Fatal("oversized address accepted")
		}
	}
	if _, err := ResolveProxy(&Proxy{Scheme: "http", Host: host, Port: 8080}, ProxySetting{}); err == nil {
		t.Fatal("oversized proxy host accepted")
	}
}
