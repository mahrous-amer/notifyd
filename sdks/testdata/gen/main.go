// Command gen produces sdks/testdata/signature_vectors.json — the shared
// fixture every language SDK's test suite verifies its signature helper
// against. It calls the same provider.SignHMAC function notifyd itself uses
// to sign outbound webhook requests, so the vectors are guaranteed to match
// production behavior rather than a hand-copied reimplementation of it.
//
// Run via sdks/testdata/regen_vectors.sh; not part of the notifyd build.
package main

import (
	"encoding/json"
	"os"

	"github.com/bse/notifyd/internal/provider"
)

// vector is one signature test case: given secret/timestamp/body, computing
// provider.SignHMAC(secret, timestamp, body) must equal signatureHex, and
// the full header value is what each SDK's verify helper expects to parse.
type vector struct {
	Name         string `json:"name"`
	Secret       string `json:"secret"`
	Timestamp    string `json:"timestamp"`
	Body         string `json:"body"`
	SignatureHex string `json:"signature_hex"`
	HeaderValue  string `json:"header_value"`
}

func main() {
	cases := []struct {
		name      string
		secret    string
		timestamp string
		body      string
	}{
		{
			name:      "simple_json_body",
			secret:    "whsec_test_1234567890abcdef",
			timestamp: "1700000000",
			body:      `{"id":"evt_01H9X8K2QYTVRM3F7WZC4B5D6E","type":"notification.delivered"}`,
		},
		{
			name:      "empty_body",
			secret:    "whsec_test_1234567890abcdef",
			timestamp: "1700000001",
			body:      "",
		},
		{
			name:      "unicode_body",
			secret:    "whsec_unicode_secret_ñ",
			timestamp: "1700000002",
			body:      `{"message":"café ☃ 😀"}`,
		},
		{
			name:      "long_body",
			secret:    "whsec_long_body_secret",
			timestamp: "1700000003",
			body:      `{"data":"` + repeat("a", 500) + `"}`,
		},
		{
			name:      "body_containing_dot_literal",
			secret:    "whsec_dot_edge_case",
			timestamp: "1700000004",
			body:      `{"note":"1700000004.not-the-real-timestamp"}`,
		},
	}

	vectors := make([]vector, 0, len(cases))
	for _, c := range cases {
		sig := provider.SignHMAC(c.secret, c.timestamp, []byte(c.body))
		vectors = append(vectors, vector{
			Name:         c.name,
			Secret:       c.secret,
			Timestamp:    c.timestamp,
			Body:         c.body,
			SignatureHex: sig,
			HeaderValue:  "sha256=" + sig,
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(vectors); err != nil {
		panic(err)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
