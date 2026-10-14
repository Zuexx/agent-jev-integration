package stats

import (
	"sync"
	"testing"
)

func TestCounter(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c.Add("e")
			}
		}()
	}
	wg.Wait()
	counts, total := c.Snapshot()
	if counts["e"] != 10000 || total != 10000 {
		t.Fatalf("counts=%d total=%d", counts["e"], total)
	}
}
