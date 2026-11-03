package mailnet

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func serve(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				handler(conn)
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); wg.Wait() })
	return listener.Addr().String()
}

func proxyAt(address, scheme string) *Proxy {
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	return &Proxy{Scheme: scheme, Host: host, Port: n}
}

func readConnect(conn net.Conn) (string, error) {
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return "", err
	}
	if request.Method != "CONNECT" || request.Host != request.RequestURI {
		return "", errors.New("invalid CONNECT target")
	}
	return request.Host, nil
}

func echo(conn net.Conn) { _, _ = io.Copy(conn, conn) }

func TestDirectIgnoresEnvironmentAndReleasesCancellation(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "http://127.0.0.1:1")
	}
	address := serve(t, echo)
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := (Dialer{Timeout: 50 * time.Millisecond}).DialContext(ctx, "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	time.Sleep(75 * time.Millisecond)
	assertEcho(t, conn)
	if _, err := (Dialer{}).DialContext(context.Background(), "udp", address); err == nil {
		t.Fatal("UDP accepted")
	}
}

func assertEcho(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	var data [4]byte
	if _, err := io.ReadFull(conn, data[:]); err != nil || string(data[:]) != "ping" {
		t.Fatalf("echo failed: %q %v", data, err)
	}
}

func TestCONNECTBufferedTunnelAndWatcherRelease(t *testing.T) {
	target := "mail.example:993"
	observed := make(chan string, 1)
	address := serve(t, func(conn net.Conn) {
		actual, err := readConnect(conn)
		if err != nil {
			return
		}
		observed <- actual
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Established\r\nX-Proxy: local\r\n\r\nhello")
		echo(conn)
	})
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := (Dialer{Proxy: proxyAt(address, "http"), Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := <-observed; got != target {
		t.Fatalf("target: %q", got)
	}
	cancel()
	time.Sleep(125 * time.Millisecond)
	var greeting [5]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil || string(greeting[:]) != "hello" {
		t.Fatalf("buffered tunnel bytes lost: %q %v", greeting, err)
	}
	assertEcho(t, conn)
}

func TestCONNECTRejectsInvalidResponsesAndCloses(t *testing.T) {
	replies := []string{
		"HTTP/1.1 407 password-secret\r\n\r\n", "HTTP/1.1 500 rejected\r\n\r\n",
		"HTTP/1.1 200 OK\r\n", "HTTP/1.1 200 OK\n\n", "HTTP/1.1 20 OK\r\n\r\n",
		"HTTP/2 200 OK\r\n\r\n", "HTTP/1.1 200 OK\r\nBad Header: secret\r\n\r\n",
		"HTTP/1.1 200 OK\r\n folded: value\r\n\r\n", "HTTP/1.1 200 " + strings.Repeat("s", 5000) + "\r\n\r\n",
		"HTTP/1.1 200 OK\r\n" + strings.Repeat("X-Header: "+strings.Repeat("x", 990)+"\r\n", 40) + "\r\n",
		"HTTP/1.1 200 O\x00K\r\n\r\n", "HTTP/1.1 200 OK\r\nX-Test: a\x7fb\r\n\r\n",
		"HTTP/1.1 200 OK\r\nX-Test: a\rb\r\n\r\n", "HTTP/1.1 200 OK\r\n: empty\r\n\r\n",
		"HTTP/1.1 200 OK\r\nX-Test : value\r\n\r\n",
		"HTTP/1.1 200 OK\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\n",
	}
	for i, reply := range replies {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			closed := make(chan struct{})
			address := serve(t, func(conn net.Conn) {
				defer close(closed)
				if _, err := readConnect(conn); err != nil {
					return
				}
				_, _ = io.WriteString(conn, reply)
				if i == 2 {
					return // Deliberately truncated headers require an EOF.
				}
				_, _ = io.Copy(io.Discard, conn)
			})
			conn, err := (Dialer{Proxy: proxyAt(address, "http"), Timeout: time.Second}).DialContext(context.Background(), "tcp", "mail.example:993")
			if err == nil || conn != nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "mail.example") {
				t.Fatalf("invalid or unsanitized result: %v %v", conn, err)
			}
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("failed tunnel left connection open")
			}
		})
	}
}

