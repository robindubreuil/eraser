package email

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robindubreuil/eraser/internal/config"
)

func TestSendWithTLS_ConnectionError(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     1,
		UseTLS:   true,
		Username: "u",
		Password: "p",
	}, "sender@example.com")

	result := s.Send(context.Background(), Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "Test",
		Body:    "body",
	})
	if result.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(result.Error.Error(), "SMTP") {
		t.Errorf("error = %q, expected SMTP-related error", result.Error.Error())
	}
}

func TestSendWithTLS_TLSConnectionFailed(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     1,
		UseTLS:   true,
		Username: "u",
		Password: "p",
	}, "sender@example.com")

	result := s.Send(context.Background(), Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "TLS Test",
		Body:    "tls body",
	})
	if result.Success {
		t.Fatal("expected failure for TLS to port 1")
	}
}

func TestSMTPSender_Send_NoAuthNoTLS(t *testing.T) {
	srv := newMockSMTPServer(t)
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(portStr)

	s := NewSMTPSender(config.SMTPConfig{
		Host: host,
		Port: port,
	}, "sender@example.com")

	ctx := context.Background()
	result := s.Send(ctx, Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "Plain send no auth",
		Body:    "Hello plain world",
	})
	if !result.Success {
		t.Fatalf("Send() failed: %v", result.Error)
	}
}

func TestContextWithSequence(t *testing.T) {
	ctx := ContextWithSequence(context.Background(), 99)
	seq := sequenceFromContext(ctx)
	if seq != 99 {
		t.Errorf("sequenceFromContext() = %d, want 99", seq)
	}
}

func TestSequenceFromContext_Missing(t *testing.T) {
	seq := sequenceFromContext(context.Background())
	if seq != 0 {
		t.Errorf("sequenceFromContext() = %d, want 0 for missing key", seq)
	}
}

func TestValidateMessage_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr bool
	}{
		{"tab in from", Message{From: "user\t@evil.com", To: "a@b.com"}, true},
		{"null in to", Message{From: "a@b.com", To: "user@\x00evil.com"}, true},
		{"valid unicode", Message{From: "user@example.com", To: "tëst@example.com"}, false},
		{"long local part", Message{From: "a234567890123456789012345678901234567890123456789012345678901234@example.com", To: "b@c.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMessage(tt.msg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSender_EdgeCases(t *testing.T) {
	t.Run("sendgrid provider", func(t *testing.T) {
		cfg := config.EmailConfig{Provider: "sendgrid"}
		_, err := NewSender(cfg)
		if err == nil {
			t.Error("expected error for sendgrid provider")
		}
		if !strings.Contains(err.Error(), "sendgrid") {
			t.Errorf("error should mention sendgrid: %v", err)
		}
	})

	t.Run("ses provider", func(t *testing.T) {
		cfg := config.EmailConfig{Provider: "ses"}
		_, err := NewSender(cfg)
		if err == nil {
			t.Error("expected error for ses provider")
		}
	})
}

func TestSMTPSender_Send_SubjectValidation(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
	}, "sender@example.com")

	result := s.Send(context.Background(), Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "Line 1\nLine 2",
		Body:    "body",
	})
	if result.Success {
		t.Error("should fail with LF in subject")
	}
	if !strings.Contains(result.Error.Error(), "invalid characters") {
		t.Errorf("error = %v, want subject invalid characters", result.Error)
	}
}

func TestSanitizeSMTPError_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"auth lowercase", fmt.Errorf("535 authentication failed"), "SMTP authentication failed"},
		{"mixed case Auth", fmt.Errorf("Auth required"), "SMTP authentication failed"},
		{"certificate lowercase", fmt.Errorf("x509: certificate verify failed"), "TLS certificate error"},
		{"Certificate capitalized", fmt.Errorf("bad Certificate chain"), "TLS certificate error"},
		{"connection refused", fmt.Errorf("connection refused"), "SMTP error: check your configuration"},
		{"timeout error", fmt.Errorf("i/o timeout"), "SMTP error: check your configuration"},
		{"nil error message", fmt.Errorf(""), "SMTP error: check your configuration"},
		{"tls handshake", fmt.Errorf("tls: handshake failure"), "SMTP error: check your configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSMTPError(tt.err)
			if got.Error() != tt.want {
				t.Errorf("sanitizeSMTPError() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestSMTPSender_NoTLSNoAuth_PlainPath(t *testing.T) {
	srv := newMockSMTPServer(t)
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(portStr)

	s := NewSMTPSender(config.SMTPConfig{
		Host: host,
		Port: port,
	}, "test@example.com")

	result := s.Send(context.Background(), Message{
		From:    "test@example.com",
		To:      "target@example.com",
		Subject: "No TLS No Auth Test",
		Body:    "Plain SMTP test body",
	})
	if !result.Success {
		t.Fatalf("Send() failed: %v", result.Error)
	}
	if !strings.HasPrefix(result.MessageID, "smtp-target@example.com-") {
		t.Errorf("MessageID = %q, unexpected format", result.MessageID)
	}
}

func TestSendWithTLS_FullHandshake(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatal(err)
	}

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srvTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
	}
	srvCertDER, err := x509.CreateCertificate(rand.Reader, srvTemplate, caCert, &srvKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srvCert, err := x509.ParseCertificate(srvCertDER)
	if err != nil {
		t.Fatal(err)
	}

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(caCert)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{srvCertDER},
		PrivateKey:  srvKey,
		Leaf:        srvCert,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	})

	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 1024), 1024)
		fmt.Fprintf(conn, "220 test ESMTP\r\n")
		for scanner.Scan() {
			line := scanner.Text()
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
				fmt.Fprintf(conn, "250-test\r\n250 AUTH PLAIN\r\n")
			} else if strings.HasPrefix(upper, "AUTH PLAIN") {
				fmt.Fprintf(conn, "235 2.7.0 Authentication successful\r\n")
			} else if strings.HasPrefix(upper, "MAIL FROM") {
				fmt.Fprintf(conn, "250 2.1.0 OK\r\n")
			} else if strings.HasPrefix(upper, "RCPT TO") {
				fmt.Fprintf(conn, "250 2.1.5 OK\r\n")
			} else if strings.HasPrefix(upper, "DATA") {
				fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			} else if line == "." {
				fmt.Fprintf(conn, "250 2.0.0 OK: Message accepted\r\n")
			} else if strings.HasPrefix(upper, "QUIT") {
				fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
				return
			} else if strings.HasPrefix(upper, "RSET") {
				fmt.Fprintf(conn, "250 2.0.0 OK\r\n")
			} else if strings.HasPrefix(upper, "NOOP") {
				fmt.Fprintf(conn, "250 2.0.0 OK\r\n")
			}
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	orig := tlsDial
	tlsDial = func(network, addr string, cfg *tls.Config) (*tls.Conn, error) {
		cfg.RootCAs = rootCAs
		cfg.InsecureSkipVerify = false
		return tls.Dial(network, addr, cfg)
	}
	defer func() { tlsDial = orig }()

	sender := NewSMTPSender(config.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     port,
		UseTLS:   true,
		Username: "user",
		Password: "pass",
	}, "from@test.com")

	result := sender.Send(context.Background(), Message{
		From:    "from@test.com",
		To:      "to@test.com",
		Subject: "TLS Test",
		Body:    "Hello TLS",
	})
	if !result.Success {
		t.Errorf("Send() failed: %v", result.Error)
	}
}
