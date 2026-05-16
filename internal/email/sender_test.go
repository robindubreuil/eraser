package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/robindubreuil/eraser/internal/config"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid simple", "user@example.com", false},
		{"valid with dots", "first.last@example.com", false},
		{"valid with plus", "user+tag@example.com", false},
		{"valid with subdomain", "user@mail.example.com", false},
		{"CRLF injection", "user@example.com\r\nBcc: evil@evil.com", true},
		{"LF injection", "user@example.com\nBcc: evil@evil.com", true},
		{"CR injection", "user@example.com\revil@evil.com", true},
		{"comma injection", "user@example.com,evil@evil.com", true},
		{"semicolon injection", "user@example.com;evil@evil.com", true},
		{"empty string", "", true},
		{"missing @", "userexample.com", true},
		{"missing domain", "user@", true},
		{"missing local", "@example.com", true},
		{"spaces", "user @example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestNewSender(t *testing.T) {
	t.Run("smtp provider", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "smtp",
			From:     "test@example.com",
			SMTP: config.SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "test@example.com",
				Password: "secret",
				UseTLS:   true,
			},
		}
		sender, err := NewSender(cfg)
		if err != nil {
			t.Fatalf("NewSender() error = %v", err)
		}
		if sender.Name() != "smtp" {
			t.Errorf("Name() = %q, want %q", sender.Name(), "smtp")
		}
	})

	t.Run("empty provider defaults to smtp", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "",
			From:     "test@example.com",
			SMTP: config.SMTPConfig{
				Host: "smtp.example.com",
				Port: 587,
			},
		}
		sender, err := NewSender(cfg)
		if err != nil {
			t.Fatalf("NewSender() error = %v", err)
		}
		if sender.Name() != "smtp" {
			t.Errorf("Name() = %q, want %q", sender.Name(), "smtp")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		cfg := config.EmailConfig{
			Provider: "carrier-pigeon",
			From:     "test@example.com",
		}
		_, err := NewSender(cfg)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestSMTPSender_Name(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{}, "test@example.com")
	if s.Name() != "smtp" {
		t.Errorf("Name() = %q, want %q", s.Name(), "smtp")
	}
}

func TestNewSMTPSender(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.SMTPConfig
		from string
	}{
		{"with TLS", config.SMTPConfig{Host: "smtp.example.com", Port: 587, Username: "u", Password: "p", UseTLS: true}, "a@b.com"},
		{"without TLS", config.SMTPConfig{Host: "smtp.example.com", Port: 25}, "a@b.com"},
		{"empty config", config.SMTPConfig{}, ""},
		{"port 465", config.SMTPConfig{Host: "mail.test", Port: 465, UseTLS: true}, "x@y.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSMTPSender(tt.cfg, tt.from)
			if s == nil {
				t.Fatal("NewSMTPSender returned nil")
			}
			if s.from != tt.from {
				t.Errorf("from = %q, want %q", s.from, tt.from)
			}
			if s.config.Host != tt.cfg.Host {
				t.Errorf("Host = %q, want %q", s.config.Host, tt.cfg.Host)
			}
			if s.config.Port != tt.cfg.Port {
				t.Errorf("Port = %d, want %d", s.config.Port, tt.cfg.Port)
			}
		})
	}
}

func TestValidateMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr bool
	}{
		{"valid", Message{From: "a@b.com", To: "c@d.com"}, false},
		{"bad sender", Message{From: "not-email", To: "c@d.com"}, true},
		{"bad recipient", Message{From: "a@b.com", To: "bad"}, true},
		{"both bad", Message{From: "x", To: "y"}, true},
		{"empty from", Message{From: "", To: "c@d.com"}, true},
		{"empty to", Message{From: "a@b.com", To: ""}, true},
		{"injection in from", Message{From: "a@b.com\r\nevil@e.com", To: "c@d.com"}, true},
		{"injection in to", Message{From: "a@b.com", To: "c@d.com;evil@e.com"}, true},
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

func TestSanitizeSMTPError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"auth error", fmt.Errorf("535 authentication failed"), "SMTP authentication failed"},
		{"auth uppercase", fmt.Errorf("AUTH required"), "SMTP authentication failed"},
		{"cert error", fmt.Errorf("x509: certificate verify failed"), "TLS certificate error"},
		{"Certificate capitalized", fmt.Errorf("bad Certificate chain"), "TLS certificate error"},
		{"generic", fmt.Errorf("connection refused"), "SMTP error: check your configuration"},
		{"timeout", fmt.Errorf("i/o timeout"), "SMTP error: check your configuration"},
		{"empty error", fmt.Errorf(""), "SMTP error: check your configuration"},
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

