package provider_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// emailConfigParams groups the fields of the email provider's JSONB config so
// newEmailConfig doesn't need a long positional parameter list.
type emailConfigParams struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	CC       []string
	ReplyTo  string
}

func newEmailConfig(p emailConfigParams) json.RawMessage {
	m := map[string]interface{}{
		"host":     p.Host,
		"port":     p.Port,
		"username": p.Username,
		"password": p.Password,
		"from":     p.From,
		"to":       p.To,
	}
	if len(p.CC) > 0 {
		m["cc"] = p.CC
	}
	if p.ReplyTo != "" {
		m["reply_to"] = p.ReplyTo
	}
	raw, _ := json.Marshal(m)
	return raw
}

func validEmailConfigParams(host string, port int) emailConfigParams {
	return emailConfigParams{
		Host:     host,
		Port:     port,
		Username: "alerts@example.com",
		Password: "hunter2",
		From:     "alerts@example.com",
		To:       []string{"ops@example.com"},
	}
}

// --- Type / Capabilities / FetchMetrics ---

func TestEmailProvider_Type(t *testing.T) {
	p := provider.NewEmailProvider()
	assert.Equal(t, "email", p.Type())
}

func TestEmailProvider_Capabilities_ReturnsEmpty(t *testing.T) {
	// BYO-SMTP has no delivery/read receipt channel; open/click tracking is
	// explicitly out of scope for v1.
	p := provider.NewEmailProvider()
	caps := p.Capabilities()
	assert.Empty(t, caps.Capabilities)
}

func TestEmailProvider_FetchMetrics_ReturnsErrMetricsNotSupported(t *testing.T) {
	p := provider.NewEmailProvider()

	metrics, err := p.FetchMetrics(context.Background(), nil, "any-id")

	assert.Nil(t, metrics)
	assert.True(t, errors.Is(err, domain.ErrMetricsNotSupported))
}

// --- ValidateConfig matrix ---

func TestEmailProvider_ValidateConfig(t *testing.T) {
	p := provider.NewEmailProvider()

	t.Run("valid config", func(t *testing.T) {
		cfg := newEmailConfig(validEmailConfigParams("smtp.example.com", 587))
		require.NoError(t, p.ValidateConfig(cfg))
	})

	t.Run("valid config with cc and reply_to", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.CC = []string{"escalations@example.com"}
		params.ReplyTo = "noreply@example.com"
		cfg := newEmailConfig(params)
		require.NoError(t, p.ValidateConfig(cfg))
	})

	t.Run("missing host", func(t *testing.T) {
		params := validEmailConfigParams("", 587)
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "host is required")
	})

	t.Run("missing port", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 0)
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "port")
	})

	t.Run("port out of range", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 70000)
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "port")
	})

	t.Run("missing username", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.Username = ""
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("missing password", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.Password = ""
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password is required")
	})

	t.Run("missing from", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.From = ""
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from is required")
	})

	t.Run("invalid from address syntax", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.From = "not-an-email"
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from")
	})

	t.Run("missing to", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.To = nil
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to is required")
	})

	t.Run("invalid to address syntax", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.To = []string{"not-an-email"}
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to")
	})

	t.Run("invalid cc address syntax", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.CC = []string{"not-an-email"}
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cc")
	})

	t.Run("invalid reply_to address syntax", func(t *testing.T) {
		params := validEmailConfigParams("smtp.example.com", 587)
		params.ReplyTo = "not-an-email"
		err := p.ValidateConfig(newEmailConfig(params))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reply_to")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid email config")
	})
}

// --- Send: MIME structure per FormatMode, over a real in-process SMTP server ---

// hostPort splits a "host:port" listener address into the separate host and
// numeric port that emailConfigParams expects.
func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, port
}

// newEmailProviderTrusting returns an EmailProvider configured to trust
// server's self-signed certificate, for tests that exercise MIME structure,
// recipient handling, or SMTP-reply classification rather than TLS
// verification itself.
func newEmailProviderTrusting(t *testing.T, server *smtpTestServer) *provider.EmailProvider {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(server.leafCertificate(t))
	return provider.NewEmailProviderWithTLSRootCAs(pool)
}

