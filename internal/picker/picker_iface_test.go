package picker

import (
	"testing"
)

// Verify Builtin implements Interface at compile time.
var _ Interface = (*Builtin)(nil)

func TestBuiltin_ImplementsInterface(t *testing.T) {
	b := &Builtin{}
	_ = Interface(b) // compile-time check
}
