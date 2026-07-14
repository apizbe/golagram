package golagram

import (
	"testing"
	"time"
)

func TestBackoff_DoublesAndCaps(t *testing.T) {
	b := NewBackoff(time.Second, 8*time.Second)

	want := []time.Duration{1, 2, 4, 8, 8, 8}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Errorf("Next() #%d = %v, want %v", i, got, w*time.Second)
		}
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := NewBackoff(time.Second, 8*time.Second)
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Errorf("Next() after Reset = %v, want %v", got, time.Second)
	}
}
