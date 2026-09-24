package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// The OpenAPI document is written by hand rather than generated from
// annotations, which buys a modern specification and no code generator at the
// cost of one failure mode: an endpoint added without its documentation. This
// test removes that cost by comparing the two mechanically, so the drift is
// caught by the build rather than by a reader months later.

// routesOutsideSpec are the paths the reference deliberately does not describe.
var routesOutsideSpec = map[string]bool{
	// The reference describing itself would be noise.
	"GET /docs":           true,
	"GET /docs/scalar.js": true,
	"GET /openapi.yaml":   true,
}

// openAPIDocument is the slice of the specification this test reads.
type openAPIDocument struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// specOperations reads the document and returns its operations as "METHOD /path".
func specOperations(t *testing.T) map[string]bool {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the openapi document: %v", err)
	}

	var doc openAPIDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the openapi document: %v", err)
	}

	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	operations := make(map[string]bool)
	for path, item := range doc.Paths {
		for key := range item {
			if methods[key] {
				operations[strings.ToUpper(key)+" "+path] = true
			}
		}
	}
	if len(operations) == 0 {
		t.Fatal("the openapi document declares no operations")
	}
	return operations
}

// routerOperations walks the live route table and returns the same shape.
func routerOperations(t *testing.T) map[string]bool {
	t.Helper()

	// The stores are nil: routing is registered in one place regardless of the
	// dependencies, and no handler runs during a walk.
	server := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{})

	router, ok := server.routes().(chi.Routes)
	if !ok {
		t.Fatal("the router does not expose its routes")
	}

	operations := make(map[string]bool)
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi reports a trailing slash on mounted subtrees; the document does not.
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			route = "/"
		}
		// chi and OpenAPI spell path parameters identically, as {name}, so the
		// route needs no rewriting to be compared with the document.
		operations[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk the route table: %v", err)
	}
	return operations
}

// TestOpenAPICoversEveryRoute fails when the route table and the reference have
// drifted apart in either direction.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	spec := specOperations(t)
	routes := routerOperations(t)

	var undocumented, phantom []string

	for operation := range routes {
		if routesOutsideSpec[operation] {
			continue
		}
		// chi registers these implicitly; they are transport behaviour, not API.
		if strings.HasPrefix(operation, "OPTIONS ") {
			continue
		}
		if !spec[operation] {
			undocumented = append(undocumented, operation)
		}
	}
	for operation := range spec {
		if !routes[operation] {
			phantom = append(phantom, operation)
		}
	}

	sort.Strings(undocumented)
	sort.Strings(phantom)

	if len(undocumented) > 0 {
		t.Errorf("these routes exist but are missing from internal/httpapi/docs/openapi.yaml:\n  %s",
			strings.Join(undocumented, "\n  "))
	}
	if len(phantom) > 0 {
		t.Errorf("internal/httpapi/docs/openapi.yaml describes these operations, but no route serves them:\n  %s",
			strings.Join(phantom, "\n  "))
	}
}
