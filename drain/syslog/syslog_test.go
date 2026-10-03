package syslog_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/syslog"
	"github.com/jeremygprawira/wlog/pipeline"
)

// event returns the one event the frame tests hold.
func event() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00.123456Z",
		"level":     "error",
		"kind":      "request",
		"outcome":   "error",
		"summary":   "GET /orders failed",
		"event_id":  "018f4b3c-7c00-7a00-8000-000000000000",
		"trace":     map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "request_id": "req-1"},
		"service":   map[string]any{"name": "checkout", "instance": "pod-1"},
	}
}

// parts is one parsed RFC 5424 frame.
type parts struct {
	pri       int
	version   string
	timestamp string
	hostname  string
	appName   string
	procid    string
	msgID     string
	sd        string
	msg       string
}

// parseFrame reads one RFC 5424 frame by its field layout, which is the ABNF shape:
// PRI VERSION SP TIMESTAMP SP HOSTNAME SP APP-NAME SP PROCID SP MSGID SP SD MSG.
func parseFrame(t *testing.T, frame string) parts {
	t.Helper()
	if !strings.HasPrefix(frame, "<") {
		t.Fatalf("frame has no PRI: %q", frame)
	}
	gt := strings.IndexByte(frame, '>')
	if gt < 0 {
		t.Fatalf("frame has no PRI terminator: %q", frame)
	}
	pri, err := strconv.Atoi(frame[1:gt])
	if err != nil {
		t.Fatalf("PRI is not a number: %q", frame)
	}
	if pri > 191 {
		t.Fatalf("PRI %d is above 191: %q", pri, frame)
	}
	rest := frame[gt+1:]
	fields := strings.SplitN(rest, " ", 7)
	if len(fields) != 7 {
		t.Fatalf("frame has %d fields, want 7: %q", len(fields), frame)
	}
	// VERSION is one to three digits with a non-zero first, and TIMESTAMP is RFC 3339 or
	// the nil value.
	if fields[0] == "" || fields[0][0] == '0' {
		t.Fatalf("VERSION %q starts with a zero: %q", fields[0], frame)
	}
	if fields[1] != "-" {
		if _, err := time.Parse(time.RFC3339Nano, fields[1]); err != nil {
			t.Fatalf("TIMESTAMP %q is not RFC 3339: %q", fields[1], frame)
		}
	}
	sdAndMsg := fields[6]
	var sd, msg string
	switch {
	case strings.HasPrefix(sdAndMsg, "-"):
		sd, msg = "-", strings.TrimPrefix(sdAndMsg[1:], " ")
	case strings.HasPrefix(sdAndMsg, "["):
		// The closing bracket is the first unescaped one, because \] inside a parameter
		// value is a character and not the end of the structured data.
		end := -1
		for i := 1; i < len(sdAndMsg); i++ {
			if sdAndMsg[i] == '\\' {
				i++
				continue
			}
			if sdAndMsg[i] == ']' {
				end = i
				break
			}
		}
		if end < 0 {
			t.Fatalf("structured data has no ]: %q", frame)
		}
		sd, msg = sdAndMsg[:end+1], strings.TrimPrefix(sdAndMsg[end+1:], " ")
	default:
		t.Fatalf("structured data is neither - nor [ : %q", frame)
	}
	return parts{
		pri: pri, version: fields[0], timestamp: fields[1],
		hostname: fields[2], appName: fields[3], procid: fields[4],
		msgID: fields[5], sd: sd, msg: msg,
	}
}

// TestSyslog_Frame proves the frame has the RFC 5424 field layout, the severity, the
// BOM, and the canonical event as JSON.
func TestSyslog_Frame(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(syslog.WithAddr(srv.addr), syslog.WithNetwork("udp"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	frame := srv.recv(t)

	got := parseFrame(t, frame)
	if got.pri != 11 {
		t.Errorf("PRI = %d, want 11 (User 1 x 8 + error 3)", got.pri)
	}
	if got.version != "1" {
		t.Errorf("version = %q, want 1", got.version)
	}
	if got.timestamp != "2026-09-22T10:00:00.123456Z" {
		t.Errorf("timestamp = %q", got.timestamp)
	}
	if got.hostname != "pod-1" {
		t.Errorf("hostname = %q, want pod-1", got.hostname)
	}
	if got.appName != "checkout" {
		t.Errorf("appName = %q, want checkout", got.appName)
	}
	if got.msgID != "request" {
		t.Errorf("msgID = %q, want request", got.msgID)
	}
	if got.sd != "-" {
		t.Errorf("structured data = %q, want -", got.sd)
	}
	if !strings.HasPrefix(got.msg, "\ufeff") {
		t.Error("the message has no byte order mark")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got.msg, "\ufeff")), &decoded); err != nil {
		t.Fatalf("the message is not JSON: %v", err)
	}
	if decoded["summary"] != "GET /orders failed" {
		t.Errorf("summary = %v", decoded["summary"])
	}
}

