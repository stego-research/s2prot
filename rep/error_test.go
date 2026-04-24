package rep

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrUnsupportedRepVersion_Metadata(t *testing.T) {
	bb := 12345
	err := fmt.Errorf("%w, metadata: {\"build\": %d}", ErrUnsupportedRepVersion, bb)

	if !errors.Is(err, ErrUnsupportedRepVersion) {
		t.Errorf("errors.Is(err, ErrUnsupportedRepVersion) = false, want true")
	}

	expectedMsg := "Unsupported replay version, metadata: {\"build\": 12345}"
	if err.Error() != expectedMsg {
		t.Errorf("err.Error() = %q, want %q", err.Error(), expectedMsg)
	}
}
