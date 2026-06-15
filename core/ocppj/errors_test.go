package ocppj

import (
	"errors"
	"testing"
)

func TestSentinels_AreDistinct(t *testing.T) {
	all := []error{
		ErrConnClosed, ErrConnDropped, ErrNotConnected, ErrAlreadyConnected,
		ErrCallTimeout, ErrCallCancelled, ErrQueueFull, ErrConcurrentCallLimit,
		ErrUnknownAction, ErrHandlerNotRegistered, ErrInvalidDirection, ErrDuplicateHandler,
		ErrUnsupportedVersion, ErrVersionMismatch,
	}
	for i := range all {
		for j := range all {
			if i != j && errors.Is(all[i], all[j]) {
				t.Errorf("sentinel %d and %d are not distinct", i, j)
			}
		}
	}
}