func TestCONNECTExactHeaderLimit(t *testing.T) {
	for _, size := range []int{32 * 1024, 32*1024 + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			reply := "HTTP/1.1 200 OK\r\n"
			for size-len(reply)-2 > 4096 {
				reply += "X: " + strings.Repeat("x", 4091) + "\r\n"
			}
			reply += "X: " + strings.Repeat("x", size-len(reply)-7) + "\r\n\r\n"
			if len(reply) != size {
				t.Fatal("incorrect header fixture size")
			}
			address := serve(t, func(conn net.Conn) {
				if _, err := readConnect(conn); err != nil {
					return
				}
				_, _ = io.WriteString(conn, reply+"hello")
			})
			conn, err := (Dialer{Proxy: proxyAt(address, "http"), Timeout: time.Second}).DialContext(context.Background(), "tcp", "mail.test:993")
			if size > 32*1024 {
				if err == nil || conn != nil {
					t.Fatal("oversized headers accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			data, err := io.ReadAll(conn)
			if err != nil || string(data) != "hello" {
				t.Fatalf("tunnel bytes at header boundary: %q %v", data, err)
			}
		})
	}
}

type blockingCloseConn struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
}

func (c *blockingCloseConn) Close() error {
	close(c.entered)
	<-c.release
	return c.Conn.Close()
}

func TestHandshakeFinishJoinsRunningCancellation(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	defer left.Close()
	conn := &blockingCloseConn{Conn: left, entered: make(chan struct{}), release: make(chan struct{})}
	defer close(conn.release)
	ctx, cancel := context.WithCancel(context.Background())
	finish := watchHandshake(ctx, conn)
	cancel()
	select {
	case <-conn.entered:
	case <-time.After(time.Second):
		t.Fatal("cancellation callback did not start")
	}
	finished := make(chan error, 1)
	go func() { finished <- finish() }()
	select {
	case err := <-finished:
		t.Fatalf("finish returned before callback completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	conn.release <- struct{}{}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("finish did not join completed callback")
	}
}

func TestHandshakeFinishCancellationRace(t *testing.T) {
	for i := 0; i < 200; i++ {
		left, right := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		finish := watchHandshake(ctx, left)
		cancelled := make(chan struct{})
		go func() { cancel(); close(cancelled) }()
		err := finish()
		<-cancelled
		if err == nil {
			// A stopped callback must not close or poison a reused connection.
			if err := left.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("successful handshake connection closed: %v", err)
			}
		} else if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected finish error: %v", err)
		}
		_ = left.Close()
		_ = right.Close()
	}
}

