package webconsole

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedConsoleAndAPIBoundary(t *testing.T) {
	handler := Handler()
	for _, path := range []string{"/api/unknown", "/api/v1/undeclared", "/other", "/console/assets/missing.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d, want404", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/console/", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "<div id=\"root\">") {
		t.Fatalf("console status=%d body=%q", response.Code, response.Body.String())
	}
	assets := regexp.MustCompile(`(?:src|href)="(/console/assets/[^"]+)"`).FindAllStringSubmatch(response.Body.String(), -1)
	if len(assets) < 2 {
		t.Fatal("console is missing compiled scripts/styles")
	}
	for _, asset := range assets {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest(http.MethodGet, asset[1], nil))
		data, _ := io.ReadAll(out.Result().Body)
		if out.Code != 200 || len(data) == 0 {
			t.Fatalf("asset %s missing", asset[1])
		}
	}
}
