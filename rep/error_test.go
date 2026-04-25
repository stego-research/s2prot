package rep

import (
	"errors"
	"testing"
)

func TestUnsupportedRepVersionError_ErrorsIs(t *testing.T) {
	err := &UnsupportedRepVersionError{Build: 12345}

	if !errors.Is(err, ErrUnsupportedRepVersion) {
		t.Errorf("errors.Is(err, ErrUnsupportedRepVersion) = false, want true")
	}
}

func TestUnsupportedRepVersionError_MessageWithoutClosest(t *testing.T) {
	err := &UnsupportedRepVersionError{Build: 12345}

	expected := "Unsupported replay version, metadata: {\"build\": 12345}"
	if err.Error() != expected {
		t.Errorf("err.Error() = %q, want %q", err.Error(), expected)
	}
}

func TestUnsupportedRepVersionError_MessageWithClosest(t *testing.T) {
	err := &UnsupportedRepVersionError{Build: 12345, Closest: 96702}

	expected := "Unsupported replay version, metadata: {\"build\": 12345, \"closest\": 96702}"
	if err.Error() != expected {
		t.Errorf("err.Error() = %q, want %q", err.Error(), expected)
	}
}
