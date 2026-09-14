package site

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestHomeCompleteWithoutJavaScript(t *testing.T) {
	response := request(t, http.MethodGet, "/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, required := range []string{
		"<main id=\"main-content\">",
		"Skip to content",
		"Go runs the server",
		"templ renders the HTML",
		"Tailwind shapes the surface",
		"HTMX adds interaction",
		"There is deliberately no apply command yet",
		"href=\"/principles?topic=trust\"",
		"href=\"https://github.com/gotthboard\"",
		"View on GitHub",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("body missing %q", required)
		}
	}
	if count := strings.Count(body, "<main "); count != 1 {
		t.Errorf("main landmark count = %d, want 1", count)
	}
	if count := strings.Count(body, "href=\"https://github.com/gotthboard\""); count != 1 {
		t.Errorf("GitHub source link count = %d, want 1", count)
	}
	if strings.Contains(body, "href=\"https://github.com/gotthboard/gotth-stack\"") {
		t.Error("GitHub source link targets the repository instead of the organization")
	}
	if strings.Contains(body, "style=") || strings.Contains(body, "<script>") {
		t.Error("page contains inline style or script")
	}
	assertCommonHeaders(t, response)
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %q, want none", got)
	}
}

func TestHeadMatchesWithoutBody(t *testing.T) {
	for _, path := range []string{"/", "/principles?topic=recovery", "/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css", "/static/htmx-2.0.10.min.js", "/healthz"} {
		get := request(t, http.MethodGet, path, nil)
		head := request(t, http.MethodHead, path, nil)
		if head.Code != get.Code {
			t.Errorf("HEAD %s status = %d, GET = %d", path, head.Code, get.Code)
		}
		if head.Body.Len() != 0 {
			t.Errorf("HEAD %s body length = %d, want 0", path, head.Body.Len())
		}
		if head.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
			t.Errorf("HEAD %s Content-Length = %q, GET = %q", path, head.Header().Get("Content-Length"), get.Header().Get("Content-Length"))
		}
	}
}

func TestPrinciplesProgressiveEnhancement(t *testing.T) {
	tests := []struct {
		key       string
		wantTitle string
	}{
		{key: "control", wantTitle: "The operator stays in the loop."},
		{key: "trust", wantTitle: "Secrets do not become configuration confetti."},
		{key: "recovery", wantTitle: "Unknown is a state, not a success color."},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			full := request(t, http.MethodGet, "/principles?topic="+test.key, nil)
			fragment := request(t, http.MethodGet, "/principles?topic="+test.key, map[string]string{"HX-Request": "true"})
			if full.Code != http.StatusOK || fragment.Code != http.StatusOK {
				t.Fatalf("status full=%d fragment=%d, want 200", full.Code, fragment.Code)
			}
			if !strings.Contains(full.Body.String(), test.wantTitle) || !strings.Contains(fragment.Body.String(), test.wantTitle) {
				t.Fatalf("selected title %q missing from full or fragment", test.wantTitle)
			}
			if !strings.Contains(full.Body.String(), "<!doctype html>") {
				t.Error("ordinary request did not receive a complete page")
			}
			if strings.Contains(fragment.Body.String(), "<!doctype html>") || !strings.HasPrefix(fragment.Body.String(), "<article data-topic=\""+test.key+"\"") {
				t.Error("HTMX request did not receive only the principle detail fragment")
			}
		})
	}

	defaultResponse := request(t, http.MethodGet, "/principles", nil)
	if !strings.Contains(defaultResponse.Body.String(), "The operator stays in the loop.") {
		t.Error("missing topic did not select the documented default")
	}
	nonExactHTMX := request(t, http.MethodGet, "/principles?topic=control", map[string]string{"HX-Request": "TRUE"})
	if !strings.Contains(nonExactHTMX.Body.String(), "<!doctype html>") {
		t.Error("non-exact HX-Request value changed response shape")
	}
}

