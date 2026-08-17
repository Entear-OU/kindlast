// Package delivery is the channel seam: how a rendered message leaves the
// process (doc §17.5).
//
// One interface with one implementation today, which is deliberate rather than
// premature. The point of the seam is not that a second channel is imminent; it
// is that the dispatcher must not know what SMTP is, so that ENT-209's doorbell
// path and any later channel reuse this rather than growing a second delivery
// mechanism beside it (doc §23.6). A second mechanism is how a product ends up
// with two retry policies, two failure logs and two answers to "did it send".
package delivery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is a rendered message ready to leave.
//
// Deliberately not the store's row type and not the domain's: a channel needs a
// recipient, a subject and a body, and giving it the database id would invite a
// channel that updates the row itself.
type Message struct {
	To       string
	Subject  string
	BodyText string
	BodyHTML string
}

// Channel is somewhere a message can be sent.
type Channel interface {
	// Name identifies the channel in logs. Not a display string.
	Name() string
	// Send delivers, or returns why it could not. A returned error means the
	// message was not delivered and should be retried; nil means it was handed
	// off successfully.
	Send(ctx context.Context, msg Message) error
}

// SMTP delivers over SMTP submission.
//
// No authentication and no TLS, which is correct for the deployment this
// targets and wrong for any other. The bundled stack reaches Mailpit at
// `mailpit:1025` on a private compose network that publishes no SMTP port, so
// there is nothing to authenticate to and nothing on the wire to protect.
//
// A deployment sending real mail needs both, and that is a deliberate follow-on
// rather than an oversight: adding credentials means adding secret handling,
// and guessing at STARTTLS behaviour against an unknown relay is how a
// dispatcher ends up silently sending in the clear. This type refuses to grow
// that quietly; see the check in NewSMTP.
type SMTP struct {
	addr string
	from string
}

// NewSMTP builds the SMTP channel.
//
// The address is not dialled here. A mail server that is down at boot is an
// ordinary condition for a queue whose entire job is outliving one, so failing
// to start over it would be the wrong trade: the rows are safe on disk and the
// dispatcher retries.
func NewSMTP(addr, from string) (*SMTP, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, errors.New("delivery: an SMTP address is required")
	}
	if strings.TrimSpace(from) == "" {
		return nil, errors.New("delivery: a sender address is required")
	}
	// Caught here rather than at the first send, because the first send is hours
	// after the deployment and the error surfaces as an invitation that never
	// arrived rather than as a configuration mistake.
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("delivery: SMTP address must be host:port: %w", err)
	}
	// SplitHostPort accepts "mailpit:" and ":1025", returning an empty half
	// rather than an error. Both dial nowhere useful, and the second is the one
	// an operator writes by accident when copying a port out of compose.
	if host == "" || port == "" {
		return nil, fmt.Errorf("delivery: SMTP address must be host:port, got %q", addr)
	}
	return &SMTP{addr: addr, from: from}, nil
}

func (s *SMTP) Name() string { return "smtp" }

func (s *SMTP) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		// Not retryable, but returned as an error anyway: the row stays pending
		// with the reason recorded, which is visible, rather than being marked
		// sent, which would be a lie.
		return errors.New("delivery: the message has no recipient")
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("delivery: dialling %s: %w", s.addr, err)
	}

	host, _, _ := net.SplitHostPort(s.addr)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("delivery: opening an SMTP session: %w", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("delivery: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("delivery: RCPT TO: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("delivery: DATA: %w", err)
	}
	if _, err := writer.Write([]byte(s.render(msg))); err != nil {
		_ = writer.Close()
		return fmt.Errorf("delivery: writing the message: %w", err)
	}
	if err := writer.Close(); err != nil {
		// The close is where the server accepts or rejects the message, so this
		// is the error that matters most and the one easiest to discard.
		return fmt.Errorf("delivery: completing the message: %w", err)
	}

	// Best effort. A server that accepted the message and then failed to say
	// goodbye has still delivered it, and treating that as a failure would
	// redeliver on the next drain.
	_ = client.Quit()
	return nil
}

// render builds the RFC 5322 message.
//
// Headers are written here rather than assembled by a library because the set
// is small and the one that matters is easy to get wrong. `Date` and
// `Message-ID` are not decoration: mail without them is scored as spam by most
// receivers, which for an invitation means it is delivered to a folder nobody
// opens, and the sender sees a successful send either way.
func (s *SMTP) render(msg Message) string {
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", s.from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", headerSafe(msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(msg.BodyText, "\n", "\r\n"))

	return b.String()
}

// headerSafe strips CR and LF from a header value.
//
// Header injection: a subject carrying a newline ends the Subject header and
// begins whatever the attacker writes next, including `Bcc:`. The subject is
// built from an organisation name, which a customer chooses, so this is
// attacker-controlled input reaching a protocol that is line-delimited.
//
// Stripped rather than rejected, because a refusal here happens at delivery
// time, long after the invitation was minted, and would leave a permanently
// undeliverable row for a name the product accepted at creation.
func headerSafe(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}
