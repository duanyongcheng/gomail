// Package mailnet provides verified, explicitly routed mail protocol connections.
package mailnet

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"strconv"
	"strings"
)

const (
	maxHostBytes        = 254 // A DNS name may include a final dot.
	maxRootPEMBytes     = 1 << 20
	maxRootCertificates = 128
)

type Proxy struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type ProxySetting struct {
	Mode  string `json:"mode"`
	Proxy *Proxy `json:"proxy"`
}

func ResolveProxy(global *Proxy, account ProxySetting) (*Proxy, error) {
	switch account.Mode {
	case "", "inherit":
		if account.Proxy != nil {
			return nil, errors.New("inherited proxy must not include custom settings")
		}
	case "disabled":
		if account.Proxy != nil {
			return nil, errors.New("disabled proxy must not include custom settings")
		}
		return nil, nil
	case "custom":
		if account.Proxy == nil {
			return nil, errors.New("custom proxy settings required")
		}
		global = account.Proxy
	default:
		return nil, errors.New("invalid proxy mode")
	}
	if global == nil {
		return nil, nil
	}
	if err := global.validate(); err != nil {
		return nil, err
	}
	copy := *global
	return &copy, nil
}

func (p Proxy) validate() error {
	if p.Scheme != "socks5" && p.Scheme != "http" {
		return errors.New("unsupported proxy scheme")
	}
	if !validHost(p.Host) || !validPort(p.Port) {
		return errors.New("invalid proxy address")
	}
	return nil
}

type Endpoint struct {
	Host      string
	Port      int
	Security  string
	RootCAPEM string
}

func (e Endpoint) Validate() error {
	if !validHost(e.Host) || !validPort(e.Port) {
		return errors.New("invalid endpoint address")
	}
	if e.Security != "tls" && e.Security != "starttls" {
		return errors.New("verified TLS or STARTTLS required")
	}
	_, err := e.rootPool()
	return err
}

func (e Endpoint) TLSConfig() (*tls.Config, error) {
	if !validHost(e.Host) || !validPort(e.Port) {
		return nil, errors.New("invalid endpoint address")
	}
	if e.Security != "tls" && e.Security != "starttls" {
		return nil, errors.New("verified TLS or STARTTLS required")
	}
	roots, err := e.rootPool()
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: e.Host, RootCAs: roots}, nil
}

func (e Endpoint) rootPool() (*x509.CertPool, error) {
	if e.RootCAPEM == "" {
		return nil, nil
	}
	if len(e.RootCAPEM) > maxRootPEMBytes {
		return nil, errors.New("root certificate PEM too large")
	}
	rest := bytes.TrimSpace([]byte(e.RootCAPEM))
	var certificates []*x509.Certificate
	for len(rest) > 0 {
		// pem.Decode otherwise skips arbitrary text preceding a PEM block.
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, errors.New("invalid root certificate PEM")
		}
		endMarker := []byte("-----END CERTIFICATE-----")
		end := bytes.Index(rest, endMarker)
		if end < 0 || len(certificates) >= maxRootCertificates {
			return nil, errors.New("invalid root certificate PEM")
		}
		if bytes.Contains(rest[len("-----BEGIN CERTIFICATE-----"):end], []byte("-----BEGIN")) {
			return nil, errors.New("invalid root certificate PEM")
		}
		end += len(endMarker)
		if newline := bytes.IndexByte(rest[end:], '\n'); newline >= 0 {
			end += newline + 1
		} else {
			end = len(rest)
		}
		// Decode only the first block so malformed blocks cannot be skipped.
		block, remaining := pem.Decode(rest[:end])
		if block == nil || len(remaining) != 0 || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errors.New("invalid root certificate PEM")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid ||
			(certificate.KeyUsage != 0 && certificate.KeyUsage&x509.KeyUsageCertSign == 0) {
			return nil, errors.New("invalid root CA certificate")
		}
		certificates = append(certificates, certificate)
		rest = bytes.TrimSpace(rest[end:])
	}
	if len(certificates) == 0 {
		return nil, errors.New("root certificate PEM is empty")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("system certificate roots unavailable")
	}
	for _, certificate := range certificates {
		roots.AddCert(certificate)
	}
	return roots, nil
}

func validPort(port int) bool { return port >= 1 && port <= 65535 }

// Host fields contain bare IPv6 literals; brackets belong only in host:port addresses.
func validHost(host string) bool {
	if host == "" || len(host) > maxHostBytes {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	name := strings.TrimSuffix(host, ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	// Reject dotted numeric strings which resemble malformed IPv4 addresses.
	if strings.Contains(name, ".") && strings.Trim(name, "0123456789.") == "" {
		return false
	}
	return true
}

func validateAddress(address string) error {
	if len(address) > maxHostBytes+8 {
		return errors.New("invalid target address")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || !validHost(host) || port == "" || strings.Trim(port, "0123456789") != "" {
		return errors.New("invalid target address")
	}
	if strings.HasPrefix(address, "[") && (net.ParseIP(host) == nil || !strings.Contains(host, ":")) {
		return errors.New("invalid target address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || !validPort(n) {
		return errors.New("invalid target address")
	}
	return nil
}

func (e Endpoint) address() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }
