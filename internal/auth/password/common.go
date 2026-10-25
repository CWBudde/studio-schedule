package password

import (
	_ "embed"
	"strings"
	"sync"
)

// commonList holds the 10,000 most common passwords, one per line: the list
// 10k-most-common.txt of SecLists (MIT License; source, checksum and license
// in common-passwords-LICENSE.txt). It is embedded, so the check needs no
// network and works in the WASM demo as on the server.
//
//go:embed common-passwords.txt
var commonList string

// common is the set of commonList in lower case, built on first use: only
// setting a password needs it, signing in does not.
var common = sync.OnceValue(func() map[string]struct{} {
	set := make(map[string]struct{}, strings.Count(commonList, "\n")+1)

	for line := range strings.Lines(commonList) {
		if pw := strings.TrimRight(line, "\r\n"); pw != "" {
			set[strings.ToLower(pw)] = struct{}{}
		}
	}

	return set
})

// IsCommon reports whether pw is one of the 10,000 most common passwords,
// ignoring case (ASVS 2.1.7).
func IsCommon(pw string) bool {
	_, ok := common()[strings.ToLower(pw)]

	return ok
}