func socksRequest(conn net.Conn) (string, error) {
	var greeting [3]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil || greeting != [3]byte{5, 1, 0} {
		return "", errors.New("bad greeting")
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	var head [5]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 5 || head[1] != 1 || head[2] != 0 || head[3] != 3 {
		return "", errors.New("expected remote DNS request")
	}
	data := make([]byte, int(head[4])+2)
	if _, err := io.ReadFull(conn, data); err != nil {
		return "", err
	}
	return net.JoinHostPort(string(data[:len(data)-2]), strconv.Itoa(int(binary.BigEndian.Uint16(data[len(data)-2:])))), nil
}

func TestSOCKSRemoteDNSAndWatcherRelease(t *testing.T) {
	observed := make(chan string, 1)
	address := serve(t, func(conn net.Conn) {
		target, err := socksRequest(conn)
		if err != nil {
			return
		}
		observed <- target
		_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		echo(conn)
	})
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := (Dialer{Proxy: proxyAt(address, "socks5"), Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", "unresolvable.invalid:993")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := <-observed; got != "unresolvable.invalid:993" {
		t.Fatalf("remote target: %q", got)
	}
	cancel()
	time.Sleep(125 * time.Millisecond)
	assertEcho(t, conn)
}

func TestProxyCancellationTimeoutAndNoFallback(t *testing.T) {
	var directCalls atomic.Int32
	target := serve(t, func(conn net.Conn) { directCalls.Add(1) })
	for _, scheme := range []string{"http", "socks5"} {
		for _, mode := range []string{"cancel", "timeout", "reject"} {
			t.Run(scheme+"/"+mode, func(t *testing.T) {
				started := make(chan struct{})
				address := serve(t, func(conn net.Conn) {
					close(started)
					if mode != "reject" {
						_, _ = io.Copy(io.Discard, conn)
						return
					}
					if scheme == "http" {
						_, _ = readConnect(conn)
						_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\n\r\n")
					} else {
						var greeting [3]byte
						_, _ = io.ReadFull(conn, greeting[:])
						_, _ = conn.Write([]byte{5, 255})
					}
				})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "cancel" {
					go func() { <-started; cancel() }()
				}
				begin := time.Now()
				conn, err := (Dialer{Proxy: proxyAt(address, scheme), Timeout: 75 * time.Millisecond}).DialContext(ctx, "tcp", target)
				if err == nil || conn != nil || time.Since(begin) > time.Second {
					t.Fatalf("proxy failure/cancellation: %v %v", conn, err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			})
		}
	}
	if directCalls.Load() != 0 {
		t.Fatal("proxy failure fell back to direct")
	}
}

func TestSOCKSMalformedReplies(t *testing.T) {
	for _, reply := range [][]byte{{4, 0}, {5, 2}, {5}, {5, 0, 0, 1}, {5, 0, 0, 255, 0, 0}, {5, 1, 0, 1, 127, 0, 0, 1, 0, 1}} {
		address := serve(t, func(conn net.Conn) {
			if len(reply) <= 2 {
				var greeting [3]byte
				_, _ = io.ReadFull(conn, greeting[:])
			} else if _, err := socksRequest(conn); err != nil {
				return
			}
			_, _ = conn.Write(reply)
		})
		conn, err := (Dialer{Proxy: proxyAt(address, "socks5"), Timeout: time.Second}).DialContext(context.Background(), "tcp", "remote.invalid:993")
		if err == nil || conn != nil {
			t.Fatalf("invalid SOCKS response accepted: %v %v", conn, err)
		}
	}
}

type certificateFixture struct {
	root   string
	server tls.Certificate
}

func certificates(t *testing.T) certificateFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"mail.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return certificateFixture{root: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})), server: tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}}
}

func TestVerifiedTLSAndTargetSNI(t *testing.T) {
	fixture := certificates(t)
	sni := make(chan string, 8)
	tlsAddress := serve(t, func(conn net.Conn) {
		secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{fixture.server}, MinVersion: tls.VersionTLS12, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni <- hello.ServerName
			return nil, nil
		}})
		defer secure.Close()
		echo(secure)
	})
	// CONNECT routes a DNS target to the local TLS fixture without local DNS.
	proxyAddress := serve(t, func(conn net.Conn) {
		_, err := readConnect(conn)
		if err != nil {
			return
		}
		upstream, err := net.Dial("tcp", tlsAddress)
		if err != nil {
			return
		}
		defer upstream.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, conn); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(conn, upstream)
		_ = conn.Close()
		<-done
	})
	d := Dialer{Proxy: proxyAt(proxyAddress, "http"), Timeout: time.Second}
	for _, mode := range []string{"trusted", "untrusted", "wrong-host", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			e := Endpoint{Host: "mail.test", Port: 993, Security: "tls", RootCAPEM: fixture.root}
			if mode == "untrusted" {
				e.RootCAPEM = ""
			}
			if mode == "wrong-host" {
				e.Host = "wrong.test"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var conn net.Conn
			var err error
			if mode == "starttls" {
				e.Security = "starttls"
				var raw net.Conn
				raw, err = d.DialContext(ctx, "tcp", e.address())
				if err == nil {
					conn, err = d.UpgradeTLS(ctx, raw, e)
				}
			} else {
				conn, err = d.DialTLS(ctx, e)
			}
			if mode == "untrusted" || mode == "wrong-host" {
				if err == nil || conn != nil {
					t.Fatal("unverified certificate accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				cancel()
				assertEcho(t, conn)
			}
			if got := <-sni; got != e.Host {
				t.Fatalf("target SNI: %q", got)
			}
		})
	}
}

func TestTLSCancellationDeadlinesAndInvalidUpgrade(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "context-deadline"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			address := serve(t, func(conn net.Conn) { close(started); _, _ = io.Copy(io.Discard, conn) })
			p := proxyAt(address, "http")
			e := Endpoint{Host: p.Host, Port: p.Port, Security: "tls"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				go func() { <-started; cancel() }()
			}
			if mode == "context-deadline" {
				var end context.CancelFunc
				ctx, end = context.WithTimeout(ctx, 25*time.Millisecond)
				defer end()
			}
			begin := time.Now()
			conn, err := (Dialer{Timeout: 75 * time.Millisecond}).DialTLS(ctx, e)
			if err == nil || conn != nil || time.Since(begin) > time.Second {
				t.Fatalf("TLS cancellation/deadline: %v %v", conn, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost TLS cancellation: %v", err)
			}
		})
	}
	left, right := net.Pipe()
	defer right.Close()
	if _, err := (Dialer{}).UpgradeTLS(context.Background(), left, Endpoint{Host: "localhost", Port: 443, Security: "tls"}); err == nil {
		t.Fatal("upgrade accepted implicit TLS endpoint")
	}
	if _, err := right.Write([]byte("x")); err == nil {
		t.Fatal("invalid upgrade did not close connection")
	}
	if _, err := (Dialer{}).DialTLS(context.Background(), Endpoint{Host: "localhost", Port: 443, Security: "starttls"}); err == nil {
		t.Fatal("implicit TLS accepted STARTTLS endpoint")
	}
}

