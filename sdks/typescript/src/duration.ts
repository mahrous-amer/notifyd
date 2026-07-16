// One (amount, unit) pair, e.g. the "4ms" in "1h2m3s4ms". Unit alternatives
// list "ns"/"us"/"µs"/"μs"/"ms" before the bare "m" and "s" they'd
// otherwise be swallowed by — regex alternation matches the first
// alternative that fits, so "m" would consume the "m" in "500ms" and leave
// a dangling, unmatched "s" if it came first.
const DURATION_UNIT = String.raw`(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|h|m|s)`;

// Anchored to match one or more consecutive unit pairs spanning the ENTIRE
// string, with nothing before or after. Go's own time.ParseDuration is
// similarly strict: "45s garbage" and "garbage 45s" are both parse errors,
// not "45 seconds with some ignored trailing/leading text."
const FULL_DURATION_PATTERN = new RegExp(`^(?:${DURATION_UNIT})+$`);
const UNIT_PATTERN = new RegExp(DURATION_UNIT, "g");

const MILLISECONDS_PER_UNIT: Record<string, number> = {
  h: 60 * 60 * 1000,
  m: 60 * 1000,
  s: 1000,
  ms: 1,
  us: 1e-3,
  "µs": 1e-3, // U+00B5 MICRO SIGN
  "μs": 1e-3, // U+03BC GREEK SMALL LETTER MU — Go accepts both spellings
  ns: 1e-6,
};

/**
 * Parses a Go-style duration string (e.g. "24h0m0s", "1h30m", "500us") into
 * milliseconds. notifyd's TokenResponse.expires_in is produced by Go's
 * time.Duration.String(), so the SDK needs to understand that format
 * specifically rather than a generic duration parser.
 *
 * Unlike Go's time.ParseDuration, this rejects negative durations. A
 * negative expires_in can never be legitimate for a token lifetime, so
 * treating it as a parse error catches a malformed/malicious response
 * instead of silently caching a token as "already expired" or
 * "expires in the far past."
 */
export function parseGoDuration(duration: string): number {
  if (duration.startsWith("-")) {
    throw new Error(`notifyd: negative duration not allowed: "${duration}"`);
  }
  if (!FULL_DURATION_PATTERN.test(duration)) {
    throw new Error(`notifyd: unparseable duration "${duration}"`);
  }

  let totalMilliseconds = 0;
  for (const [, amount, unit] of duration.matchAll(UNIT_PATTERN)) {
    totalMilliseconds += parseFloat(amount) * MILLISECONDS_PER_UNIT[unit];
  }
  return totalMilliseconds;
}
