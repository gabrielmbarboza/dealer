package config

import (
	"sync"
	"testing"
)

func TestStore_GetReturnsInitialValue(t *testing.T) {
	initial := &Config{Services: []Service{{Name: "seed"}}}
	store := NewStore(initial)

	got := store.Get()
	if got != initial {
		t.Fatalf("Get() = %p, want %p (initial)", got, initial)
	}
}

func TestStore_SetThenGetReturnsLatest(t *testing.T) {
	store := NewStore(&Config{Services: []Service{{Name: "first"}}})

	updated := &Config{Services: []Service{{Name: "second"}}}
	store.Set(updated)

	got := store.Get()
	if got != updated || got.Services[0].Name != "second" {
		t.Fatalf("Get() = %+v, want %+v", got, updated)
	}
}

func TestStore_ConcurrentGetSetIsRaceFree(t *testing.T) {
	store := NewStore(&Config{Services: []Service{{Name: "seed"}}})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			store.Set(&Config{Services: []Service{{Name: "writer"}}})
		}(i)
		go func() {
			defer wg.Done()
			if c := store.Get(); c == nil || len(c.Services) == 0 {
				t.Errorf("Get() returned invalid config: %+v", c)
			}
		}()
	}
	wg.Wait()
}
