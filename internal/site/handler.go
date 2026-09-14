package site

import (
	"bytes"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/a-h/templ"
)

const (
	contentSecurityPolicy = "default-src 'none'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'"
	maxPrinciplesQuery    = 64
)

// NewHandler constructs the fixed public-site route and security boundary.
// Complexity: time O(1), Omega(1), tight Theta(1); auxiliary space O(1),
// Omega(1), tight Theta(1); variables: the route count is fixed at five;
// delegated costs: net/http allocates the fixed ServeMux route table.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serveHome)
	mux.HandleFunc("HEAD /{$}", serveHome)
	mux.HandleFunc("GET /principles", servePrinciples)
	mux.HandleFunc("HEAD /principles", servePrinciples)
	mux.HandleFunc("GET /static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css", serveSiteCSS)
	mux.HandleFunc("HEAD /static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css", serveSiteCSS)
	mux.HandleFunc("GET /static/htmx-2.0.10.min.js", serveHTMX)
	mux.HandleFunc("HEAD /static/htmx-2.0.10.min.js", serveHTMX)
	mux.HandleFunc("GET /healthz", serveHealth)
	mux.HandleFunc("HEAD /healthz", serveHealth)
	return securityHeaders(rejectNonCanonicalPaths(mux))
}

// rejectNonCanonicalPaths prevents ServeMux from redirecting ambiguous paths.
// Complexity: time O(n), Omega(n), tight Theta(n); auxiliary space O(n),
// Omega(1), with no single tight bound because path.Clean may allocate;
// variables: n is request path bytes; delegated costs: path.Clean normalizes n.
func rejectNonCanonicalPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.RawPath != "" || request.URL.Path == "" || path.Clean(request.URL.Path) != request.URL.Path {
			http.NotFound(response, request)
			return
		}
		next.ServeHTTP(response, request)
	})
}

// securityHeaders adds one fixed policy to every response, including errors.
// Complexity: construction and per-request time O(1), Omega(1), tight
// Theta(1); auxiliary space O(1), Omega(1), tight Theta(1); variables: the
// header set is fixed; delegated costs: the wrapped handler owns route work.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

// serveHome validates the empty query contract and renders the default page.
// Complexity: time O(n), Omega(n), tight Theta(n); auxiliary space O(n),
// Omega(n), tight Theta(n); variables: n is the fixed rendered page byte size;
// delegated costs: templ rendering copies n bytes into a response buffer.
func serveHome(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		http.NotFound(response, request)
		return
	}
	writeComponent(response, request, page(pageView{Selected: principles[0]}))
}

// servePrinciples strictly parses one bounded topic and selects its response.
// Complexity: time O(q+p+n), Omega(q+n), tight Theta(q+p+n) in the last-item
// case; auxiliary space O(q+n), Omega(n), tight Theta(q+n); variables: q is
// raw query bytes (at most 64), p is the fixed principle count, n is rendered
// bytes; delegated costs: url.ParseQuery parses q and templ renders n.
func servePrinciples(response http.ResponseWriter, request *http.Request) {
	if len(request.URL.RawQuery) > maxPrinciplesQuery {
		http.NotFound(response, request)
		return
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query) > 1 {
		http.NotFound(response, request)
		return
	}
	selected := principles[0]
	if len(query) == 1 {
		values, present := query["topic"]
		if !present {
			http.NotFound(response, request)
			return
		}
		if len(values) != 1 || values[0] == "" {
			http.NotFound(response, request)
			return
		}
		selected, present = principleByKey(values[0])
		if !present {
			http.NotFound(response, request)
			return
		}
	}
	if request.Header.Get("HX-Request") == "true" {
		writeComponent(response, request, principleDetail(selected))
		return
	}
	writeComponent(response, request, page(pageView{Selected: selected}))
}

// writeComponent renders fully before committing headers so errors stay fixed.
// Complexity: time O(n), Omega(n), tight Theta(n); auxiliary space O(n),
// Omega(n), tight Theta(n); variables: n is the rendered byte count; delegated
// costs: templ rendering and ResponseWriter.Write each process n bytes.
func writeComponent(response http.ResponseWriter, request *http.Request, component templ.Component) {
	var rendered bytes.Buffer
	if err := component.Render(request.Context(), &rendered); err != nil {
		http.Error(response, "internal server error", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(rendered.Len()))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(rendered.Bytes())
	}
}

// serveSiteCSS returns the generated embedded stylesheet.
// Complexity: time O(n), Omega(1), tight Theta(n) for GET and Theta(1) for
// HEAD; auxiliary space O(1), Omega(1), tight Theta(1); variables: n is asset
// bytes; delegated costs: ResponseWriter.Write copies n bytes.
func serveSiteCSS(response http.ResponseWriter, request *http.Request) {
	writeAsset(response, request, "text/css; charset=utf-8", siteCSS)
}

// serveHTMX returns the pinned embedded HTMX distribution.
// Complexity: time O(n), Omega(1), tight Theta(n) for GET and Theta(1) for
// HEAD; auxiliary space O(1), Omega(1), tight Theta(1); variables: n is asset
// bytes; delegated costs: ResponseWriter.Write copies n bytes.
func serveHTMX(response http.ResponseWriter, request *http.Request) {
	writeAsset(response, request, "text/javascript; charset=utf-8", htmxJS)
}

// writeAsset emits one already-validated immutable embedded asset.
// Complexity: time O(n), Omega(1), tight Theta(n) for GET and Theta(1) for
// HEAD; auxiliary space O(1), Omega(1), tight Theta(1); variables: n is asset
// bytes; delegated costs: ResponseWriter.Write copies n bytes.
func writeAsset(response http.ResponseWriter, request *http.Request, contentType string, content []byte) {
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(content)
	}
}

// serveHealth returns a fixed process-readiness response.
// Complexity: time O(1), Omega(1), tight Theta(1); auxiliary space O(1),
// Omega(1), tight Theta(1); variables: the response is fixed at three bytes;
// delegated costs: ResponseWriter.Write copies those bytes for GET.
func serveHealth(response http.ResponseWriter, request *http.Request) {
	const body = "ok\n"
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(body)))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write([]byte(body))
	}
}
