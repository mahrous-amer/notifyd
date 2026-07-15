#!/usr/bin/env bash
# Regenerates sdks/testdata/signature_vectors.json from notifyd's own
# provider.SignHMAC implementation (internal/provider/hmac_signing.go).
#
# All three SDKs' signature-verification tests read this file, so it is the
# single source of truth for "does this language's HMAC helper agree with
# what notifyd actually sends." Run this after any change to SignHMAC and
# commit the resulting JSON diff alongside it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

go run ./sdks/testdata/gen > sdks/testdata/signature_vectors.json

echo "Wrote sdks/testdata/signature_vectors.json"
