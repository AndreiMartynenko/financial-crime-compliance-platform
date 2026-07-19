package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type SMTPConfig struct {
	Host, Username, Password, From string
	Port                           int
	StartTLS                       bool
	Timeout                        time.Duration
}

type SMTPSender struct{ config SMTPConfig }

func NewSMTPSender(config SMTPConfig) *SMTPSender { return &SMTPSender{config: config} }

func (s *SMTPSender) Send(ctx context.Context, message domain.OutboxMessage) error {
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	connection, err := (&net.Dialer{Timeout: s.config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else if s.config.Timeout > 0 {
		_ = connection.SetDeadline(time.Now().Add(s.config.Timeout))
	}
	client, err := smtp.NewClient(connection, s.config.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if s.config.StartTLS {
		if err := client.StartTLS(&tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(s.config.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(message.Destination); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP body: %w", err)
	}
	title := headerValue(fmt.Sprint(message.Payload["title"]))
	body := fmt.Sprint(message.Payload["message"])
	content := "From: " + s.config.From + "\r\nTo: " + message.Destination + "\r\nSubject: " + title + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n"
	if _, err := io.WriteString(writer, content); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("send SMTP body: %w", err)
	}
	return client.Quit()
}

func headerValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}
