import { describe, expect, it } from "vitest";
import { parseGoDuration } from "../src/duration.js";

describe("parseGoDuration", () => {
  it("parses hours, minutes, and seconds", () => {
    expect(parseGoDuration("24h0m0s")).toBe(24 * 60 * 60 * 1000);
  });

  it("parses a subset of units", () => {
    expect(parseGoDuration("1h30m")).toBe((60 + 30) * 60 * 1000);
    expect(parseGoDuration("45s")).toBe(45 * 1000);
  });

  it("parses milliseconds", () => {
    expect(parseGoDuration("500ms")).toBe(500);
  });

  it("throws on an unparseable string", () => {
    expect(() => parseGoDuration("not-a-duration")).toThrow();
  });
});
