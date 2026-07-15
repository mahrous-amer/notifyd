/**
 * Parses a Go-style duration string (e.g. "24h0m0s", "1h30m", "45s") into
 * milliseconds. notifyd's TokenResponse.expires_in is produced by Go's
 * time.Duration.String(), so the SDK needs to understand that format
 * specifically rather than a generic duration parser.
 */
export function parseGoDuration(duration: string): number {
  // "ms" must be listed before "m" and "s" — regex alternation matches the
  // first alternative that fits, so "m" would otherwise consume the "m" in
  // "500ms" and leave a dangling, unmatched "s".
  const unitPattern = /(\d+(?:\.\d+)?)(h|ms|m|s)/g;
  const millisecondsPerUnit: Record<string, number> = {
    h: 60 * 60 * 1000,
    m: 60 * 1000,
    s: 1000,
    ms: 1,
  };

  let totalMilliseconds = 0;
  let matchedAnything = false;
  for (const match of duration.matchAll(unitPattern)) {
    matchedAnything = true;
    const [, amount, unit] = match;
    totalMilliseconds += parseFloat(amount) * millisecondsPerUnit[unit];
  }

  if (!matchedAnything) {
    throw new Error(`notifyd: unparseable duration "${duration}"`);
  }
  return totalMilliseconds;
}
