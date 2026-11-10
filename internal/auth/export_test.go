package auth

import "testing"

// HoldHashSlotsForTest takes every argon2 slot until the returned function
// is called.
func HoldHashSlotsForTest(t testing.TB) (release func()) {
	t.Helper()
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	return func() {
		for range cap(hashSlots) {
			<-hashSlots
		}
	}
}
