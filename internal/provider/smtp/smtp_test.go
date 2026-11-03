package smtp

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailnet"
)

type smtpFixture struct {
	mu              sync.Mutex
	data            int
	rcpts           []string
	raw             string
	auth            string
	rejectRecipient string
	loseReply       bool
	startTLS        bool
	authStatus      int
	tlsConfig       *tls.Config
}

func smtpCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic smtp"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func smtpServer(t *testing.T, fixture *smtpFixture) Config {
	t.Helper()
	cert, roots := smtpCertificate(t)
	fixture.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() { defer workers.Done(); fixture.serve(conn) }()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-stop; workers.Wait() })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	security := "tls"
	if fixture.startTLS {
		security = "starttls"
	}
	return Config{Endpoint: mailnet.Endpoint{Host: "127.0.0.1", Port: n, Security: security, RootCAPEM: roots}, Dialer: mailnet.Dialer{Timeout: time.Second}, Username: "sender@example.invalid", Password: "synthetic-password", Email: "sender@example.invalid", AuthMode: "password"}
}

func (f *smtpFixture) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if !f.startTLS {
		conn = tls.Server(conn, f.tlsConfig)
	}
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = fmt.Fprint(conn, s) }
	write("220 localhost synthetic\r\n")
	secure := !f.startTLS
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "EHLO"):
			if secure {
				write("250-localhost\r\n250 AUTH PLAIN XOAUTH2\r\n")
			} else {
				write("250-localhost\r\n250 STARTTLS\r\n")
			}
		case line == "STARTTLS":
			write("220 begin TLS\r\n")
			conn = tls.Server(conn, f.tlsConfig)
			r = bufio.NewReader(conn)
			secure = true
		case strings.HasPrefix(line, "AUTH "):
			f.mu.Lock()
			f.auth = line
			f.mu.Unlock()
			if !secure {
				write("538 TLS required\r\n")
			} else if f.authStatus != 0 {
				write(fmt.Sprintf("%d synthetic authentication refusal\r\n", f.authStatus))
			} else {
				write("235 authenticated\r\n")
			}
		case strings.HasPrefix(line, "MAIL FROM:"):
			write("250 sender accepted\r\n")
		case strings.HasPrefix(line, "RCPT TO:"):
			f.mu.Lock()
			f.rcpts = append(f.rcpts, line)
			f.mu.Unlock()
			if strings.Contains(line, f.rejectRecipient) && f.rejectRecipient != "" {
				write("550 recipient rejected\r\n")
			} else {
				write("250 recipient accepted\r\n")
			}
		case line == "DATA":
			f.mu.Lock()
			f.data++
			f.mu.Unlock()
			write("354 send data\r\n")
			var body strings.Builder
			for {
				text, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if text == ".\r\n" {
					break
				}
				body.WriteString(text)
			}
			f.mu.Lock()
			f.raw = body.String()
			f.mu.Unlock()
			if f.loseReply {
				return
			}
			write("250 queued\r\n")
		case line == "QUIT":
			write("221 bye\r\n")
			return
		case line == "RSET" || line == "NOOP":
			write("250 ok\r\n")
		default:
			write("500 unsupported\r\n")
		}
	}
}

func TestSMTPDefinitiveAuthFailurePausesOnlySendingResource(t *testing.T) {
	for _, mode := range []string{"password", "xoauth2"} {
		for _, code := range []int{454, 534, 535, 538} {
			t.Run(fmt.Sprintf("%s/%d", mode, code), func(t *testing.T) {
				fixture := &smtpFixture{authStatus: code}
				config := smtpServer(t, fixture)
				config.AuthMode = mode
				config.Token = func(context.Context) (string, error) { return "synthetic-access", nil }
				barriers := 0
				result := New(config).Send(t.Context(), smtpMail(), func() error { barriers++; return nil })
				want := domain.ConnectionConfigurationRequired
				if code == 535 {
					want = domain.ConnectionReauthRequired
				}
				if code == 454 {
					if !result.Retryable || result.AccessState != "" {
						t.Fatal("temporary failure blocked resource", result)
					}
				} else if result.Retryable || result.AccessState != want {
					t.Fatal("definitive failure spent retry budget", result)
				}
				fixture.mu.Lock()
				defer fixture.mu.Unlock()
				if result.Accepted || result.Unknown || barriers != 0 || fixture.data != 0 || len(fixture.rcpts) != 0 {
					t.Fatal("authentication refusal attempted submission", result, barriers)
				}
			})
		}
	}
}

func smtpMail() domain.OutgoingMail {
	return domain.OutgoingMail{Email: "sender@example.invalid", Raw: []byte("From: sender@example.invalid\r\nTo: first@example.invalid\r\nMessage-ID: <synthetic@example.invalid>\r\nSubject: Synthetic\r\n\r\nbody\r\n"), EnvelopeRecipients: []string{"first@example.invalid", "hidden@example.invalid"}}
}