func TestPrinciplesRejectMalformedInputWithoutReflection(t *testing.T) {
	for _, path := range []string{
		"/principles?topic=",
		"/principles?topic=control&topic=trust",
		"/principles?topic=control&extra=1",
		"/principles?extra=1",
		"/principles?topic=%zz",
		"/principles?topic=attacker-reflection-marker",
		"/principles?topic=" + strings.Repeat("x", maxPrinciplesQuery+1),
	} {
		response := request(t, http.MethodGet, path, nil)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.Code)
		}
		if strings.Contains(response.Body.String(), "attacker-reflection-marker") {
			t.Error("rejection reflected attacker input")
		}
		assertCommonHeaders(t, response)
	}
}

func TestMethodsAndUnknownRoutesFailClosed(t *testing.T) {
	for _, path := range []string{"/", "/principles", "/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css", "/healthz"} {
		response := request(t, http.MethodPost, path, nil)
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status = %d, want 405", path, response.Code)
		}
		allow := response.Header().Get("Allow")
		if !strings.Contains(allow, http.MethodGet) || !strings.Contains(allow, http.MethodHead) {
			t.Errorf("POST %s Allow = %q, want GET and HEAD", path, allow)
		}
		assertCommonHeaders(t, response)
	}
	unknown := request(t, http.MethodGet, "/not-a-route", nil)
	if unknown.Code != http.StatusNotFound {
		t.Errorf("unknown route status = %d, want 404", unknown.Code)
	}
	homeQuery := request(t, http.MethodGet, "/?unexpected=1", nil)
	if homeQuery.Code != http.StatusNotFound {
		t.Errorf("home query status = %d, want 404", homeQuery.Code)
	}
	for _, target := range []string{"http://example.test//", "http://example.test/a/../", "http://example.test/principles%2f..%2f"} {
		response := request(t, http.MethodGet, target, nil)
		if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" {
			t.Errorf("non-canonical %q status = %d Location = %q, want 404 without redirect", target, response.Code, response.Header().Get("Location"))
		}
		assertCommonHeaders(t, response)
	}
}

func TestEmbeddedAssetsAndHealth(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		contains    string
		cache       string
	}{
		{path: "/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css", contentType: "text/css; charset=utf-8", contains: "--color-signal", cache: "public, max-age=31536000, immutable"},
		{path: "/static/htmx-2.0.10.min.js", contentType: "text/javascript; charset=utf-8", contains: "htmx", cache: "public, max-age=31536000, immutable"},
		{path: "/healthz", contentType: "text/plain; charset=utf-8", contains: "ok\n", cache: "no-store"},
	}
	for _, test := range tests {
		response := request(t, http.MethodGet, test.path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", test.path, response.Code)
		}
		if got := response.Header().Get("Content-Type"); got != test.contentType {
			t.Errorf("GET %s Content-Type = %q, want %q", test.path, got, test.contentType)
		}
		if got := response.Header().Get("Cache-Control"); got != test.cache {
			t.Errorf("GET %s Cache-Control = %q, want %q", test.path, got, test.cache)
		}
		if !strings.Contains(response.Body.String(), test.contains) {
			t.Errorf("GET %s body missing %q", test.path, test.contains)
		}
		assertCommonHeaders(t, response)
	}
}

func TestRenderFailureIsFixedAndNonDisclosing(t *testing.T) {
	component := templ.ComponentFunc(func(context.Context, io.Writer) error {
		return errors.New("secret-render-error")
	})
	response := httptest.NewRecorder()
	writeComponent(response, httptest.NewRequest(http.MethodGet, "/", nil), component)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if got := response.Body.String(); got != "internal server error\n" || strings.Contains(got, "secret-render-error") {
		t.Errorf("body = %q, want fixed non-disclosing error", got)
	}
}

func BenchmarkRoutes(b *testing.B) {
	handler := NewHandler()
	tests := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{name: "health", path: "/healthz"},
		{name: "principle-fragment", path: "/principles?topic=recovery", headers: map[string]string{"HX-Request": "true"}},
		{name: "home", path: "/"},
		{name: "css", path: "/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css"},
		{name: "htmx", path: "/static/htmx-2.0.10.min.js"},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			b.ReportAllocs()
			for range b.N {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					b.Fatalf("status = %d, want 200", response.Code)
				}
			}
		})
	}
}

func request(t *testing.T, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)
	return response
}

func assertCommonHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	expected := map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"Permissions-Policy":      "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	}
	for name, want := range expected {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
