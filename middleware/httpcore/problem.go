// This file holds the problem document: the RFC 9457 JSON a service writes when it
// refuses a request, and the reader that turns one back into an ErrorInfo.
package httpcore

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// problemMediaType is the content type of an RFC 9457 document.
const problemMediaType = "application/problem+json"

// problemDocument is the RFC 9457 body with the wlog extensions. It never carries an
// internal field, a stack, a cause, a caller, or attrs, so a response never leaks what
// the event keeps for the operator.
type problemDocument struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Detail    string         `json:"detail,omitempty"`
	Instance  string         `json:"instance,omitempty"`
	Code      string         `json:"code,omitempty"`
	Fix       string         `json:"fix,omitempty"`
	Link      string         `json:"link,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// WriteProblem records err on the event of the request and writes an RFC 9457 document,
// so the response and the event agree. With no event on the context, the error becomes a
// plain log line, and the document carries the message alone.
func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	wlog.Error(ctx, err)
	info, ok := wlog.CurrentError(ctx)
	if !ok {
		info = wlog.ErrorInfo{Message: err.Error()}
	}

	document := problemFrom(info, r)
	body, marshalErr := json.Marshal(document)
	if marshalErr != nil {
		http.Error(w, http.StatusText(document.Status), document.Status)
		return
	}
	w.Header().Set("Content-Type", problemMediaType)
	w.WriteHeader(document.Status)
	_, _ = w.Write(body)
}

// problemFrom builds the document from one error detail and the request it refused.
func problemFrom(info wlog.ErrorInfo, r *http.Request) problemDocument {
	status := info.Status
	if status < 400 || status > 599 {
		// A detail without a usable status is the server's fault, which is the safe
		// reading of a missing one.
		status = http.StatusInternalServerError
	}
	document := problemDocument{
		Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: info.Message, Code: info.Code, Fix: info.Fix, Link: info.Link, Data: info.Data,
	}
	if info.Link != "" {
		document.Type = info.Link
	}
	if r != nil {
		document.Instance = r.URL.Path
		if trace, ok := propagate.FromContext(r.Context()); ok {
			document.RequestID = trace.RequestID
		}
	}
	return document
}

// ParseProblem reads an RFC 9457 document back into an ErrorInfo, for a client or a test.
func ParseProblem(body []byte) (wlog.ErrorInfo, bool) {
	var document problemDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return wlog.ErrorInfo{}, false
	}
	if document.Title == "" && document.Detail == "" && document.Code == "" {
		return wlog.ErrorInfo{}, false
	}
	info := wlog.ErrorInfo{
		Code: document.Code, Message: document.Detail, Status: document.Status,
		Fix: document.Fix, Link: document.Link, Data: document.Data,
	}
	if document.Type != "" && document.Type != "about:blank" {
		info.Link = document.Type
	}
	return info, true
}
