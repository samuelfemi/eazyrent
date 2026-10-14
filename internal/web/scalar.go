package web

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/femi/golang-easyrent/docs"
)

// openapiSpec is the raw spec; host/scheme are rewritten per request.
var openapiSpec = docs.Spec

// scalarPage is the Scalar API reference UI. It loads the spec from
// /openapi.json served by openapiSpecHandler.
const scalarPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>EasyRent API Reference</title>
</head>
<body>
<script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>
`

// scalarDocs serves the Scalar API reference page.
func (h Handler) scalarDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(scalarPage))
}

// openapiSpecHandler serves the OpenAPI spec with the host/scheme rewritten
// from the incoming request (X-Forwarded-Host/Proto when behind a host like
// Railway), so Scalar's "Try it out" targets the right base URL.
func (h Handler) openapiSpecHandler(w http.ResponseWriter, r *http.Request) {
	host, scheme := specHostScheme(r)
	out := rewriteSpecHost(openapiSpec, host, scheme)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// specHostScheme derives the public host/scheme for the spec from the
// request, preferring proxy headers. Empty host means "keep the spec default".
func specHostScheme(r *http.Request) (host, scheme string) {
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		host = v
	} else {
		host = r.Host
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		scheme = v
	} else if r.TLS != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}
	return host, scheme
}

// rewriteSpecHost returns a copy of the Swagger 2.0 spec with host/schemes
// set from the request. On any error it returns the raw spec unchanged.
func rewriteSpecHost(raw []byte, host, scheme string) []byte {
	if host == "" && (scheme != "https" && scheme != "http") {
		return raw
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		return raw
	}
	if host != "" {
		spec["host"] = host
	}
	if scheme == "https" || scheme == "http" {
		spec["schemes"] = []string{scheme}
	}
	out, err := json.Marshal(spec)
	if err != nil {
		return raw
	}
	// Keep parity with the checked-in swagger.json formatting style is not
	// required; compact JSON is fine for Scalar. Ensure valid JSON output.
	if !json.Valid(out) {
		return raw
	}
	return bytes.Clone(out)
}
