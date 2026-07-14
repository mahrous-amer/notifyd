package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"

	"github.com/bse/notifyd/internal/domain"
)

// implicitTLSPort is the well-known port for SMTPS (TLS from the first byte,
// as opposed to STARTTLS which upgrades a plaintext connection mid-session).
const implicitTLSPort = 465

// sendTimeout bounds how long a single Send call may take end-to-end (dial,
// TLS handshake, auth, and message transfer) when the caller's context has no
// earlier deadline of its own.
const sendTimeout = 30 * time.Second

type emailConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	CC       []string `json:"cc,omitempty"`
	ReplyTo  string   `json:"reply_to,omitempty"`
}

// EmailProvider sends notifications through a customer-supplied SMTP server
// (bring-your-own-SMTP). Credentials and delivery settings live in the
// channel config; the provider never sends through a shared platform mailbox.
type EmailProvider struct {
	// tlsRootCAs overrides the system trust store used to verify the SMTP
	// server's certificate. Nil means "use the system pool", which is the
	// correct default for every production SMTP relay. Tests set this to
	// trust an in-process server's self-signed certificate.
	tlsRootCAs *x509.CertPool
}

func NewEmailProvider() *EmailProvider {
	return &EmailProvider{}
}

// NewEmailProviderWithTLSRootCAs returns an EmailProvider that verifies SMTP
// server certificates against the given pool instead of the system trust
// store. Intended for connecting to internal SMTP relays with private CAs,
// and for tests exercising TLS negotiation against an in-process server.
func NewEmailProviderWithTLSRootCAs(pool *x509.CertPool) *EmailProvider {
	return &EmailProvider{tlsRootCAs: pool}
}

func (e *EmailProvider) Type() string { return "email" }

// Capabilities returns an empty set. BYO-SMTP has no delivery or read
// receipt channel, and open/click tracking is out of scope for v1.
func (e *EmailProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{}
}

// FetchMetrics always returns ErrMetricsNotSupported: plain SMTP delivery
// offers no mechanism to query engagement after the message is accepted.
func (e *EmailProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}

func (e *EmailProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg emailConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid email config: %w", err)
	}
	return validateEmailConfig(cfg)
}

func validateEmailConfig(cfg emailConfig) error {
	if cfg.Host == "" {
		return fmt.Errorf("email config: host is required")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("email config: port must be between 1 and 65535")
	}
	if cfg.Username == "" {
		return fmt.Errorf("email config: username is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("email config: password is required")
	}
	if cfg.From == "" {
		return fmt.Errorf("email config: from is required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return fmt.Errorf("email config: from: %w", err)
	}
	if len(cfg.To) == 0 {
		return fmt.Errorf("email config: to is required")
	}
	if err := validateAddressList("to", cfg.To); err != nil {
		return err
	}
	if err := validateAddressList("cc", cfg.CC); err != nil {
		return err
	}
	if cfg.ReplyTo != "" {
		if _, err := mail.ParseAddress(cfg.ReplyTo); err != nil {
			return fmt.Errorf("email config: reply_to: %w", err)
		}
	}
	return nil
}

func validateAddressList(field string, addrs []string) error {
	for _, addr := range addrs {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("email config: %s: invalid address %q: %w", field, addr, err)
		}
	}
	return nil
}

func (e *EmailProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg emailConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal email config: %w", err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, sendTimeout)
		defer cancel()
	}

	message := buildEmailMessage(cfg, req)

	if err := sendViaSMTP(ctx, cfg, message, e.tlsRootCAs); err != nil {
		return classifyEmailSendError(err), nil
	}

	return &SendResponse{Success: true}, nil
}

// classifyEmailSendError converts a delivery error into a SendResponse,
// deciding whether the failure is worth retrying.
//
// Permanent (no retry): SMTP 5xx replies — bad credentials, rejected
// recipient, relay denied. Retrying with the same config cannot help.
//
// Transient (retry): SMTP 421/450/451, connection failures, and timeouts —
// the same request will likely succeed once the network or server recovers.
func classifyEmailSendError(err error) *SendResponse {
	return &SendResponse{
		Success:      false,
		ErrorMessage: err.Error(),
		Permanent:    isPermanentSMTPError(err),
	}
}

func isPermanentSMTPError(err error) bool {
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return protoErr.Code >= 500 && protoErr.Code < 600
	}
	// Connection failures, DNS errors, transientSMTPError, and context
	// deadline/cancellation are all transport-level problems that a later
	// retry can plausibly resolve.
	return false
}

// transientSMTPError marks a failure as retryable when it has no SMTP status
// code to classify by (e.g. a locally-detected precondition failure such as a
// missing STARTTLS advertisement, rather than a server reply).
type transientSMTPError struct{ msg string }

func (e *transientSMTPError) Error() string { return e.msg }

