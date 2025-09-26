package rep

import (
	"reflect"
	"testing"

	"github.com/stego-research/s2prot/build"
)

func TestFindClosestSupportedBaseBuild(t *testing.T) {
	// Save originals and restore after
	origBuilds := make(map[int]string)
	for k, v := range build.Builds {
		origBuilds[k] = v
	}
	origDups := make(map[int]int)
	for k, v := range build.Duplicates {
		origDups[k] = v
	}
	defer func() {
		// restore
		for k := range build.Builds {
			delete(build.Builds, k)
		}
		for k, v := range origBuilds {
			build.Builds[k] = v
		}
		for k := range build.Duplicates {
			delete(build.Duplicates, k)
		}
		for k, v := range origDups {
			build.Duplicates[k] = v
		}
	}()

	// Prepare synthetic supported builds
	for k := range build.Builds {
		delete(build.Builds, k)
	}
	for k := range build.Duplicates {
		delete(build.Duplicates, k)
	}
	build.Builds[100] = "a"
	build.Builds[150] = "b"
	build.Builds[300] = "c"
	build.Duplicates[160] = 150 // 160 supported via duplicate

	cases := []struct {
		base   int
		want   int
		found  bool
	}{
		{99, 100, true},   // below first
		{101, 100, true},  // closer to 100
		{155, 150, true},  // closer to 150 than 160
		{280, 300, true},  // closer to 300
	}

	for _, c := range cases {
		got, ok := findClosestSupportedBaseBuild(c.base)
		if ok != c.found || got != c.want {
			t.Fatalf("base=%d: got=(%d,%v), want=(%d,%v)", c.base, got, ok, c.want, c.found)
		}
	}

	// When there are no builds at all
	for k := range build.Builds {
		delete(build.Builds, k)
	}
	for k := range build.Duplicates {
		delete(build.Duplicates, k)
	}
	if got, ok := findClosestSupportedBaseBuild(12345); ok || got != 0 {
		t.Fatalf("expected not found with 0, got (%d,%v)", got, ok)
	}

	_ = reflect.TypeOf(0) // keep reflect import used if conditions change
}
