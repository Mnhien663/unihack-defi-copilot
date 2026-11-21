/**
 * Stable JSON representation for deposit review comparisons.
 * Object property order is ignored, but every value, key, type,
 * array position and raw amount string must match exactly.
 */
export function canonicalDepositJson(value: unknown): string {
  function encode(input: unknown): string {
    if (input === null || typeof input === 'string' || typeof input === 'boolean' ||
        (typeof input === 'number' && Number.isFinite(input))) {
      const encoded = JSON.stringify(input);
      if (typeof encoded !== 'string') throw new Error('Invalid deposit summary JSON value.');
      return encoded;
    }
    if (Array.isArray(input)) {
      return `[${input.map(encode).join(',')}]`;
    }
    if (input !== null && typeof input === 'object') {
      const fields = Object.keys(input).sort().map((key) => {
        const record = input as Record<string, unknown>;
        return `${JSON.stringify(key)}:${encode(record[key])}`;
      });
      return `{${fields.join(',')}}`;
    }
    throw new Error('Invalid deposit summary JSON value.');
  }
  return encode(value);
}
