// Package id mints ULIDs: 26 characters of Crockford base32, a 48-bit
// millisecond timestamp followed by 80 random bits, sortable by creation time.
package id

import (
	"crypto/rand"
	"sync"
	"time"
)

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
)

// New returns a new ULID. IDs minted by one process are strictly increasing,
// even within the same millisecond.
func New() string {
	mu.Lock()
	ms := uint64(time.Now().UnixMilli())
	if ms <= lastMS {
		ms = lastMS
		for i := len(lastRand) - 1; i >= 0; i-- { // increment the random part
			lastRand[i]++
			if lastRand[i] != 0 {
				break
			}
		}
	} else {
		lastMS = ms
		if _, err := rand.Read(lastRand[:]); err != nil {
			panic("id: crypto/rand: " + err.Error())
		}
	}
	var b [16]byte // 6 bytes time + 10 bytes random
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (8 * (5 - i)))
	}
	copy(b[6:], lastRand[:])
	mu.Unlock()

	// 128 bits -> 26 groups of 5 bits (the first group holds only 3 bits).
	var out [26]byte
	var acc uint
	bits, o := 0, 25
	for i := 15; i >= 0; i-- {
		acc |= uint(b[i]) << bits
		bits += 8
		for bits >= 5 && o >= 0 {
			out[o] = alphabet[acc&31]
			acc >>= 5
			bits -= 5
			o--
		}
	}
	if o == 0 {
		out[0] = alphabet[acc&31]
	}
	return string(out[:])
}
