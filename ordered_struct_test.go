package s2prot

import (
	"encoding/json"
	"testing"
)

func TestStructMarshalJSONOrder(t *testing.T) {
	s := Struct{}
	s["a"] = 1
	s["b"] = 2
	s["c"] = 3
	// specify order different from map iteration order
	s["__order"] = []string{"a", "b", "c"}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	got := string(b)
	want := "{\"a\":1,\"b\":2,\"c\":3}"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
}

func TestStructMarshalJSONSkipsOrderKey(t *testing.T) {
	s := Struct{"__order": []string{"k"}, "k": "v"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(b) != "{\"k\":\"v\"}" {
		t.Fatalf("unexpected json: %s", string(b))
	}
}
