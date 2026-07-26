package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The public browser terminal runs this same binary against production. If the
// guard ever moves to the UI layer or a new write method skips it, this test is
// what catches it.
func TestReadOnlyBlocksEveryWrite(t *testing.T) {
	src := NewLiveSource("http://localhost:8099", 5*time.Second)
	src.SetReadOnly(true)
	ctx := context.Background()

	if _, err := src.StudioSaveDraft(ctx, map[string]any{}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("StudioSaveDraft: got %v, want ErrReadOnly", err)
	}
	if _, err := src.StudioPublish(ctx, "test"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("StudioPublish: got %v, want ErrReadOnly", err)
	}
	if _, err := src.StudioRollback(ctx, 1, "test"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("StudioRollback: got %v, want ErrReadOnly", err)
	}
}
