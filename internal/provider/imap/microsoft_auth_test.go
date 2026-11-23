package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"github.com/duanyongcheng/gomail/internal/domain"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMicrosoftIMAPVerifiedTLSExactXOAUTH2NoPasswordFallback(t *testing.T) {
	// Obtain a disposable certificate from the standard local HTTPS fixture;
	// the protocol listener uses the same certificate and explicit trust anchor.
	https := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer https.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: https.TLS.Certificates, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(conn, "* OK [CAPABILITY IMAP4rev1 AUTH=XOAUTH2 SASL-IR] synthetic ready\r\n")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				result <- err
				return
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				result <- fmt.Errorf("bad IMAP command")
				return
			}
			if fields[1] == "CAPABILITY" {
				fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 SASL-IR\r\n%s OK capability\r\n", fields[0])
				continue
			}
			if fields[1] != "AUTHENTICATE" || len(fields) < 3 || fields[2] != "XOAUTH2" {
				result <- fmt.Errorf("unexpected authentication mechanism")
				return
			}
			var encoded string
			if len(fields) > 3 {
				encoded = fields[3]
			} else {
				fmt.Fprint(conn, "+ \r\n")
				encoded, err = r.ReadString('\n')
				if err != nil {
					result <- err
					return
				}
				encoded = strings.TrimSpace(encoded)
			}
			payload, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || string(payload) != "user=sender@example.test\x01auth=Bearer exchange-imap-token\x01\x01" {
				result <- fmt.Errorf("XOAUTH2 frame mismatch")
				return
			}
			fmt.Fprintf(conn, "%s OK authenticated\r\n", fields[0])
			result <- nil
			return
		}
	}()
	host, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	root := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw})
	p := &Provider{Config: domain.ProtocolConfig{IMAP: domain.ProtocolEndpoint{Host: host, Port: port, Security: "tls", RootCAPEM: string(root), Username: "sender@example.test", Password: "never-use-password", AuthMode: "xoauth2"}}, Token: func(context.Context) (string, error) { return "exchange-imap-token", nil }}
	client, close, err := p.client(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = client
	close()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
