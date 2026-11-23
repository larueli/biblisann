/** Returns the ISBN-13 for a valid ISBN-10 or ISBN-13 (hyphens and spaces allowed), else null. */
export function normalizeIsbn(raw: string): string | null {
  const s = raw
    .replace(/^isbn(-1[03])?:?/i, "")
    .replace(/[\s-]/g, "")
    .toUpperCase();
  if (/^\d{9}[\dX]$/.test(s)) {
    let sum = 0;
    for (let i = 0; i < 9; i++) sum += (10 - i) * Number(s[i]);
    const check = (11 - (sum % 11)) % 11;
    if ((check === 10 ? "X" : String(check)) !== s[9]) return null;
    const first12 = `978${s.slice(0, 9)}`;
    return first12 + String(check13(first12));
  }
  if (/^97[89]\d{10}$/.test(s) && check13(s.slice(0, 12)) === Number(s[12])) return s;
  return null;
}

function check13(first12: string): number {
  let sum = 0;
  for (let i = 0; i < 12; i++) sum += Number(first12[i]) * (i % 2 === 0 ? 1 : 3);
  return (10 - (sum % 10)) % 10;
}
