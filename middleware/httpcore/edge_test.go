// This file tests the problem document and the edge cases of the HTTP core black box.
package httpcore_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// problemError is the failure the test extractor explains.
type problemError struct{}

// Error returns the message of the failure.
func (problemError) Error() string { return "card declined" }

// problemExtractor returns a full error detail, including the fields a response must
// never carry.
type problemExtractor struct{}

// Extract returns the detail of a problemError, and a plain detail otherwise.
func (problemExtractor) Extract(err error) wlog.ErrorInfo {
	var problem problemError
	if !errors.As(err, &problem) {
		return wlog.ErrorInfo{Message: err.Error()}
	}
	return wlog.ErrorInfo{
		Code: "PAYMENT_DECLINED", Message: "card declined", Status: http.StatusBadGateway,
		Fix:  "Ask the customer for another card.",
		Link: "https://example.com/errors/PAYMENT_DECLINED",
		Data: map[string]any{"order_id": "4821"},

		Internal: map[string]any{"bank_token": "secret"},
		Stack:    "internal stack",
		Cause:    "the bank said no",
		Caller:   "handler.go:42",
		Attrs:    map[string]any{"attempt": 1},
	}
}

// TestHTTPCore_BET6_ProblemJSON proves that WriteProblem writes an RFC 9457 document
// without the internal fields, that the event and the response agree, and that
// ParseProblem reads the document back.
func TestHTTPCore_BET6_ProblemJSON(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithErrorExtractor(problemExtractor{}))
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpcore.WriteProblem(w, r, problemError{})
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/42", nil))

	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	if response.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", response.Code)
	}

	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("the body is not JSON: %v\n%s", err, response.Body.String())
	}
	for key, want := range map[string]any{
		"type":     "https://example.com/errors/PAYMENT_DECLINED",
		"title":    "Bad Gateway",
		"status":   float64(502),
		"detail":   "card declined",
		"instance": "/orders/42",
		"code":     "PAYMENT_DECLINED",
		"fix":      "Ask the customer for another card.",
	} {
		if document[key] != want {
			t.Errorf("%s = %v, want %v", key, document[key], want)
		}
	}
	if data, _ := document["data"].(map[string]any); data["order_id"] != "4821" {
		t.Errorf("data = %v, want the order id", document["data"])
	}
	for _, key := range []string{"internal", "stack", "cause", "causes", "caller", "attrs"} {
		if _, present := document[key]; present {
			t.Errorf("the document carries %q: %v", key, document)
		}
	}

	info, _ := rec.Last()["error"].(map[string]any)
	if info["code"] != "PAYMENT_DECLINED" {
		t.Errorf("the event error = %v, want the code the response names", info)
	}

	parsed, ok := httpcore.ParseProblem(response.Body.Bytes())
	if !ok {
		t.Fatalf("ParseProblem refused the document: %s", response.Body.String())
	}
	if parsed.Code != "PAYMENT_DECLINED" || parsed.Message != "card declined" ||
		parsed.Status != http.StatusBadGateway {
		t.Errorf("ParseProblem = %+v, want the code, the message, and the status", parsed)
	}
}

// TestHTTPCore_HTTP19_EdgeCases proves the edge cases: a HEAD request, a 204, a 304, an
// encoded body, a problem document, a sniffed type, and the traceparent rules.
func TestHTTPCore_HTTP19_EdgeCases(t *testing.T) {
	t.Run("head", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(bodyWriter(`{"a":1}`))
		req := httptest.NewRequest(http.MethodHead, "/x", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if _, kept := httpFields(t, rec.Last())["response_body"]; kept {
			t.Error("a HEAD response captured a body")
		}
		if _, kept := httpFields(t, rec.Last())["request_body"]; kept {
			t.Error("a HEAD request captured a body")
		}
	})

	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			log, rec := wlogtest.New(t)
			handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"a":1}`))
				}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			if _, kept := httpFields(t, rec.Last())["response_body"]; kept {
				t.Errorf("a %d response captured a body", status)
			}
		})
	}

	t.Run("encoded request", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(bodyWriter(""))
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if _, kept := httpFields(t, rec.Last())["request_body"]; kept {
			t.Error("an encoded request captured a body")
		}
	})

	t.Run("problem document", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				_, _ = w.Write([]byte(`{"code":"E1"}`))
			}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
		body, _ := httpFields(t, rec.Last())["response_body"].(map[string]any)
		if body["code"] != "E1" {
			t.Errorf("a problem document was not parsed: %v", httpFields(t, rec.Last())["response_body"])
		}
	})

	t.Run("sniffed type", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		handler := httpcore.NetHTTP(log, httpcore.CaptureAll())(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"a":1}`))
			}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
		if _, kept := httpFields(t, rec.Last())["response_body"]; kept {
			t.Error("a body with no content type was captured")
		}
	})

	t.Run("traceparent", func(t *testing.T) {
		const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		const spanID = "00f067aa0ba902b7"

		for _, tc := range []struct {
			name   string
			header string
			want   string
		}{
			{"future version", "01-" + traceID + "-" + spanID + "-01-extra", traceID},
			{"refused version", "ff-" + traceID + "-" + spanID + "-01", ""},
			{"missing", "", ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				log, rec := wlogtest.New(t)
				handler := httpcore.NetHTTP(log)(okHandler())
				req := httptest.NewRequest(http.MethodGet, "/x", nil)
				if tc.header != "" {
					req.Header.Set("traceparent", tc.header)
				}
				handler.ServeHTTP(httptest.NewRecorder(), req)
				trace, _ := rec.Last()["trace"].(map[string]any)
				got, _ := trace["trace_id"].(string)
				if tc.want != "" && got != tc.want {
					t.Errorf("trace_id = %q, want %q", got, tc.want)
				}
				if tc.want == "" && !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got) {
					t.Errorf("trace_id = %q, want a generated id", got)
				}
			})
		}
	})
}

// bodyWriter answers every request with the given body and a JSON content type.
func bodyWriter(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}
