package golagram_test

import (
	"testing"

	gg "github.com/apizbe/golagram"
	"github.com/apizbe/golagram/fsmtest"
)

// MemoryStorage is the reference implementation — it must pass the same
// conformance suite any third-party backend is held to.
func TestMemoryStorage_Conformance(t *testing.T) {
	fsmtest.Run(t, func(t *testing.T) gg.FSMStorage {
		return gg.NewMemoryStorage()
	})
}
