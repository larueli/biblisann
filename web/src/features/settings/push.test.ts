import { describe, expect, it } from "vitest";
import { urlBase64ToUint8Array } from "./push";

describe("urlBase64ToUint8Array", () => {
  it("decodes base64url without padding", () => {
    // "hello?>" encodes to aGVsbG8_Pg in base64url (the standard form has + or / there)
    expect(Array.from(urlBase64ToUint8Array("aGVsbG8_Pg"))).toEqual(
      Array.from(new TextEncoder().encode("hello?>")),
    );
  });
  it("handles the 65-byte VAPID key length", () => {
    const key = btoa(String.fromCharCode(...new Uint8Array(65).fill(4)))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
    expect(urlBase64ToUint8Array(key)).toHaveLength(65);
  });
});
