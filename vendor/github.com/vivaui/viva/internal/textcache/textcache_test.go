package textcache

import "testing"

func TestTrimLargeTextReleasesOldKeys(t *testing.T) {
	var released []int
	cache := New(2, func(value int) { released = append(released, value) })
	short := Key{Value: "ui"}
	old := Key{Value: "old long line"}
	latest := Key{Value: "most recent long line"}
	cache.Put(short, 1)
	cache.Put(old, 2)
	cache.Put(latest, 3)

	cache.TrimLargeText(4, 1, 2)
	if len(released) != 0 {
		t.Fatalf("trimming released a visible pair: %v", released)
	}
	cache.TrimLargeText(4, len(latest.Value), 1)
	if _, found := cache.Get(old); found || len(released) != 1 || released[0] != 2 {
		t.Fatalf("old long line still held or released incorrectly: %v", released)
	}
	if _, found := cache.Get(latest); !found {
		t.Fatal("trimming evicted the line being drawn")
	}

	cache.TrimLargeText(4, 0, 0)
	if _, found := cache.Get(short); !found || cache.Len() != 1 || len(released) != 2 || released[1] != 3 {
		t.Fatalf("unexpected entries after dropping long layouts: %d, releases %v", cache.Len(), released)
	}
	cache.Put(old, 4)
	if value, found := cache.Get(old); !found || value != 4 {
		t.Fatal("cache could not reuse a released entry")
	}
}
