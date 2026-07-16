package notifyd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// signatureVector mirrors one entry in sdks/testdata/signature_vectors.json.
type signatureVector struct {
	Name         string `json:"name"`
	Secret       string `json:"secret"`
	Timestamp    string `json:"timestamp"`
	Body         string `json:"body"`
	SignatureHex string `json:"signature_hex"`
	HeaderValue  string `json:"header_value"`
}

func loadSignatureVectors(t *testing.T) []signatureVector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "signature_vectors.json"))
	if err != nil {
		t.Fatalf("reading shared signature vectors: %v", err)
	}
	var vectors []signatureVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parsing shared signature vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("signature_vectors.json is empty")
	}
	return vectors
}

func TestVerifyWebhookSignature_SharedVectors(t *testing.T) {
	for _, v := range loadSignatureVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			err := VerifyWebhookSignature(v.Secret, v.Timestamp, []byte(v.Body), v.HeaderValue)
			if err != nil {
				t.Fatalf("VerifyWebhookSignature: %v (expected valid signature %s)", err, v.HeaderValue)
			}
		})
	}
}

func TestVerifyWebhookSignature_RejectsTamperedBody(t *testing.T) {
	vectors := loadSignatureVectors(t)
	v := vectors[0]

	err := VerifyWebhookSignature(v.Secret, v.Timestamp, []byte(v.Body+"tampered"), v.HeaderValue)
	if err != ErrInvalidSignature {
		t.Fatalf("got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhookSignature_RejectsWrongSecret(t *testing.T) {
	vectors := loadSignatureVectors(t)
	v := vectors[0]

	err := VerifyWebhookSignature("wrong-secret", v.Timestamp, []byte(v.Body), v.HeaderValue)
	if err != ErrInvalidSignature {
		t.Fatalf("got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhookSignature_RejectsMalformedHeader(t *testing.T) {
	vectors := loadSignatureVectors(t)
	v := vectors[0]

	for _, malformed := range []string{
		v.SignatureHex,           // missing "sha256=" prefix
		"sha1=" + v.SignatureHex, // wrong algorithm prefix
		"sha256=not-hex!!",       // undecodable hex
		"",
	} {
		err := VerifyWebhookSignature(v.Secret, v.Timestamp, []byte(v.Body), malformed)
		if err != ErrInvalidSignature {
			t.Errorf("header %q: got %v, want ErrInvalidSignature", malformed, err)
		}
	}
}
