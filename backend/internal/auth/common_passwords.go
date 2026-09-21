package auth

import (
	_ "embed"
	"strings"
	"sync"
)

// The denylist is the single most effective password rule available: it rejects
// what attackers actually try first, without asking anyone to remember a symbol.
// Kept deliberately small — a full breach corpus belongs in a file, not a
// binary, and this covers the shapes a household would plausibly pick.
//
// common_passwords.txt here is a COPY. shared/common_passwords.txt is canonical
// and is copied in by scripts/sync-denylist.sh, gated by `make check`: both
// backends reject the same passwords or they are not the same app. It is copied
// rather than read in place because //go:embed cannot reference a parent
// directory. Never hand-edit the copy.
//
//go:embed common_passwords.txt
var commonPasswordsRaw string

var commonPasswords = sync.OnceValue(func() map[string]struct{} {
	set := make(map[string]struct{})
	for line := range strings.SplitSeq(commonPasswordsRaw, "\n") {
		if entry := strings.TrimSpace(strings.ToLower(line)); entry != "" && !strings.HasPrefix(entry, "#") {
			set[entry] = struct{}{}
		}
	}
	return set
})

// Compared case-insensitively: Password1234 and password1234 are the same
// guess, and an attacker tries both.
func isCommonPassword(password string) bool {
	_, found := commonPasswords()[strings.ToLower(password)]
	return found
}
