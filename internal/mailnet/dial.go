package mailnet

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const defaultTimeout = 15 * time.Second

type Dialer struct {
	Proxy   *Proxy
	Timeout time.Duration
}

func (d Dialer) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return defaultTimeout
}

func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, errors.New("unsupported network")
	}
	if err := validateAddress(address); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout())
	defer cancel()
	direct := &net.Dialer{Timeout: d.timeout()}
	if d.Proxy == nil {
		conn, err := direct.DialContext(ctx, network, address)
		return dialResult(ctx, conn, err)
	}
	p := *d.Proxy
	if err := p.validate(); err != nil {
		return nil, err
	}
	proxyAddress := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	if p.Scheme == "socks5" {
		socks, err := proxy.SOCKS5("tcp", proxyAddress, nil, socksForward{direct})
		if err != nil {
			return nil, errors.New("proxy setup failed")
		}
		conn, err := socks.(proxy.ContextDialer).DialContext(ctx, network, address)
		return dialResult(ctx, conn, err)
	}
	conn, err := direct.DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return dialResult(ctx, conn, err)
	}
	finish := watchHandshake(ctx, conn)
	tunnel, err := connectTunnel(conn, address)
	if finishErr := finish(); err == nil {
		err = finishErr
	}
	return dialResult(ctx, tunnel, err)
}

type socksForward struct{ *net.Dialer }

func (d socksForward) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d socksForward) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.Dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &noAuthSOCKSConn{Conn: conn, reader: bufio.NewReader(conn)}, nil
}

type noAuthSOCKSConn struct {
	net.Conn
	reader  *bufio.Reader
	checked bool
}

func (c *noAuthSOCKSConn) Read(b []byte) (int, error) {
	if !c.checked {
		// x/net accepts an unoffered auth method when its Authenticate hook is nil.
		// Enforce this slice's no-auth policy before its SOCKS state machine proceeds.
		greeting, err := c.reader.Peek(2)
		if err != nil {
			return 0, err
		}
		if greeting[0] != 5 || greeting[1] != 0 {
			return 0, errors.New("unsupported SOCKS authentication response")
		}
		c.checked = true
	}
	return c.reader.Read(b)
}

func dialResult(ctx context.Context, conn net.Conn, err error) (net.Conn, error) {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("network connection or handshake failed")
	}
	return conn, nil
}

// finish joins an already-running cancellation callback before clearing deadlines.
// The caller owns command cancellation after a successful handshake.
func watchHandshake(ctx context.Context, conn net.Conn) func() error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		close(done)
	})
	return func() error {
		if !stop() {
			<-done
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return conn.SetDeadline(time.Time{})
	}
}

func (d Dialer) DialTLS(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Security != "tls" {
		return nil, errors.New("implicit TLS endpoint required")
	}
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout())
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", endpoint.address())
	if err != nil {
		return nil, err
	}
	return d.handshake(ctx, conn, endpoint)
}

// UpgradeTLS requires the provider to have successfully negotiated STARTTLS/STLS.
// On any error, including invalid configuration, it closes the supplied connection.
func (d Dialer) UpgradeTLS(ctx context.Context, conn net.Conn, endpoint Endpoint) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("TLS connection required")
	}
	if endpoint.Security != "starttls" {
		_ = conn.Close()
		return nil, errors.New("STARTTLS endpoint required")
	}
	return d.handshake(ctx, conn, endpoint)
}

func (d Dialer) handshake(ctx context.Context, conn net.Conn, endpoint Endpoint) (net.Conn, error) {
	config, err := endpoint.TLSConfig()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout())
	defer cancel()
	finish := watchHandshake(ctx, conn)
	secure := tls.Client(conn, config)
	err = secure.HandshakeContext(ctx)
	if finishErr := finish(); err == nil {
		err = finishErr
	}
	return dialResult(ctx, secure, err)
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

func connectTunnel(conn net.Conn, address string) (net.Conn, error) {
	if _, err := io.WriteString(conn, "CONNECT "+address+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n"); err != nil {
		return conn, errors.New("proxy request failed")
	}
	reader := bufio.NewReaderSize(conn, 4096)
	var header bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil || len(line) < 2 || !bytes.HasSuffix(line, []byte("\r\n")) || header.Len()+len(line) > 32*1024 {
			return conn, errors.New("invalid proxy response headers")
		}
		if header.Len() == 0 {
			status := strings.SplitN(string(line[:len(line)-2]), " ", 3)
			if len(status) != 3 || (status[0] != "HTTP/1.1" && status[0] != "HTTP/1.0") ||
				len(status[1]) != 3 || strings.Trim(status[1], "0123456789") != "" || !validHeaderValue(status[2]) {
				return conn, errors.New("invalid proxy response status")
			}
		} else if len(line) != 2 && !validHeaderLine(line[:len(line)-2]) {
			return conn, errors.New("invalid proxy response headers")
		}
		header.Write(line)
		if len(line) == 2 {
			break
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header.Bytes())), &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return conn, errors.New("proxy tunnel rejected")
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

func validHeaderLine(line []byte) bool {
	colon := bytes.IndexByte(line, ':')
	if colon <= 0 {
		return false
	}
	for _, c := range line[:colon] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
			return false
		}
	}
	return validHeaderValue(string(line[colon+1:]))
}

func validHeaderValue(value string) bool {
	for _, c := range []byte(value) {
		if (c < 32 && c != '\t') || c == 127 {
			return false
		}
	}
	return true
}

// HTTPTransport snapshots explicit routing. HTTP clients must separately disable
// redirects (CheckRedirect) before attaching credentials or OAuth tokens.
func (d Dialer) HTTPTransport() *http.Transport {
	if d.Proxy != nil {
		copy := *d.Proxy
		d.Proxy = &copy
	}
	return &http.Transport{
		Proxy: nil, DialContext: d.DialContext,
		DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if err := validateAddress(address); err != nil {
				return nil, err
			}
			host, port, _ := net.SplitHostPort(address)
			n, _ := strconv.Atoi(port)
			return d.DialTLS(ctx, Endpoint{Host: host, Port: n, Security: "tls"})
		},
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: d.timeout(), ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout: 90 * time.Second, ExpectContinueTimeout: time.Second,
		MaxIdleConns: 32, MaxIdleConnsPerHost: 4, MaxResponseHeaderBytes: 1 << 20,
	}
}