func TestSMTPSender_Send_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		subject  string
		username string
		wantErr  string
	}{
		{"invalid sender", "bad", "to@example.com", "Sub", "", "invalid sender"},
		{"invalid recipient", "from@example.com", "not-email", "Sub", "", "invalid recipient"},
		{"subject CRLF", "from@example.com", "to@example.com", "Sub\r\nject", "", "subject contains invalid characters"},
		{"subject CR", "from@example.com", "to@example.com", "Sub\rject", "", "subject contains invalid characters"},
		{"subject LF", "from@example.com", "to@example.com", "Sub\nject", "", "subject contains invalid characters"},
		{"auth without TLS", "from@example.com", "to@example.com", "Sub", "user", "SMTP auth requires TLS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSMTPSender(config.SMTPConfig{
				Host:     "smtp.example.com",
				Port:     587,
				Username: tt.username,
				UseTLS:   false,
			}, tt.from)
			result := s.Send(context.Background(), Message{
				From:    tt.from,
				To:      tt.to,
				Subject: tt.subject,
				Body:    "body",
			})
			if result.Success {
				t.Fatal("expected failure")
			}
			if !strings.Contains(result.Error.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", result.Error.Error(), tt.wantErr)
			}
		})
	}
}

func TestSMTPSender_Send_TLSConnectionError(t *testing.T) {
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

func TestSMTPSender_Send_NoTLSConnectionError(t *testing.T) {
	s := NewSMTPSender(config.SMTPConfig{
		Host: "127.0.0.1",
		Port: 1,
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

type mockSMTPServer struct {
	addr string
	ln   net.Listener
	done chan struct{}
}

func newMockSMTPServer(t *testing.T) *mockSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &mockSMTPServer{addr: ln.Addr().String(), ln: ln, done: make(chan struct{})}
	go s.serve()
	return s
}

func (s *mockSMTPServer) serve() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *mockSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	w.WriteString("220 ready\r\n")
	w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO") || strings.HasPrefix(cmd, "HELO"):
			w.WriteString("250-localhost\r\n250 OK\r\n")
			w.Flush()
		case strings.HasPrefix(cmd, "MAIL FROM"):
			w.WriteString("250 OK\r\n")
			w.Flush()
		case strings.HasPrefix(cmd, "RCPT TO"):
			w.WriteString("250 OK\r\n")
			w.Flush()
		case strings.HasPrefix(cmd, "DATA"):
			w.WriteString("354 Go ahead\r\n")
			w.Flush()
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dl) == "." {
					w.WriteString("250 OK\r\n")
					w.Flush()
					break
				}
			}
		case strings.HasPrefix(cmd, "QUIT"):
			w.WriteString("221 Bye\r\n")
			w.Flush()
			return
		default:
			w.WriteString("500 huh\r\n")
			w.Flush()
		}
	}
}

func (s *mockSMTPServer) Addr() string { return s.addr }

func (s *mockSMTPServer) Close() {
	s.ln.Close()
	<-s.done
}

func TestSMTPSender_Send_SuccessNoTLS(t *testing.T) {
	srv := newMockSMTPServer(t)
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(portStr)

	s := NewSMTPSender(config.SMTPConfig{
		Host: host,
		Port: port,
	}, "sender@example.com")

	ctx := ContextWithSequence(context.Background(), 42)
	result := s.Send(ctx, Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "Test Subject",
		Body:    "Hello, World!",
	})
	if !result.Success {
		t.Fatalf("Send() failed: %v", result.Error)
	}
	if result.MessageID != "smtp-recipient@example.com-42" {
		t.Errorf("MessageID = %q, want %q", result.MessageID, "smtp-recipient@example.com-42")
	}
	if result.Error != nil {
		t.Errorf("Error = %v, want nil", result.Error)
	}
}

func TestSMTPSender_Send_NoSequenceInContext(t *testing.T) {
	srv := newMockSMTPServer(t)
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(portStr)

	s := NewSMTPSender(config.SMTPConfig{
		Host: host,
		Port: port,
	}, "sender@example.com")

	result := s.Send(context.Background(), Message{
		From:    "sender@example.com",
		To:      "recipient@example.com",
		Subject: "Test",
		Body:    "body",
	})
	if !result.Success {
		t.Fatalf("Send() failed: %v", result.Error)
	}
	if !strings.HasPrefix(result.MessageID, "smtp-recipient@example.com-") {
		t.Errorf("MessageID = %q, unexpected format", result.MessageID)
	}
}
