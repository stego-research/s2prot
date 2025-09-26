package rep

import (
	"bytes"
	"testing"
)

func TestNewEventsWithBuildCoercion_InvalidInput(t *testing.T) {
	// Empty buffer is not a valid MPQ/SC2Replay, should yield ErrInvalidRepFile
	r := bytes.NewReader([]byte{})
	rep, coercedTo, err := NewEventsWithBuildCoercion(r, true, true, true)
	if rep != nil {
		t.Fatalf("expected nil rep, got: %#v", rep)
	}
	if coercedTo != 0 {
		t.Fatalf("expected coercedTo=0 for invalid input, got: %d", coercedTo)
	}
	if err != ErrInvalidRepFile {
		t.Fatalf("expected ErrInvalidRepFile, got: %v", err)
	}
}

func TestNewFromFileEventsWithBuildCoercion_InvalidFile(t *testing.T) {
	// Non-existent file should yield ErrInvalidRepFile
	rep, coercedTo, err := NewFromFileEventsWithBuildCoercion("this_file_should_not_exist_12345.SC2Replay", true, true, true)
	if rep != nil {
		t.Fatalf("expected nil rep, got: %#v", rep)
	}
	if coercedTo != 0 {
		t.Fatalf("expected coercedTo=0 for invalid input, got: %d", coercedTo)
	}
	if err != ErrInvalidRepFile {
		t.Fatalf("expected ErrInvalidRepFile, got: %v", err)
	}
}
