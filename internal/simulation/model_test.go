package simulation

import (
	"testing"
	"time"
)

func TestPhysicalModel(t *testing.T) {
	for device := 0; device < 20; device++ {
		model := New(42, device)
		for i := 0; i < 1000; i++ {
			now := time.Now().UTC()
			m := model.Next("meter-1", "session-1", "msg-1", uint64(i+1), now, time.Second)
			if err := m.Validate(now); err != nil {
				t.Fatalf("device=%d step=%d: %v", device, i, err)
			}
			if m.Temperature < 25 || m.Temperature > 60 {
				t.Fatalf("unstable thermal model: %v", m.Temperature)
			}
		}
	}
}

func TestSeedIsReproducible(t *testing.T) {
	a, b := New(42, 1), New(42, 1)
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		x := a.Next("d", "s", "m", uint64(i+1), now, time.Second)
		y := b.Next("d", "s", "m", uint64(i+1), now, time.Second)
		if x != y {
			t.Fatal("same seed produced different results")
		}
	}
}