// extractHeaderValue returns the value portion of the given header (the text
// after "Name: ") from a captured RFC 5322 message.
func extractHeaderValue(t *testing.T, message, header string) string {
	t.Helper()
	prefix := header + ": "
	for _, line := range strings.Split(message, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("header %q not found in message:\n%s", header, message)
	return ""
}

// decodeQuotedPrintableBodyPart extracts the single-part message body (the
// text after the blank line separating headers from content) and decodes it
// as quoted-printable.
func decodeQuotedPrintableBodyPart(t *testing.T, message string) string {
	t.Helper()
	_, body, found := strings.Cut(message, "\r\n\r\n")
	require.True(t, found, "message must have a blank line separating headers from body")
	body = strings.TrimSuffix(body, "\r\n")

	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	require.NoError(t, err, "body must be valid quoted-printable")
	return string(decoded)
}

func TestEmailProvider_Send_PlainFormatMode_SendsTextPlainOnly(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Plain Alert", Body: "Something happened", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.True(t, server.didAuthenticate(), "provider must authenticate before sending mail")

	data := server.capturedData()
	assert.Contains(t, data, "Subject: Plain Alert")
	assert.Contains(t, data, "Content-Type: text/plain")
	assert.Contains(t, data, "Something happened")
	assert.NotContains(t, data, "text/html")
}

func TestEmailProvider_Send_HTMLFormatMode_SendsTextHTMLOnly(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "HTML Alert", Body: "<p>Something happened</p>", FormatMode: "html"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)

	data := server.capturedData()
	assert.Contains(t, data, "Subject: HTML Alert")
	assert.Contains(t, data, "Content-Type: text/html")
	assert.Contains(t, data, "<p>Something happened</p>")
}

func TestEmailProvider_Send_MarkdownFormatMode_SendsHTMLWithPlainAlternative(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "MD Alert", Body: "**bold** text", FormatMode: "markdown"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)

	data := server.capturedData()
	assert.Contains(t, data, "Subject: MD Alert")
	assert.Contains(t, data, "multipart/alternative")
	assert.Contains(t, data, "Content-Type: text/plain")
	assert.Contains(t, data, "Content-Type: text/html")
	// The HTML part should contain rendered markdown (bold tag), the plain
	// part should retain the raw markdown source as the fallback.
	assert.Contains(t, data, "<strong>bold</strong>")
	assert.Contains(t, data, "**bold** text")
}

func TestEmailProvider_Send_ArabicSubjectAndBody_EncodedForSafeTransit(t *testing.T) {
	// Strict relays without 8BITMIME can mangle raw UTF-8 sent with an
	// implicit 7bit transfer encoding. The subject must be a valid RFC 2047
	// encoded word, and the body must declare and use quoted-printable so it
	// survives transit and decodes back to the original text.
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	arabicSubject := "تنبيه هام"
	arabicBody := "حدث خطأ في النظام، يرجى المراجعة فورا."
	req := provider.SendRequest{Subject: arabicSubject, Body: arabicBody, FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)

	data := server.capturedData()

	subjectValue := extractHeaderValue(t, data, "Subject")
	decodedSubject, err := (&mime.WordDecoder{}).DecodeHeader(subjectValue)
	require.NoError(t, err, "subject must be a valid RFC 2047 encoded word")
	assert.Equal(t, arabicSubject, decodedSubject)

	assert.Contains(t, data, "Content-Transfer-Encoding: quoted-printable")
	assert.NotContains(t, data, arabicBody, "raw UTF-8 body must not appear unencoded on the wire")

	decodedBody := decodeQuotedPrintableBodyPart(t, data)
	assert.Equal(t, arabicBody, decodedBody)
}

func TestEmailProvider_Send_DefaultFormatMode_TreatsAsPlain(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "No Mode", Body: "Body text"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)
	assert.Contains(t, server.capturedData(), "Content-Type: text/plain")
}

func TestEmailProvider_Send_ServerDropsConnectionAfterAcceptingData_StillReportsSuccess(t *testing.T) {
	// The server accepts the message at the DATA stage (the point at which
	// delivery is actually committed) but disconnects before replying to the
	// subsequent QUIT. That must not be reported as a failed send — doing so
	// would cause a spurious retry and a duplicate message.
	server := newSMTPTestServer(t)
	server.dropAfterData = true
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success, "message was accepted at DATA; a QUIT-stage disconnect must not flip this to failure")
}

func TestEmailProvider_Send_PropagatesRecipients(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	params := validEmailConfigParams(host, port)
	params.To = []string{"a@example.com", "b@example.com"}
	params.CC = []string{"c@example.com"}
	cfg := newEmailConfig(params)
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)

	to := server.capturedTo()
	assert.ElementsMatch(t, []string{"a@example.com", "b@example.com", "c@example.com"}, to)

	data := server.capturedData()
	assert.Contains(t, data, "To: a@example.com, b@example.com")
	assert.Contains(t, data, "Cc: c@example.com")
}

