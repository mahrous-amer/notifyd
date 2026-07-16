import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { verifyWebhookSignature } from "../src/signature.js";

interface SignatureVector {
  name: string;
  secret: string;
  timestamp: string;
  body: string;
  signature_hex: string;
  header_value: string;
}

function loadSignatureVectors(): SignatureVector[] {
  const vectorsPath = fileURLToPath(new URL("../../testdata/signature_vectors.json", import.meta.url));
  const vectors = JSON.parse(readFileSync(vectorsPath, "utf8")) as SignatureVector[];
  if (vectors.length === 0) {
    throw new Error("signature_vectors.json is empty");
  }
  return vectors;
}

describe("verifyWebhookSignature against shared cross-language vectors", () => {
  const vectors = loadSignatureVectors();

  it.each(vectors.map((v) => [v.name, v] as const))("%s", (_name, vector) => {
    const isValid = verifyWebhookSignature(
      vector.secret,
      vector.timestamp,
      vector.body,
      vector.header_value,
    );
    expect(isValid).toBe(true);
  });
});

describe("verifyWebhookSignature rejects invalid input", () => {
  const [vector] = loadSignatureVectors();

  it("rejects a tampered body", () => {
    const isValid = verifyWebhookSignature(
      vector.secret,
      vector.timestamp,
      vector.body + "tampered",
      vector.header_value,
    );
    expect(isValid).toBe(false);
  });

  it("rejects the wrong secret", () => {
    const isValid = verifyWebhookSignature(
      "wrong-secret",
      vector.timestamp,
      vector.body,
      vector.header_value,
    );
    expect(isValid).toBe(false);
  });

  it.each([
    ["missing sha256= prefix", vector.signature_hex],
    ["wrong algorithm prefix", `sha1=${vector.signature_hex}`],
    ["undecodable hex", "sha256=not-hex!!"],
    ["empty header", ""],
  ])("rejects a malformed header (%s)", (_label, malformedHeader) => {
    const isValid = verifyWebhookSignature(vector.secret, vector.timestamp, vector.body, malformedHeader);
    expect(isValid).toBe(false);
  });
});
