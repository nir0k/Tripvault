// Package docs serves the API reference: the OpenAPI document and the Scalar
// reader that renders it.
//
// Both are embedded in the binary. Nothing is fetched from a CDN, so the
// reference works on an air-gapped host exactly as it does on a connected one.
package docs

import (
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//go:embed openapi.yaml
var specFS embed.FS

// The Scalar reader is vendored as a gzip stream rather than as the 3.6 MB
// source: it is stored compressed, shipped compressed to any client that says it
// accepts gzip, and decompressed on the fly only for one that does not.
//
//go:embed assets/scalar.standalone.js.gz
var scalarGzip []byte

// specVersionPlaceholder is the version written in the checked-in document. It
// keeps the file a valid OpenAPI document on its own and is replaced at start-up
// with the running build's version, so the reference can never claim a version
// the service is not.
const specVersionPlaceholder = "0.0.0-dev"

// Paths the reference is served under.
const (
	referencePath = "/docs"
	specPath      = "/openapi.yaml"
	scriptPath    = "/docs/scalar.js"
)

// Handler serves the API reference.
type Handler struct {
	spec []byte
}

// NewHandler - loads the OpenAPI document and stamps it with the running version.
//
// Arguments:
//   - version: the product version to write into the document, so it matches the
//     binary and the web bundle.
//
// Returns:
//   - a handler ready to be mounted.
//   - an error if the embedded document cannot be read.
func NewHandler(version string) (*Handler, error) {
	spec, err := specFS.ReadFile("openapi.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded openapi document: %w", err)
	}

	stamped := strings.Replace(string(spec), specVersionPlaceholder, version, 1)
	return &Handler{spec: []byte(stamped)}, nil
}

// Register - mounts the reference on a router.
//
// Arguments:
//   - mount: the router's method for registering a GET route, so this package
//     does not depend on a particular router type.
func (h *Handler) Register(mount func(pattern string, handler http.HandlerFunc)) {
	mount(referencePath, h.handleReference)
	mount(specPath, h.handleSpec)
	mount(scriptPath, h.handleScript)
}

// handleSpec serves the OpenAPI document itself, for a client that would rather
// read or generate from it than look at the rendered page.
func (h *Handler) handleSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(h.spec)
}

// handleScript serves the vendored Scalar bundle, passing the stored gzip
// straight through when the client accepts it.
func (h *Handler) handleScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	// The bundle only changes when the version does, and the path is versionless,
	// so revalidation is cheap and staleness is avoided.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Add("Vary", "Accept-Encoding")

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(scalarGzip)
		return
	}

	reader, err := gzip.NewReader(strings.NewReader(string(scalarGzip)))
	if err != nil {
		http.Error(w, "documentation asset is unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = reader.Close() }()
	_, _ = io.Copy(w, reader)
}

// readerConfiguration switches off everything in the reader that would reach
// outside this instance.
//
//   - showDeveloperTools defaults to "localhost", which is why the Configure,
//     Share and Deploy panel appears during local work. Those publish the
//     document to the vendor's hosted registry, which a self-hosted service has
//     no business doing.
//   - proxyUrl defaults to the vendor's request proxy, used by the built-in
//     "Try it" client to get around CORS. Routing a request through it would
//     send the reader's bearer token to a third party. The reference is served
//     from the same origin as the API, so no proxy is needed at all.
//   - telemetry defaults to true. Nothing in the vendored bundle currently reads
//     it, but the default is not something to rely on.
//   - agent.disabled removes the AI assistant. The reader enables it by itself on
//     a local host and needs a key anywhere else, so leaving it to the default
//     would mean the reference behaves differently depending on where it is read.
//   - persistAuth keeps a token typed into "Try it" out of browser storage.
//
// The MCP integration needs no switch: the reader only offers it when an "mcp"
// key is configured, and there is none here.
const readerConfiguration = `{
  "showDeveloperTools": "never",
  "proxyUrl": "",
  "telemetry": false,
  "persistAuth": false,
  "agent": { "disabled": true }
}`

// referencePage is the whole reader: a Scalar element pointed at our document,
// plus the vendored script. Both URLs are relative, so the page works behind any
// host or path prefix without configuration.
const referencePage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tripvault API reference</title>
</head>
<body>
<div id="app"></div>
<script id="api-reference" data-url="` + specPath + `" data-configuration='` + readerConfiguration + `'></script>
<script src="` + scriptPath + `"></script>
</body>
</html>
`

// handleReference serves the reader page.
func (h *Handler) handleReference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page loads only same-origin scripts, so a restrictive policy costs
	// nothing and keeps an injected reference from reaching out.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:")
	_, _ = w.Write([]byte(referencePage))
}