// TestSyslog_StructuredData proves the element holds the low-cardinality fields and
// escapes the reserved characters.
func TestSyslog_StructuredData(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithStructuredData("wlog@32473"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	got := parseFrame(t, srv.recv(t))
	for _, want := range []string{`[wlog@32473`, `level="error"`, `kind="request"`, `outcome="error"`, `trace_id="4bf92f3577b34da6a3ce929d0e0e4736"`, `request_id="req-1"`} {
		if !strings.Contains(got.sd, want) {
			t.Errorf("structured data %q is missing %q", got.sd, want)
		}
	}
}

// TestSyslog_BadSDID proves a structured data id outside name@digits is refused.
func TestSyslog_BadSDID(t *testing.T) {
	for _, id := range []string{"wlog", "wlog@", "@32473", "wlog@abc", "wlog@32 473"} {
		if _, err := syslog.NewSender(syslog.WithAddr("127.0.0.1:1"), syslog.WithStructuredData(id)); err == nil {
			t.Errorf("NewSender accepted the structured data id %q", id)
		}
	}
}

// TestSyslog_TCPOctetCounting proves a TCP listener receives the length, one space, then
// the frame.
func TestSyslog_TCPOctetCounting(t *testing.T) {
	addr, frames := listenTCP(t, nil)
	sender, err := syslog.NewSender(syslog.WithAddr(addr), syslog.WithNetwork("tcp"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	frame := <-frames
	parseFrame(t, frame)
}

// TestSyslog_TLS proves a TLS listener receives an octet-counted frame.
func TestSyslog_TLS(t *testing.T) {
	serverTLS := selfSignedTLS(t)
	addr, frames := listenTCP(t, serverTLS)
	clientTLS := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // a loopback test
	sender, err := syslog.NewSender(
		syslog.WithAddr(addr),
		syslog.WithNetwork("tls"),
		syslog.WithTLSConfig(clientTLS),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	frame := <-frames
	parseFrame(t, frame)
}

// TestSyslog_UDPTruncation proves a frame over the cap becomes the summary form.
func TestSyslog_UDPTruncation(t *testing.T) {
	srv := listenUDP(t)
	// The cap holds the header, the summary, and the event id, and not the JSON message.
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithMaxUDPBytes(200),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	frame := srv.recv(t)
	if !strings.Contains(frame, "GET /orders failed event_id=018f4b3c-7c00-7a00-8000-000000000000") {
		t.Errorf("truncated frame = %q, want the summary and the event id", frame)
	}
	if strings.Contains(frame, "event_id=018f4b3c-7c00-7a00-8000-000000000000}") {
		t.Error("the truncated frame still holds the full message")
	}
}

// TestSyslog_D13_ReportsATruncation proves a UDP frame over the cap reports WLOG_CAP_REACHED
// through the Logger, so a silent cut is visible.
func TestSyslog_D13_ReportsATruncation(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithMaxUDPBytes(64),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	problems := make(chan wlog.Problem, 4)
	log := wlog.New(wlog.WithSilent(), wlog.OnProblem(func(p wlog.Problem) { problems <- p }))
	if err := sender.Setup(log); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	_ = srv.recv(t)

	reports := 0
	for len(problems) > 0 {
		if p := <-problems; p.Code == "WLOG_CAP_REACHED" {
			reports++
		}
	}
	if reports != 1 {
		t.Errorf("WLOG_CAP_REACHED reports = %d, want one", reports)
	}
}

// TestSyslog_D4_WriteDeadlineFromContext proves a collector that never reads cannot block
// the worker: the context deadline bounds the write, so SendBatch returns and Close works.
func TestSyslog_D4_WriteDeadlineFromContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// The peer never reads, so the kernel buffer fills.
		accepted <- conn
	}()
	t.Cleanup(func() {
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	sender, err := syslog.NewSender(syslog.WithAddr(listener.Addr().String()), syslog.WithNetwork("tcp"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	// A large frame fills the send buffer, so the write blocks until the deadline.
	big := event()
	big["blob"] = strings.Repeat("x", 4<<20)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := sender.SendBatch(ctx, []map[string]any{big}); err == nil {
		t.Fatal("SendBatch returned nil, want a deadline error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("SendBatch took %v, want it bounded by the context", elapsed)
	}
	if err := sender.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestSyslog_D17_TheSummaryFormFitsTheCap proves the UDP summary form is cut to the cap at
// a rune boundary, so the datagram fits and stays valid UTF-8.
func TestSyslog_D17_TheSummaryFormFitsTheCap(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithMaxUDPBytes(64),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	big := event()
	big["summary"] = strings.Repeat("é", 200)
	if err := sender.SendBatch(context.Background(), []map[string]any{big}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	frame := srv.recv(t)
	if len(frame) > 64 {
		t.Errorf("frame is %d bytes, want at most 64", len(frame))
	}
	if !utf8.ValidString(frame) {
		t.Error("the frame is not valid UTF-8")
	}
}

// TestSyslog_D17_SkipsADatagramTooLargeForThePath proves a frame the path refuses with
// EMSGSIZE is skipped, so one large event does not fail the batch.
func TestSyslog_D17_SkipsADatagramTooLargeForThePath(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		// The cap is above the datagram limit, so no summary form replaces the frame.
		syslog.WithMaxUDPBytes(1<<20),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	big := event()
	big["blob"] = strings.Repeat("x", 100<<10)
	if err := sender.SendBatch(context.Background(), []map[string]any{big}); err != nil {
		t.Errorf("SendBatch = %v, want the datagram skipped", err)
	}
}

// TestSyslog_D12_RedialsAfterThePeerCloses proves a batch after the peer closed dials again
// instead of writing into a dead socket, where the frames would be lost.
func TestSyslog_D12_RedialsAfterThePeerCloses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	conns := make(chan net.Conn, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns <- conn
		}
	}()

	sender, err := syslog.NewSender(syslog.WithAddr(listener.Addr().String()), syslog.WithNetwork("tcp"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("the first batch failed: %v", err)
	}
	first := <-conns
	_ = first.Close()
	// The peer's close travels over the loopback, so the drain learns of it on the next
	// probe.
	time.Sleep(100 * time.Millisecond)

	// The peer is gone, so the next batch must dial again.
	if err := sender.SendBatch(context.Background(), []map[string]any{event()}); err != nil {
		t.Fatalf("the second batch failed: %v", err)
	}
	select {
	case second := <-conns:
		_ = second.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("the drain did not dial again after the peer closed")
	}
}

// TestSyslog_MissingConfig proves a missing address is refused.
func TestSyslog_MissingConfig(t *testing.T) {
	if _, err := syslog.NewSender(); err == nil {
		t.Error("NewSender accepted a missing address")
	}
	if _, err := syslog.NewSender(syslog.WithAddr("127.0.0.1:1"), syslog.WithNetwork("smtp")); err == nil {
		t.Error("NewSender accepted an unknown network")
	}
}

// TestSyslog_Env proves the environment supplies the address and the network.
func TestSyslog_Env(t *testing.T) {
	t.Setenv("WLOG_SYSLOG_ADDR", "127.0.0.1:1")
	t.Setenv("WLOG_SYSLOG_NETWORK", "tcp")
	t.Setenv("WLOG_SYSLOG_FACILITY", "16")
	t.Setenv("WLOG_SYSLOG_APP_NAME", "app")
	t.Setenv("WLOG_SYSLOG_SD_ID", "wlog@32473")
	if _, err := syslog.NewSender(); err != nil {
		t.Fatalf("NewSender: %v", err)
	}
}

// TestSyslog_MustNewPanics proves a missing address panics rather than returning nil.
func TestSyslog_MustNewPanics(t *testing.T) {
	t.Setenv("WLOG_SYSLOG_ADDR", "")
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic on a missing address")
		}
	}()
	syslog.MustNew()
}

// TestSyslog_D7_EscapesTheStructuredData proves a value holding the three SD characters, a
// space, and a character outside ASCII arrives escaped, so the frame parses.
func TestSyslog_D7_EscapesTheStructuredData(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithStructuredData("wlog@32473"),
	)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	tricky := "a\"b\\c]d e\u00e9"
	event := event()
	trace, _ := event["trace"].(map[string]any)
	trace["request_id"] = tricky
	if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	got := parseFrame(t, srv.recv(t))
	want := `request_id="a\"b\\c\]d eé"`
	if !strings.Contains(got.sd, want) {
		t.Errorf("structured data %q is missing %q", got.sd, want)
	}
	// The structured data still closes with one bracket, so the escapes did not end it.
	if !strings.HasSuffix(got.sd, "]") {
		t.Errorf("structured data %q does not end with ]", got.sd)
	}
}

// TestSyslog_Options proves every option reaches the sender, New wraps it, and Close
// releases the connection.
func TestSyslog_Options(t *testing.T) {
	srv := listenUDP(t)
	drain, err := syslog.New(
		syslog.WithAddr(srv.addr),
		syslog.WithNetwork("udp"),
		syslog.WithFacility(syslog.Local0),
		syslog.WithAppName("app"),
		syslog.WithBOM(false),
		syslog.WithMaxUDPBytes(4096),
		syslog.WithTLSConfig(&tls.Config{}),
		syslog.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), event())
	flusher, _ := drain.(interface{ Flush(context.Context) error })
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	frame := srv.recv(t)
	if strings.Contains(frame, "\ufeff") {
		t.Error("WithBOM(false) wrote a byte order mark")
	}
	if !strings.Contains(frame, " app ") {
		t.Errorf("frame = %q, want the configured app name", frame)
	}
	closer, _ := drain.(interface{ Close(context.Context) error })
	if err := closer.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestSyslog_Severities proves each level maps to its RFC 5424 severity.
func TestSyslog_Severities(t *testing.T) {
	for _, tc := range []struct {
		level string
		pri   int
	}{
		{"debug", 15},
		{"info", 14},
		{"warn", 12},
		{"error", 11},
	} {
		srv := listenUDP(t)
		sender, err := syslog.NewSender(syslog.WithAddr(srv.addr), syslog.WithNetwork("udp"))
		if err != nil {
			t.Fatalf("NewSender: %v", err)
		}
		event := event()
		event["level"] = tc.level
		if err := sender.SendBatch(context.Background(), []map[string]any{event}); err != nil {
			t.Fatalf("SendBatch: %v", err)
		}
		if got := parseFrame(t, srv.recv(t)).pri; got != tc.pri {
			t.Errorf("level %s: PRI = %d, want %d", tc.level, got, tc.pri)
		}
	}
}

// TestSyslog_NoService proves an event with no service group uses the fallback fields.
func TestSyslog_NoService(t *testing.T) {
	srv := listenUDP(t)
	sender, err := syslog.NewSender(syslog.WithAddr(srv.addr), syslog.WithNetwork("udp"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{{"level": "info", "kind": "job"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	got := parseFrame(t, srv.recv(t))
	if got.appName != "-" {
		t.Errorf("app name = %q, want -", got.appName)
	}
	if got.hostname == "" {
		t.Error("hostname is empty")
	}
}

// udpServer is one loopback UDP listener.
type udpServer struct {
	conn net.PacketConn
	addr string
}

// listenUDP starts a loopback UDP listener.
func listenUDP(t *testing.T) *udpServer {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &udpServer{conn: conn, addr: conn.LocalAddr().String()}
}

// recv reads one datagram.
func (s *udpServer) recv(t *testing.T) string {
	t.Helper()
	_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 65535)
	n, _, err := s.conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	return string(buf[:n])
}

// listenTCP starts a loopback listener that reads one octet-counted frame. A nil tls
// config gives a plain TCP listener.
func listenTCP(t *testing.T, cfg *tls.Config) (string, chan string) {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	listener := raw
	if cfg != nil {
		listener = tls.NewListener(raw, cfg)
	}
	frames := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		countText, err := reader.ReadString(' ')
		if err != nil {
			return
		}
		count, err := strconv.Atoi(strings.TrimSpace(countText))
		if err != nil {
			return
		}
		frame := make([]byte, count)
		if _, err := readFull(reader, frame); err != nil {
			return
		}
		frames <- string(frame)
	}()
	return raw.Addr().String(), frames
}

// readFull reads len(buf) bytes.
func readFull(reader *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := reader.Read(buf[total:])
		total += n
		if errors.Is(err, net.ErrClosed) || (err != nil && n == 0) {
			return total, err
		}
		if err != nil && n > 0 && total == len(buf) {
			return total, nil
		}
	}
	return total, nil
}

// selfSignedTLS builds a TLS configuration with one in-memory certificate.
func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}
