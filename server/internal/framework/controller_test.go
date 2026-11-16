package framework

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

const ctrlPath = "/proj/app/http/controllers/city_controller.go"

func ctrlSrc(doc, body string) string {
	return "package controllers\n\ntype CityController struct{}\n\n" + doc + "func (c *CityController) Index() string {\n" + body + "\n\treturn \"\"\n}\n"
}

func diagContains(ds []Diag, sub string) bool {
	for _, d := range ds {
		if strings.Contains(d.Message, sub) {
			return true
		}
	}
	return false
}

// ---------- positive ----------

func TestController_Positive_ValidSwaggerRouterNoDiagnostics(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Router /api/v1/geo/cities [get]\n", "")
	if ds := ControllerDiagnostics(ctrlPath, src, cat); len(ds) != 0 {
		t.Errorf("expected none, got %+v", ds)
	}
}

func TestController_Positive_SwaggerKnownRequestType(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Param body body requests.CreateCityRequest true \"b\"\n", "")
	if ds := ControllerDiagnostics(ctrlPath, src, cat); diagContains(ds, "CreateCityRequest") {
		t.Errorf("known request type must not warn, got %+v", ds)
	}
}

func TestController_Positive_ResponseSuffixTypesAllowed(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Success 200 {object} responses.CityListResponse\n", "")
	if ds := ControllerDiagnostics(ctrlPath, src, cat); diagContains(ds, "CityListResponse") {
		t.Errorf("*Response types are exempt, got %+v", ds)
	}
}

func TestController_Positive_SwaggerPathTrailingSlashNormalized(t *testing.T) {
	if normalizeSwagPath("api/v1/") != "/api/v1" || normalizeSwagPath(" /a/ ") != "/a" {
		t.Error("normalizeSwagPath should add leading and strip trailing slash")
	}
}

// ---------- negative ----------

func TestController_Negative_SwaggerRouterMismatch(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Router /api/v1/wrong [get]\n", "")
	ds := ControllerDiagnostics(ctrlPath, src, cat)
	if !diagContains(ds, "does not match registered route (GET /api/v1/geo/cities)") {
		t.Errorf("expected router mismatch, got %+v", ds)
	}
}

func TestController_Negative_SwaggerMethodMismatch(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Router /api/v1/geo/cities [post]\n", "")
	if ds := ControllerDiagnostics(ctrlPath, src, cat); !diagContains(ds, "does not match registered route") {
		t.Errorf("expected method mismatch, got %+v", ds)
	}
}

func TestController_Negative_SwaggerUnknownType(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Param body body requests.GhostRequest true \"b\"\n", "")
	if ds := ControllerDiagnostics(ctrlPath, src, cat); !diagContains(ds, "Swagger type 'GhostRequest' not found") {
		t.Errorf("expected unknown swagger type, got %+v", ds)
	}
}

func TestController_Negative_RouteParamNotDefined(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("", "\t_ = ctx.Request().Route(\"id\")")
	ds := ControllerDiagnostics(ctrlPath, src, cat)
	if !diagContains(ds, "Route parameter 'id' not found on CityController.Index (route has no path parameters)") {
		t.Errorf("expected route param warning, got %+v", ds)
	}
}

func TestController_Negative_UnregisteredActionNotChecked(t *testing.T) {
	cat := indexer.Build(fixture)
	src := "package controllers\n\ntype CityController struct{}\n\n// @Router /nope [get]\nfunc (c *CityController) Destroy() {\n\t_ = ctx.Request().Route(\"id\")\n}\n"
	if ds := ControllerDiagnostics(ctrlPath, src, cat); diagContains(ds, "Route parameter") || diagContains(ds, "@Router") {
		t.Errorf("actions without routes skip route checks, got %+v", ds)
	}
}

func TestController_Negative_NotAControllerFile(t *testing.T) {
	cat := indexer.Build(fixture)
	src := ctrlSrc("// @Router /bad [get]\n", "")
	if ds := ControllerDiagnostics("/proj/app/services/x.go", src, cat); ds != nil {
		t.Errorf("non-controller files ignored, got %+v", ds)
	}
}

func TestController_Negative_SyntaxError(t *testing.T) {
	cat := indexer.Build(fixture)
	if ds := ControllerDiagnostics(ctrlPath, "package controllers\nfunc (", cat); ds != nil {
		t.Errorf("syntax error should yield nil, got %+v", ds)
	}
}
