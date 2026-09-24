// Letters and digits that cannot be mistaken for one another when read aloud
// or copied by hand: no 0/O, 1/l/I.
const ALPHABET = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'

/**
 * generatePassword proposes a temporary password for an administrator to hand
 * over. It comes from the browser's cryptographic random source.
 */
export function generatePassword(length = 14): string {
  const limit = 256 - (256 % ALPHABET.length)
  const result: string[] = []
  while (result.length < length) {
    const bytes = crypto.getRandomValues(new Uint8Array(length * 2))
    for (const byte of bytes) {
      // Bytes past the largest multiple of the alphabet size are skipped, so
      // every character is equally likely.
      if (byte < limit && result.length < length) {
        result.push(ALPHABET.charAt(byte % ALPHABET.length))
      }
    }
  }
  return result.join('')
}
