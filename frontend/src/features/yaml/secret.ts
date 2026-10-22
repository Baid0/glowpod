/** A Secret value decoded from base64: text when it's UTF-8, else just its size. */
export function decodeSecretValue(b64: string): { text?: string; bytes: number } {
  let bin: string;
  try {
    bin = atob(b64);
  } catch {
    return { bytes: 0 };
  }
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
  try {
    return { text: new TextDecoder("utf-8", { fatal: true }).decode(bytes), bytes: bytes.length };
  } catch {
    return { bytes: bytes.length };
  }
}
