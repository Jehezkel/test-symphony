package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
)

type emailMessage struct {
	To      string
	Subject string
	Text    string
}

type emailSender interface {
	Send(context.Context, emailMessage) error
}

type discardEmailSender struct{}

func (discardEmailSender) Send(context.Context, emailMessage) error { return nil }

type smtpEmailSender struct {
	host, port, username, password, from string
}

func emailSenderFromEnv() (emailSender, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_PROVIDER")))
	if provider == "" || provider == "discard" {
		if os.Getenv("APP_ENV") == "production" {
			return nil, errors.New("EMAIL_PROVIDER=smtp is required in production")
		}
		return discardEmailSender{}, nil
	}
	if provider != "smtp" {
		return nil, fmt.Errorf("unsupported EMAIL_PROVIDER %q", provider)
	}
	s := &smtpEmailSender{host: os.Getenv("SMTP_HOST"), port: os.Getenv("SMTP_PORT"), username: os.Getenv("SMTP_USERNAME"), password: os.Getenv("SMTP_PASSWORD"), from: os.Getenv("EMAIL_FROM")}
	if s.host == "" || s.port == "" || s.from == "" {
		return nil, errors.New("SMTP_HOST, SMTP_PORT and EMAIL_FROM are required for SMTP")
	}
	if port, err := strconv.Atoi(s.port); err != nil || port < 1 || port > 65535 {
		return nil, errors.New("SMTP_PORT must be a valid port")
	}
	return s, nil
}

func (s *smtpEmailSender) Send(ctx context.Context, message emailMessage) error {
	address := net.JoinHostPort(s.host, s.port)
	dialer := &tls.Dialer{NetDialer: &net.Dialer{}}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SMTP: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("initialize SMTP: %w", err)
	}
	defer client.Close()
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message: %w", err)
	}
	body := "From: " + s.from + "\r\nTo: " + message.To + "\r\nSubject: " + message.Subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + message.Text
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	return client.Quit()
}
