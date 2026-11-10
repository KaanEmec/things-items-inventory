// Small ULID generator: 48-bit millisecond time + 80 random bits, Crockford base32, 26 characters.
// Sorts by creation time; ids made in the same millisecond still increase.

const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
export const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/

let lastTime = -1
let lastRand: number[] = []

function randomDigits(): number[] {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes.slice(0, 16), (b) => b & 31)
}

function encodeTime(ms: number): string {
  let out = ''
  for (let i = 0; i < 10; i++) {
    out = ALPHABET[ms % 32] + out
    ms = Math.floor(ms / 32)
  }
  return out
}

export function ulid(now = Date.now()): string {
  if (now <= lastTime) {
    // Same millisecond (or clock stepped back): count up so order is kept.
    now = lastTime
    let i = 15
    while (i >= 0 && lastRand[i] === 31) lastRand[i--] = 0
    if (i >= 0) lastRand[i]++
  } else {
    lastTime = now
    lastRand = randomDigits()
  }
  return encodeTime(now) + lastRand.map((d) => ALPHABET[d]).join('')
}
