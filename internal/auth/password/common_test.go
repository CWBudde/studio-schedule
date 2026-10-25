package password_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
)

func TestIsCommon(t *testing.T) {
	t.Parallel()

	for _, pw := range []string{"password", "PassWord", "12345678", "qwertyuiop", "iloveyou", "SUNSHINE"} {
		if !password.IsCommon(pw) {
			t.Errorf("IsCommon(%q) = false, want true", pw)
		}
	}

	// Entries shorter than MinLength are left out of the set: the length rule
	// refuses them before the list is asked.
	for _, pw := range []string{"123456", "qwerty", "monkey", "dragon", "1234567"} {
		if password.IsCommon(pw) {
			t.Errorf("IsCommon(%q) = true, want false (shorter than %d characters)", pw, password.MinLength)
		}
	}

	// The fictional passwords of the demo and the tests are not common.
	for _, pw := range []string{"demo-password", "correct horse", "a rare phrase of words", "password!x", ""} {
		if password.IsCommon(pw) {
			t.Errorf("IsCommon(%q) = true, want false", pw)
		}
	}
}

func BenchmarkIsCommon(b *testing.B) {
	password.IsCommon("warm-up") // builds the set once

	for b.Loop() {
		password.IsCommon("a rare phrase of words")
	}
}
