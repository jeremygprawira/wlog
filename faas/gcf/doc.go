// Package wloggcf is wlog's Google Cloud Functions adapter: one event per HTTP request, and
// one event per CloudEvent function call.
//
// A Cloud Run function freezes the process between invocations, so both wrappers flush the
// drains before they return.
//
// Read top to bottom: HTTP wraps an HTTP function with http-core and adds the execution id
// and the Cloud Run trace, and CloudEvent wraps a CloudEvent function with the field set of
// the CloudEvents receiver.
package wloggcf