func TestHTTPTransportRouting(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		t.Setenv(name, "http://127.0.0.1:1")
	}
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "direct")
	}))
	defer origin.Close()
	transport := (Dialer{}).HTTPTransport()
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unsafe transport defaults")
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	for _, scheme := range []string{"http", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			observed := make(chan string, 1)
			address := serve(t, func(conn net.Conn) {
				var target string
				var err error
				if scheme == "http" {
					target, err = readConnect(conn)
					if err == nil {
						_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n")
					}
				} else {
					target, err = socksRequest(conn)
					if err == nil {
						_, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
					}
				}
				if err != nil {
					return
				}
				observed <- target
				if _, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nproxied")
				}
			})
			policy := proxyAt(address, scheme)
			tr := (Dialer{Proxy: policy, Timeout: time.Second}).HTTPTransport()
			defer tr.CloseIdleConnections()
			policy.Host = "invalid.invalid" // Transport owns a snapshot.
			c := &http.Client{Transport: tr}
			resp, err := c.Get("http://remote.invalid/resource")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil || string(body) != "proxied" || <-observed != "remote.invalid:80" {
				t.Fatal("HTTP transport ignored resolved route")
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected direct requests")
	}
}

func TestHTTPTransportProxyFailureNeverReachesOrigin(t *testing.T) {
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	for _, scheme := range []string{"http", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			address := serve(t, func(conn net.Conn) {
				if scheme == "http" {
					_, _ = readConnect(conn)
					_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\n\r\n")
				} else {
					var greeting [3]byte
					_, _ = io.ReadFull(conn, greeting[:])
					_, _ = conn.Write([]byte{5, 255})
				}
			})
			tr := (Dialer{Proxy: proxyAt(address, scheme), Timeout: time.Second}).HTTPTransport()
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, Timeout: time.Second}
			resp, err := client.Post(origin.URL, "text/plain", strings.NewReader("write"))
			if err == nil {
				_ = resp.Body.Close()
				t.Fatal("rejected proxy request unexpectedly succeeded")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("HTTP proxy failure fell back to origin")
	}
}

func TestHTTPTransportDoesNotRetryPOST(t *testing.T) {
	var requests atomic.Int32
	address := serve(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		_ = request.Body.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		request, err = http.ReadRequest(reader)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		requests.Add(1)
		// The server has applied the write, but loses the receipt.
	})
	tr := (Dialer{Timeout: time.Second}).HTTPTransport()
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: time.Second}
	resp, err := c.Get("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	resp, err = c.Post("http://"+address, "text/plain", strings.NewReader("write"))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("lost receipt unexpectedly succeeded")
	}
	if requests.Load() != 1 {
		t.Fatalf("non-idempotent write count: %d", requests.Load())
	}
}

func TestHTTPTransportHTTPSUsesProxyAndVerifiesTarget(t *testing.T) {
	fixture := certificates(t)
	for _, scheme := range []string{"http", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			observed := make(chan string, 1)
			sni := make(chan string, 1)
			address := serve(t, func(conn net.Conn) {
				var target string
				var err error
				if scheme == "http" {
					target, err = readConnect(conn)
					if err == nil {
						_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n")
					}
				} else {
					target, err = socksRequest(conn)
					if err == nil {
						_, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
					}
				}
				if err != nil {
					return
				}
				observed <- target
				secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{fixture.server}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
					sni <- hello.ServerName
					return nil, nil
				}})
				_ = secure.Handshake()
			})
			tr := (Dialer{Proxy: proxyAt(address, scheme), Timeout: time.Second}).HTTPTransport()
			defer tr.CloseIdleConnections()
			c := &http.Client{Transport: tr, Timeout: 2 * time.Second}
			resp, err := c.Get("https://mail.test/resource")
			if err == nil {
				_ = resp.Body.Close()
				t.Fatal("HTTP transport accepted an untrusted certificate")
			}
			if <-observed != "mail.test:443" || <-sni != "mail.test" {
				t.Fatal("HTTPS routing or SNI contract violated")
			}
		})
	}
}
