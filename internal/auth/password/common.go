package password

import (
	_ "embed"
	"strings"
	"sync"
	"unicode/utf8"
)

// MinLength is the fewest characters a new password may have (ADR 0004).
// The service refuses a shorter one before it asks IsCommon, so the set
// keeps only the common passwords of at least this length.
const MinLength = 8

// commonList holds the 10,000 most common passwords, one per line: the list
// 10k-most-common.txt of SecLists (MIT License; source, checksum and license
// in common-passwords-LICENSE.txt). It is embedded, so the check needs no
// network and works in the WASM demo as on the server.
//
//go:embed common-passwords.txt
var commonList string

// common is the set of the entries of commonList with at least MinLength
// characters, in lower case, built on first use: only setting a password
// needs it, signing in does not. A shorter entry can never match a password
// the length rule lets through; leaving those out keeps about 2,100 of the
// 10,000 in memory.
var common = sync.OnceValue(func() map[string]struct{} {
	set := make(map[string]struct{})

	for line := range strings.Lines(commonList) {
		pw := strings.ToLower(strings.TrimRight(line, "\r\n"))
		if utf8.RuneCountInString(pw) >= MinLength {
			set[pw] = struct{}{}
		}
	}

	return set
})

// IsCommon reports whether pw is one of the 10,000 most common passwords,
// ignoring case (ASVS 2.1.7). It knows only those of at least MinLength
// characters: a shorter pw is never common, the length rule refuses it.
func IsCommon(pw string) bool {
	_, ok := common()[strings.ToLower(pw)]

	return ok
}
