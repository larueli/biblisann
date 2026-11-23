/** Geometry and colour of a book spine, derived from the book itself so a shelf looks the same every time. */
export interface Spine {
  width: number;
  height: number;
  hue: number;
}

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

/** Thickness follows the page count (16 to 52 px), height varies a little (120 to 176 px). */
export function spine(id: string, title: string, pages: number | undefined): Spine {
  const width = pages && pages > 0 ? Math.min(52, Math.max(16, Math.round(12 + pages / 12))) : 28;
  return { width, height: 120 + (hash(id) % 5) * 14, hue: hash(title) % 360 };
}