func TestEmailProvider_Send_SetsReplyToHeader(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	params := validEmailConfigParams(host, port)
	params.ReplyTo = "support@example.com"
	cfg := newEmailConfig(params)
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.True(t, resp.Success)
	assert.Contains(t, server.capturedData(), "Reply-To: support@example.com")
}

func TestEmailProvider_Send_NegotiatesSTARTTLS_WhenServerAdvertisesIt(t *testing.T) {
	server := newSMTPTestServerWithTLS(t)
	host, port := hostPort(t, server.addr)

	pool := x509.NewCertPool()
	pool.AddCert(server.leafCertificate(t))
	p := provider.NewEmailProviderWithTLSRootCAs(pool)

	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Secure", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Contains(t, server.capturedData(), "Secure")
}

func TestEmailProvider_Send_STARTTLS_RejectsUntrustedCertificate(t *testing.T) {
	// Without the test server's certificate in the trust pool, the STARTTLS
	// handshake must fail rather than silently falling back to plaintext.
	server := newSMTPTestServerWithTLS(t)
	host, port := hostPort(t, server.addr)

	p := provider.NewEmailProvider()
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Secure", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Empty(t, server.capturedData(), "message must not be delivered over an unverified connection")
}

func TestEmailProvider_Send_NoSTARTTLSAdvertised_RefusesToSendCredentialsInPlaintext(t *testing.T) {
	// A server that never advertises STARTTLS leaves no way to encrypt the
	// session on a non-465 port, so the provider must refuse before AUTH
	// rather than silently falling through to plaintext credentials.
	server := newSMTPTestServerWithoutTLS(t)
	host, port := hostPort(t, server.addr)

	p := provider.NewEmailProvider()
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "STARTTLS")
	assert.False(t, server.didAuthenticate(), "must not attempt AUTH without an encrypted channel")
	assert.Empty(t, server.capturedData(), "must not reach DATA without an encrypted channel")
}

// --- Error classification ---

func TestEmailProvider_Send_PermanentSMTPFailure_AuthenticationRejected(t *testing.T) {
	// net/smtp surfaces AUTH failures as a textproto.Error during the AUTH
	// exchange; simulate this by having the test server refuse authentication.
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}
	server.setDataReply(535, "Authentication credentials invalid")

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent, "5xx SMTP replies must be classified permanent")
}

func TestEmailProvider_Send_PermanentSMTPFailure_MailboxUnavailable(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}
	server.setDataReply(550, "Mailbox unavailable")

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
}

func TestEmailProvider_Send_TransientSMTPFailure_ServiceNotAvailable(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}
	server.setDataReply(421, "Service not available, closing transmission channel")

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent, "421 must be classified transient (retryable)")
}

func TestEmailProvider_Send_TransientSMTPFailure_MailboxBusy(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}
	server.setDataReply(450, "Mailbox busy")

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent)
}

func TestEmailProvider_Send_TransientSMTPFailure_LocalError(t *testing.T) {
	server := newSMTPTestServer(t)
	host, port := hostPort(t, server.addr)

	p := newEmailProviderTrusting(t, server)
	cfg := newEmailConfig(validEmailConfigParams(host, port))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}
	server.setDataReply(451, "Local error in processing")

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent)
}

func TestEmailProvider_Send_ConnectionError_IsTransient(t *testing.T) {
	p := provider.NewEmailProvider()
	// Port 0 on loopback is guaranteed to be refused.
	cfg := newEmailConfig(validEmailConfigParams("127.0.0.1", 1))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	resp, err := p.Send(context.Background(), cfg, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent, "connection errors must be retryable")
}

func TestEmailProvider_Send_ContextTimeout_IsTransient(t *testing.T) {
	// A non-routable address (RFC 5737 TEST-NET-1) causes the dial to hang
	// until the context deadline fires rather than failing immediately.
	p := provider.NewEmailProvider()
	cfg := newEmailConfig(validEmailConfigParams("192.0.2.1", 25))
	req := provider.SendRequest{Subject: "Subj", Body: "Body", FormatMode: "plain"}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	resp, err := p.Send(ctx, cfg, req)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent, "timeouts must be retryable")
	assert.Less(t, elapsed, 5*time.Second, "Send must respect the context deadline, not a longer default")
}

func TestEmailProvider_Send_InvalidConfigJSON_ReturnsError(t *testing.T) {
	p := provider.NewEmailProvider()

	resp, err := p.Send(context.Background(), json.RawMessage(`{not valid}`), provider.SendRequest{Body: "x"})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unmarshal") || strings.Contains(err.Error(), "email config"))
}