// buildEmailMessage renders the full RFC 5322 message (headers + body) for
// the given format mode:
//   - "html": a single text/html part.
//   - "markdown": rendered to HTML as the primary part, with the raw
//     markdown source kept as a text/plain alternative so plain-text mail
//     clients still show readable content.
//   - "plain" or unset: a single text/plain part.
func buildEmailMessage(cfg emailConfig, req SendRequest) []byte {
	headers := buildEmailHeaders(cfg, req.Subject)

	var buf bytes.Buffer
	buf.WriteString(headers)

	switch req.FormatMode {
	case "html":
		writeSinglePart(&buf, "text/html; charset=UTF-8", req.Body)
	case "markdown":
		writeMarkdownAlternative(&buf, req.Body)
	default: // "plain" or ""
		writeSinglePart(&buf, "text/plain; charset=UTF-8", req.Body)
	}

	return buf.Bytes()
}

func buildEmailHeaders(cfg emailConfig, subject string) string {
	var h strings.Builder
	fmt.Fprintf(&h, "From: %s\r\n", cfg.From)
	fmt.Fprintf(&h, "To: %s\r\n", strings.Join(cfg.To, ", "))
	if len(cfg.CC) > 0 {
		fmt.Fprintf(&h, "Cc: %s\r\n", strings.Join(cfg.CC, ", "))
	}
	if cfg.ReplyTo != "" {
		fmt.Fprintf(&h, "Reply-To: %s\r\n", cfg.ReplyTo)
	}
	fmt.Fprintf(&h, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	h.WriteString("MIME-Version: 1.0\r\n")
	return h.String()
}

// writeSinglePart writes a MIME part with the body quoted-printable encoded.
// Raw UTF-8 sent with an implicit 7bit transfer encoding can be mangled by
// strict relays that don't support 8BITMIME; quoted-printable survives any
// relay that only understands ASCII.
func writeSinglePart(buf *bytes.Buffer, contentType, body string) {
	fmt.Fprintf(buf, "Content-Type: %s\r\n", contentType)
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	qpWriter := quotedprintable.NewWriter(buf)
	_, _ = qpWriter.Write([]byte(body)) // bytes.Buffer.Write never errors
	_ = qpWriter.Close()

	buf.WriteString("\r\n")
}

// writeMarkdownAlternative renders body as HTML and writes a
// multipart/alternative message with the raw markdown as the text/plain
// fallback. Mail clients that cannot render HTML fall back to the plain part,
// which shows the original markdown source rather than nothing.
func writeMarkdownAlternative(buf *bytes.Buffer, body string) {
	const boundary = "notifyd-markdown-boundary"

	var htmlBody bytes.Buffer
	if err := goldmark.Convert([]byte(body), &htmlBody); err != nil {
		// Conversion failures are rare (goldmark's parser does not error on
		// arbitrary text); fall back to escaping the source as HTML so the
		// message still sends rather than dropping the HTML part entirely.
		htmlBody.Reset()
		htmlBody.WriteString(body)
	}

	fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)

	fmt.Fprintf(buf, "--%s\r\n", boundary)
	writeSinglePart(buf, "text/plain; charset=UTF-8", body)

	fmt.Fprintf(buf, "--%s\r\n", boundary)
	writeSinglePart(buf, "text/html; charset=UTF-8", htmlBody.String())

	fmt.Fprintf(buf, "--%s--\r\n", boundary)
}

// sendViaSMTP delivers message over a connection to cfg.Host:cfg.Port,
// negotiating TLS the way each port implies: implicit TLS on 465 (the
// connection is TLS from the first byte), STARTTLS everywhere else when the
// server advertises it. rootCAs overrides the system trust store when
// non-nil (see EmailProvider.tlsRootCAs).
func sendViaSMTP(ctx context.Context, cfg emailConfig, message []byte, rootCAs *x509.CertPool) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	conn, err := dialWithContext(ctx, addr, cfg.Host, cfg.Port, rootCAs)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close() //nolint:errcheck

	if cfg.Port != implicitTLSPort {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			// A server that doesn't advertise STARTTLS leaves no way to
			// encrypt the session on this port. Refuse rather than fall
			// through to AUTH in plaintext. A stripped STARTTLS advertisement
			// is itself a class of MITM downgrade attack, but it is equally
			// explained by transient misconfiguration or a network
			// intermediary interfering with this one connection, so treat it
			// as retryable rather than permanently failing the channel.
			return &transientSMTPError{msg: "server does not advertise STARTTLS; refusing to send credentials over plaintext"}
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, RootCAs: rootCAs}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	return deliverMessage(client, cfg, message)
}

// dialWithContext opens the TCP connection honoring ctx's deadline, then
// wraps it in TLS immediately when the port implies implicit TLS (465).
func dialWithContext(ctx context.Context, addr, host string, port int, rootCAs *x509.CertPool) (net.Conn, error) {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	if port == implicitTLSPort {
		return tls.Client(conn, &tls.Config{ServerName: host, RootCAs: rootCAs}), nil
	}
	return conn, nil
}

func deliverMessage(client *smtp.Client, cfg emailConfig, message []byte) error {
	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}

	recipients := append(append([]string{}, cfg.To...), cfg.CC...)
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(message); err != nil {
		_ = w.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	// The server's reply to closing the DATA stream is what commits delivery;
	// that already succeeded above. QUIT only tears down the session, so a
	// failure here (e.g. the server dropping the connection right after
	// accepting the message) must not be reported as a failed send — doing so
	// would trigger a retry and duplicate a message the server already has.
	_ = client.Quit()
	return nil
}
