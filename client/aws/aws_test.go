// This file drives the AWS middleware over httptest and a real S3 client, so the tests
// run the middleware stack the SDK builds. It checks one call with three attempts, the
// API error code, the double install guard, and a call outside a unit of work.
package wlogaws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/jeremygprawira/wlog"
	wlogaws "github.com/jeremygprawira/wlog/client/aws"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAws_C5_OneCallWithAttempts proves that a PutObject retried twice records one call
// with attempts 3, the service operation, the region, and the status.
func TestAws_C5_OneCallWithAttempts(t *testing.T) {
	log, rec := wlogtest.New(t)
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL, 3)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("bucket"), Key: aws.String("key"), Body: strings.NewReader("body"),
	})
	end()
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("the server saw %d attempts, want 3", got)
	}

	record := onlyCall(t, rec)
	for key, want := range map[string]any{
		"kind": "rpc", "system": "aws", "operation": "S3.PutObject", "status": "200",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	attrs, _ := record["attrs"].(map[string]any)
	if !conformance.Equal(attrs["attempts"], 3) {
		t.Errorf("calls[0].attrs.attempts = %v, want 3", attrs["attempts"])
	}
	if attrs["region"] != "us-east-1" {
		t.Errorf("calls[0].attrs.region = %v, want us-east-1", attrs["region"])
	}
}

// TestAws_C5_ErrorCode proves that a failed operation records the API error code and the
// status, and never the error message.
func TestAws_C5_ErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL, 3)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("bucket"), Key: aws.String("key"), Body: strings.NewReader("body"),
	})
	end()
	if err == nil {
		t.Fatal("PutObject returned no error")
	}

	record := onlyCall(t, rec)
	if record["status"] != "403" {
		t.Errorf("calls[0].status = %v, want 403", record["status"])
	}
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "AccessDenied" {
		t.Errorf("calls[0].error.code = %v, want AccessDenied", callError["code"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "Access Denied") {
		t.Errorf("the error message reached the event: %s", body)
	}
}

// TestAws_B2_AppendOnce proves that two Append calls install one middleware, so one
// operation records one call.
func TestAws_B2_AppendOnce(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("key", "secret", ""),
	}
	wlogaws.Append(&cfg.APIOptions)
	wlogaws.Append(&cfg.APIOptions)
	client := s3.NewFromConfig(cfg, withEndpoint(server.URL))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("bucket"), Key: aws.String("key"), Body: strings.NewReader("body"),
	}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	end()

	onlyCall(t, rec)
}

// TestAws_B2_NoEvent proves that an operation outside a unit of work records nothing and
// reports nothing.
func TestAws_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("key", "secret", ""),
	}
	wlogaws.Append(&cfg.APIOptions)
	client := s3.NewFromConfig(cfg, withEndpoint(server.URL))

	ctx := rec.Logger().WithContext(context.Background())
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("bucket"), Key: aws.String("key"), Body: strings.NewReader("body"),
	}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// newClient returns an S3 client over one test server with the wlog middleware, and with
// a bounded number of attempts.
func newClient(t *testing.T, url string, maxAttempts int) *s3.Client {
	t.Helper()
	cfg := aws.Config{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("key", "secret", ""),
		RetryMaxAttempts: maxAttempts,
	}
	wlogaws.Append(&cfg.APIOptions)
	return s3.NewFromConfig(cfg, withEndpoint(url))
}

// withEndpoint points one S3 client at a test server.
func withEndpoint(url string) func(*s3.Options) {
	return func(o *s3.Options) {
		o.UsePathStyle = true
		// The module floor is s3 v1.29.0, which has no BaseEndpoint field, so the test uses
		// the deprecated resolver until the floor moves.
		//nolint:staticcheck // the floor predates BaseEndpoint
		o.EndpointResolver = s3.EndpointResolverFunc(func(region string, _ s3.EndpointResolverOptions) (aws.Endpoint, error) {
			//nolint:staticcheck // the floor predates BaseEndpoint
			return aws.Endpoint{URL: url, HostnameImmutable: true, SigningRegion: region}, nil
		})
	}
}

// onlyCall returns the only call record of the last event.
func onlyCall(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}
