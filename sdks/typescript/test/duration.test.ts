import { describe, expect, it } from "vitest";
import { parseGoDuration } from "../src/duration.js";

describe("parseGoDuration: valid inputs", () => {
  const cases: Array<[label: string, input: string, expectedMs: number]> = [
    ["hours, minutes, seconds", "24h0m0s", 24 * 60 * 60 * 1000],
    ["hours and minutes", "1h30m", (60 + 30) * 60 * 1000],
    ["seconds only", "45s", 45 * 1000],
    ["milliseconds", "500ms", 500],
    ["microseconds (us spelling)", "500us", 0.5],
    ["microseconds (µs, U+00B5 MICRO SIGN)", "1.5µs", 0.0015],
    ["microseconds (μs, U+03BC GREEK MU)", "1.5μs", 0.0015],
    ["nanoseconds", "1000000ns", 1],
    ["zero", "0s", 0],
    ["every unit combined", "1h2m3s4ms5us6ns", (3600 + 120 + 3) * 1000 + 4 + 0.005 + 0.000006],
  ];

  for (const [label, input, expectedMs] of cases) {
    it(`parses ${label} ("${input}")`, () => {
      expect(parseGoDuration(input)).toBeCloseTo(expectedMs, 9);
    });
  }
});

describe("parseGoDuration: rejected inputs", () => {
  const cases: Array<[label: string, input: string]> = [
    ["empty string", ""],
    ["no units at all", "not-a-duration"],
    ["garbage before a valid duration", "garbage 45s"],
    ["garbage after a valid duration", "45s garbage"],
    ["negative duration", "-5m"],
    ["negative with other units", "-1h30m"],
  ];

  for (const [label, input] of cases) {
    it(`rejects ${label} ("${input}")`, () => {
      expect(() => parseGoDuration(input)).toThrow();
    });
  }
});