func TestSMTPAcceptedWithoutRemoteIDAndFrozenBccEnvelope(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("starttls=%v", startTLS), func(t *testing.T) {
			fixture := &smtpFixture{startTLS: startTLS}
			transport := New(smtpServer(t, fixture))
			barriers := 0
			result := transport.Send(t.Context(), smtpMail(), func() error { barriers++; return nil })
			if !result.Accepted || result.Unknown || result.RemoteID != "" || barriers != 1 {
				t.Fatalf("result=%+v barriers=%d", result, barriers)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.data != 1 || len(fixture.rcpts) != 2 || !strings.Contains(strings.Join(fixture.rcpts, " "), "hidden@example.invalid") || strings.Contains(strings.ToLower(fixture.raw), "bcc:") {
				t.Fatalf("DATA=%d recipients=%d Bcc privacy failure", fixture.data, len(fixture.rcpts))
			}
		})
	}
}

func TestSMTPPreSubmissionFailuresNeverIssueDATA(t *testing.T) {
	for _, kind := range []string{"partial_rcpt", "barrier", "bcc_header", "sender"} {
		t.Run(kind, func(t *testing.T) {
			fixture := &smtpFixture{}
			if kind == "partial_rcpt" {
				fixture.rejectRecipient = "hidden@example.invalid"
			}
			transport := New(smtpServer(t, fixture))
			mail := smtpMail()
			if kind == "bcc_header" {
				mail.Raw = append([]byte("Bcc: hidden@example.invalid\r\n"), mail.Raw...)
			}
			if kind == "sender" {
				mail.Email = "other@example.invalid"
			}
			result := transport.Send(t.Context(), mail, func() error {
				if kind == "barrier" {
					return errors.New("synthetic SQLite barrier failure")
				}
				return nil
			})
			if result.Accepted || result.Unknown {
				t.Fatalf("known pre-submission failure=%+v", result)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.data != 0 {
				t.Fatalf("DATA issued after %s", kind)
			}
		})
	}
}

func TestSMTPLostFinalReceiptIsUnknownAndNotRetryable(t *testing.T) {
	fixture := &smtpFixture{loseReply: true}
	result := New(smtpServer(t, fixture)).Send(context.Background(), smtpMail(), func() error { return nil })
	if result.Accepted || !result.Unknown || result.Retryable {
		t.Fatalf("lost receipt=%+v", result)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.data != 1 {
		t.Fatalf("DATA=%d", fixture.data)
	}
}

func TestSMTPUntrustedTLSNeverAuthenticates(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("starttls=%v", startTLS), func(t *testing.T) {
			fixture := &smtpFixture{startTLS: startTLS}
			config := smtpServer(t, fixture)
			config.Endpoint.RootCAPEM = ""
			if err := New(config).Test(t.Context()); err == nil {
				t.Fatal("untrusted certificate accepted")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.auth != "" {
				t.Fatal("credentials sent before verified TLS")
			}
		})
	}
}

func TestSMTPXOAUTH2UsesExplicitTokenSource(t *testing.T) {
	fixture := &smtpFixture{}
	config := smtpServer(t, fixture)
	config.AuthMode = "xoauth2"
	config.Password = "must-not-be-used"
	calls := 0
	config.Token = func(context.Context) (string, error) { calls++; return "synthetic-smtp-token", nil }
	if err := New(config).Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	want := "AUTH XOAUTH2 " + base64.StdEncoding.EncodeToString([]byte("user=sender@example.invalid\x01auth=Bearer synthetic-smtp-token\x01\x01"))
	if calls != 1 || fixture.auth != want {
		t.Fatal("SMTP did not use exact XOAUTH2 framing")
	}
}

func TestSMTPStaleConfigurationFencePreventsBarrierAndDATA(t *testing.T) {
	fixture := &smtpFixture{}
	config := smtpServer(t, fixture)
	fences, barriers := 0, 0
	config.BeforeRequest = func(context.Context) error {
		fences++
		if fences == 2 {
			return errors.New("synthetic stale configuration")
		}
		return nil
	}
	result := New(config).Send(t.Context(), smtpMail(), func() error { barriers++; return nil })
	if result.Accepted || result.Unknown || fences != 2 || barriers != 0 {
		t.Fatalf("result=%+v fences=%d barriers=%d", result, fences, barriers)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.data != 0 {
		t.Fatal("stale configuration issued DATA")
	}
}

func TestSMTPTokenAccessErrorRetainsResourceStateBeforeSubmission(t *testing.T) {
	for _, state := range []domain.ConnectionState{domain.ConnectionReauthRequired, domain.ConnectionPermissionRequired, domain.ConnectionCredentialsUnavailable, domain.ConnectionConfigurationRequired} {
		t.Run(string(state), func(t *testing.T) {
			fixture := &smtpFixture{}
			config := smtpServer(t, fixture)
			config.AuthMode = "xoauth2"
			config.Token = func(context.Context) (string, error) {
				return "", fmt.Errorf("synthetic secret must not escape: %w", &domain.AccessError{State: state})
			}
			barriers := 0
			result := New(config).Send(t.Context(), smtpMail(), func() error { barriers++; return nil })
			if result.AccessState != state || result.Retryable || result.Unknown || result.Accepted || barriers != 0 || strings.Contains(result.Message, "synthetic secret") {
				t.Fatalf("result=%+v barriers=%d", result, barriers)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.data != 0 || fixture.auth != "" {
				t.Fatal("terminal token error submitted protocol credentials or DATA")
			}
		})
	}
}
