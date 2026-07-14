package provider_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpTestServer is a minimal, single-connection-at-a-time SMTP listener
// speaking just enough of RFC 5321 (plus STARTTLS/AUTH) to exercise a real
// net/smtp client end-to-end. It records the envelope and raw DATA payload of
// the last accepted message so tests can assert on the MIME structure the
// provider actually put on the wire.
type smtpTestServer struct {
	t        *testing.T
	listener net.Listener
	addr     string
	tlsConf  *tls.Config // non-nil enables STARTTLS and implicit-TLS support

	// replyCode/replyMsg let a test force a specific DATA-stage response, e.g.
	// to simulate a permanent 550 rejection or a transient 421 outage.
	dataReplyCode int
	dataReplyMsg  string

	// dropAfterData, when set, closes the connection right after accepting
	// DATA instead of waiting for QUIT. This simulates a server that accepted
	// the message but dropped the connection before session teardown, so
	// tests can verify the provider still reports success.
	dropAfterData bool

	mu          sync.Mutex
	lastFrom    string
	lastTo      []string
	lastData    string
	authAttempt bool
}

// newSMTPTestServer starts the listener on an ephemeral loopback port and
// registers cleanup. dataReplyCode/Msg default to "250 OK" style acceptance;
// override via the returned server's fields before the client connects.
func newSMTPTestServer(t *testing.T) *smtpTestServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := &smtpTestServer{
		t:             t,
		listener:      ln,
		addr:          ln.Addr().String(),
		dataReplyCode: 250,
		dataReplyMsg:  "OK",
	}

	t.Cleanup(func() { _ = ln.Close() })

	go s.acceptLoop()

	return s
}

// newSMTPTestServerWithTLS starts a server that advertises STARTTLS in its
// EHLO response and can complete a real TLS handshake using a freshly
// generated self-signed certificate, so tests can verify the provider
// actually upgrades the connection rather than sending credentials in the
// clear.
func newSMTPTestServerWithTLS(t *testing.T) *smtpTestServer {
	t.Helper()

	s := newSMTPTestServer(t)
	s.tlsConf = &tls.Config{Certificates: []tls.Certificate{generateSelfSignedCert(t)}}
	return s
}

// generateSelfSignedCert creates an in-memory self-signed certificate for
// 127.0.0.1, valid for the lifetime of the test.
func generateSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func (s *smtpTestServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *smtpTestServer) handleConn(conn net.Conn) {
	defer conn.Close() //nolint:errcheck

	writeLine(conn, "220 test.local ESMTP")
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			s.writeEhloResponse(conn)
		case strings.HasPrefix(upper, "STARTTLS"):
			writeLine(conn, "220 Ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConf)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
		case strings.HasPrefix(upper, "AUTH"):
			s.mu.Lock()
			s.authAttempt = true
			s.mu.Unlock()
			writeLine(conn, "235 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			s.mu.Lock()
			s.lastFrom = extractAddr(line)
			s.mu.Unlock()
			writeLine(conn, "250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			s.mu.Lock()
			s.lastTo = append(s.lastTo, extractAddr(line))
			s.mu.Unlock()
			writeLine(conn, "250 OK")
		case strings.HasPrefix(upper, "DATA"):
			s.handleData(conn, reader)
			if s.dropAfterData {
				return
			}
		case strings.HasPrefix(upper, "QUIT"):
			writeLine(conn, "221 Bye")
			return
		case strings.HasPrefix(upper, "RSET"):
			writeLine(conn, "250 OK")
		case strings.HasPrefix(upper, "NOOP"):
			writeLine(conn, "250 OK")
		default:
			writeLine(conn, "500 unrecognized command")
		}
	}
}

func (s *smtpTestServer) writeEhloResponse(conn net.Conn) {
	extensions := []string{"test.local greets you"}
	if s.tlsConf != nil {
		extensions = append(extensions, "STARTTLS")
	}
	extensions = append(extensions, "AUTH PLAIN LOGIN", "8BITMIME")

	for i, ext := range extensions {
		sep := "-"
		if i == len(extensions)-1 {
			sep = " "
		}
		fmt.Fprintf(conn, "250%s%s\r\n", sep, ext) //nolint:errcheck
	}
}

func (s *smtpTestServer) handleData(conn net.Conn, reader *bufio.Reader) {
	writeLine(conn, "354 Send message content")

	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimRight(line, "\r\n") == "." {
			break
		}
		body.WriteString(line)
	}

	s.mu.Lock()
	s.lastData = body.String()
	code, msg := s.dataReplyCode, s.dataReplyMsg
	s.mu.Unlock()

	writeLine(conn, fmt.Sprintf("%d %s", code, msg))
}

// setDataReply configures the response the server gives after DATA is
// submitted, letting tests simulate permanent or transient SMTP failures.
func (s *smtpTestServer) setDataReply(code int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dataReplyCode = code
	s.dataReplyMsg = msg
}

func (s *smtpTestServer) capturedData() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastData
}

func (s *smtpTestServer) capturedTo() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lastTo...)
}

// didAuthenticate reports whether the client issued an AUTH command before
// sending mail.
func (s *smtpTestServer) didAuthenticate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authAttempt
}

// leafCertificate parses the server's own TLS certificate so a test can add
// it to a client trust pool. Only valid for servers created via
// newSMTPTestServerWithTLS.
func (s *smtpTestServer) leafCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	cert, err := x509.ParseCertificate(s.tlsConf.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse server certificate: %v", err)
	}
	return cert
}

func writeLine(conn net.Conn, line string) {
	_, _ = fmt.Fprintf(conn, "%s\r\n", line)
}

// extractAddr pulls the bracketed address out of a "MAIL FROM:<x>" or
// "RCPT TO:<x>" command line.
func extractAddr(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return line[start+1 : end]
}
