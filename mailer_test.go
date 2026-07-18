package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSMTPEmailSenderAcceptsMessageDespiteQuitFailure(t *testing.T) {
	sender, serverDone := smtpTestSender(t, true)

	err := sender.Send(t.Context(), emailMessage{To: "recipient@example.test", Subject: "subject", Text: "body"})
	if err != nil {
		t.Fatalf("Send() error = %v, want success after accepted DATA", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestSMTPEmailSenderReturnsDataCommitFailure(t *testing.T) {
	sender, serverDone := smtpTestSender(t, false)

	err := sender.Send(t.Context(), emailMessage{To: "recipient@example.test", Subject: "subject", Text: "body"})
	if err == nil {
		t.Fatal("Send() error = nil, want DATA commit failure")
	}
	if stage := smtpErrorStage(err); stage != "data_commit" {
		t.Fatalf("SMTP error stage = %q, want data_commit", stage)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestResendVerificationRedirectsAfterAcceptedDataAndKeepsRateLimit(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	auth.baseURL = "https://example.test"
	sender, serverDone := smtpTestSender(t, true)
	auth.mailer = sender
	if err := auth.ensureUser(t.Context(), "owner@example.test", "Owner", "StrongPassword1"); err != nil {
		t.Fatal(err)
	}
	u, err := auth.authenticate(t.Context(), "owner@example.test", "StrongPassword1")
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := auth.createSession(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)
	form := url.Values{"csrf_token": {csrfToken(session)}}

	response := resendRequest(t, handler, session, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("resend status = %d, want 303: %s", response.Code, response.Body.String())
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}

	response = resendRequest(t, handler, session, form)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited resend status = %d, want 429", response.Code)
	}
}

func TestResendVerificationLogsOnlySafeFailureStage(t *testing.T) {
	auth, _, _ := recoveryAuth(t)
	auth.mailer = failingEmailSender{err: smtpError("recipient", errors.New("rejected recipient@example.test with secret-token"))}
	u, err := auth.authenticate(t.Context(), "owner@example.com", "StrongPassword1")
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := auth.createSession(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	a := &app{auth: auth, logger: log.New(&logs, "", 0)}
	form := url.Values{"csrf_token": {csrfToken(session)}}
	req := httptest.NewRequest(http.MethodPost, "/verify-email/resend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	req = req.WithContext(context.WithValue(req.Context(), userContextKey{}, u))
	response := httptest.NewRecorder()

	a.resendVerification(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("resend status = %d, want 500", response.Code)
	}
	if got := logs.String(); got != "resend verification failed: stage=recipient\n" {
		t.Fatalf("unsafe or unexpected log = %q", got)
	}
}

type failingEmailSender struct{ err error }

func (s failingEmailSender) Send(context.Context, emailMessage) error { return s.err }

func resendRequest(t *testing.T, handler http.Handler, session string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/verify-email/resend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func smtpTestSender(t *testing.T, acceptData bool) (*smtpEmailSender, <-chan error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveSMTPTestConnection(serverConn, acceptData)
	}()
	sender := &smtpEmailSender{
		host: "smtp.example.test",
		port: "465",
		from: "sender@example.test",
		dialContext: func(context.Context, string) (net.Conn, error) {
			return clientConn, nil
		},
	}
	return sender, done
}

func serveSMTPTestConnection(conn net.Conn, acceptData bool) error {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	if _, err := io.WriteString(conn, "220 smtp.example.test ESMTP\r\n"); err != nil {
		return err
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(line, "EHLO "):
			_, err = io.WriteString(conn, "250 smtp.example.test\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"):
			_, err = io.WriteString(conn, "250 sender accepted\r\n")
		case strings.HasPrefix(line, "RCPT TO:"):
			_, err = io.WriteString(conn, "250 recipient accepted\r\n")
		case line == "DATA\r\n":
			if _, err = io.WriteString(conn, "354 send message\r\n"); err != nil {
				return err
			}
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					return readErr
				}
				if dataLine == ".\r\n" {
					break
				}
			}
			if acceptData {
				_, err = io.WriteString(conn, "250 message accepted\r\n")
			} else {
				_, err = io.WriteString(conn, "451 message rejected\r\n")
				return err
			}
		case line == "QUIT\r\n":
			// Simulate a provider closing the connection after accepting DATA,
			// before it can send the successful QUIT response.
			return nil
		default:
			_, err = io.WriteString(conn, "500 unexpected command\r\n")
		}
		if err != nil {
			return err
		}
	}
}
