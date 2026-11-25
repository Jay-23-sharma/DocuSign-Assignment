package mail

import (
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type Sender struct{ addr, host, origin string }

func New(addr, base string) (*Sender, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("SMTP_ADDR must contain a host and port")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("FRONTEND_BASE_URL must be an HTTP or HTTPS origin")
	}
	u.Path = ""
	return &Sender{addr, host, u.String()}, nil
}

var ErrUnconfirmed = errors.New("mail server acceptance unconfirmed")

func (s *Sender) Send(email, token string) error {
	return s.sendText(email, "Verify your LocalSign email", "Open this link and select Verify email within 24 hours:\n\n"+s.origin+"/verify-email#"+token)
}

func (s *Sender) SendInvitation(email, subject, message string, deadline time.Time, token string) error {
	return s.sendText(email, subject, message+"\n\nSigning deadline: "+deadline.UTC().Format(time.RFC3339)+"\n\nOpen your personal signing link:\n"+s.origin+"/sign#"+token)
}

func (s *Sender) sendText(email, subject, text string) error {
	if strings.ContainsAny(email+subject, "\r\n") {
		return errors.New("invalid mail header")
	}
	conn, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer client.Close()
	const from = "verification@localsign.test"
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(email); err != nil {
		return err
	}
	body, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(body, "From: LocalSign <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", from, email, mime.QEncoding.Encode("utf-8", subject))
	if err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(body)
	if _, err := encoded.Write([]byte(text + "\n")); err != nil {
		return err
	}
	if err := encoded.Close(); err != nil {
		return err
	}
	err = body.Close()
	var rejection *textproto.Error
	if err != nil && !errors.As(err, &rejection) {
		return ErrUnconfirmed
	}
	return err
}

func (s *Sender) SendCode(email, code string) error {
	return s.sendText(email, "Your LocalSign signing code", "Your signing code is "+code+". It expires in 10 minutes.")
}
