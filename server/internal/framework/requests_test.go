package framework

import (
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func TestFieldTagKeyPrefersColAndSnake(t *testing.T) {
	f := indexer.StructField{
		Col:    "user_id",
		Alt:    []string{"userid", "user_id"},
		GoName: "UserID",
	}
	if got := fieldTagKey(f); got != "user_id" {
		t.Fatalf("expected user_id, got %q", got)
	}

	fNoCol := indexer.StructField{
		Alt:    []string{"createdat"},
		GoName: "CreatedAt",
	}
	if got := fieldTagKey(fNoCol); got != "created_at" {
		t.Fatalf("expected created_at, got %q", got)
	}
}

func TestRouteAndControllerHoverOutOfBounds(t *testing.T) {
	cat := indexer.NewCatalog()
	if got := RouteAndControllerHover("/proj/routes/api.go", "package routes", cat, -5); got != nil {
		t.Fatalf("expected nil for negative offset, got %+v", got)
	}
	if got := RouteAndControllerHover("/proj/routes/api.go", "package routes", cat, 999); got != nil {
		t.Fatalf("expected nil for out-of-bounds offset, got %+v", got)
	}
}
