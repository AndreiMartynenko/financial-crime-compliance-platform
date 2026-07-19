package notification

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

func TestSMTPSenderDeliversMessage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	go serveSMTP(t, listener, received)
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	sender := NewSMTPSender(SMTPConfig{Host: host, Port: port, From: "alerts@example.com", Timeout: time.Second})
	err = sender.Send(context.Background(), domain.OutboxMessage{Destination: "reviewer@example.com", Payload: map[string]any{"title": "Potential match", "message": "Review customer 42"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if !strings.Contains(message, "Subject: Potential match") || !strings.Contains(message, "Review customer 42") {
			t.Fatalf("message=%q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP message was not received")
	}
}

func serveSMTP(t *testing.T, listener net.Listener, received chan<- string) {
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer connection.Close()
	reader, writer := bufio.NewReader(connection), bufio.NewWriter(connection)
	write := func(value string) { _, _ = writer.WriteString(value); _ = writer.Flush() }
	write("220 localhost ESMTP\r\n")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"):
			write("250 localhost\r\n")
		case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
			write("250 ok\r\n")
		case strings.HasPrefix(line, "DATA"):
			write("354 end with dot\r\n")
			var body strings.Builder
			for {
				part, readErr := reader.ReadString('\n')
				if readErr != nil {
					t.Error(readErr)
					return
				}
				if part == ".\r\n" {
					break
				}
				body.WriteString(part)
			}
			received <- body.String()
			write("250 queued\r\n")
		case strings.HasPrefix(line, "QUIT"):
			write("221 bye\r\n")
			return
		default:
			write("250 ok\r\n")
		}
	}
}

func TestHeaderValueRemovesNewlines(t *testing.T) {
	if got := headerValue("alert\r\nBcc: attacker@example.com"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header=%q", got)
	}
}
