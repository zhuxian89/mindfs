package identity

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Secret string

func (Secret) String() string {
	return "[REDACTED]"
}

func (Secret) GoString() string {
	return "[REDACTED]"
}

type SMTPConfig struct {
	Host     string
	Port     int
	TLS      bool
	From     string
	Username string
	Password Secret
	Timeout  time.Duration
}

type VerificationPurpose string

const (
	PurposeRegister      VerificationPurpose = "register"
	PurposePasswordReset VerificationPurpose = "password_reset"
)

type MailSender interface {
	SendVerificationCode(context.Context, string, VerificationPurpose, string) error
}

type SMTPSender struct {
	config SMTPConfig
}

func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host != "smtp.qq.com" || cfg.Port != 465 || !cfg.TLS {
		return nil, errors.New("QQ SMTP requires smtp.qq.com:465 with implicit TLS")
	}
	if _, err := NormalizeQQEmail(cfg.From); err != nil {
		return nil, errors.New("SMTP sender must be an @qq.com address")
	}
	if _, err := NormalizeQQEmail(cfg.Username); err != nil {
		return nil, errors.New("SMTP username must be an @qq.com address")
	}
	if cfg.Password == "" {
		return nil, errors.New("SMTP password is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPSender{config: cfg}, nil
}

func (s *SMTPSender) SendVerificationCode(ctx context.Context, recipient string, purpose VerificationPurpose, code string) error {
	recipient, err := NormalizeQQEmail(recipient)
	if err != nil {
		return err
	}
	subject, action, err := verificationCopy(purpose)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	dialer := &net.Dialer{Timeout: s.config.Timeout}
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("connect SMTP server")
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(s.config.Timeout))
	tlsConn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.config.Host})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return errors.New("establish SMTP TLS")
	}
	client, err := smtp.NewClient(tlsConn, s.config.Host)
	if err != nil {
		return errors.New("initialize SMTP client")
	}
	defer client.Close()
	if err := client.Auth(smtp.PlainAuth("", s.config.Username, string(s.config.Password), s.config.Host)); err != nil {
		return errors.New("authenticate SMTP client")
	}
	if err := client.Mail(s.config.From); err != nil {
		return errors.New("set SMTP sender")
	}
	if err := client.Rcpt(recipient); err != nil {
		return errors.New("set SMTP recipient")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("open SMTP message")
	}
	message := strings.Join([]string{
		"From: MindFS Relay <" + s.config.From + ">",
		"To: " + recipient,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"Your MindFS Relay " + action + " verification code is: " + code,
		"",
		"This code expires in 10 minutes. If you did not request it, ignore this email.",
	}, "\r\n")
	buffered := bufio.NewWriter(writer)
	if _, err := io.WriteString(buffered, message); err != nil {
		_ = writer.Close()
		return errors.New("write SMTP message")
	}
	if err := buffered.Flush(); err != nil {
		_ = writer.Close()
		return errors.New("flush SMTP message")
	}
	if err := writer.Close(); err != nil {
		return errors.New("send SMTP message")
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func verificationCopy(purpose VerificationPurpose) (string, string, error) {
	switch purpose {
	case PurposeRegister:
		return "MindFS Relay registration code", "registration", nil
	case PurposePasswordReset:
		return "MindFS Relay password reset code", "password reset", nil
	default:
		return "", "", errors.New("invalid verification purpose")
	}
}
