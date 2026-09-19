// Package httpcore is the framework-neutral HTTP core that every wlog HTTP adapter
// drives. One Core owns capture, the operation name, the level, the route rules, trace
// context, and the emit point. An adapter passes only what its framework knows, at the
// moment it knows it.
//
// # Capture defaults
//
// A request event carries the method, the route, the path, the status, the protocol, the
// scheme, the host, the sizes, the client address, the user agent, the allow-listed
// headers, and the sorted names of the query keys and the cookies. CaptureAll adds every
// header and the values. A local, dev, or development service environment turns it on.
// A body is captured only under CaptureBody or CaptureAll.
//
// # The proxy rule
//
// http.scheme is https for a TLS connection and http otherwise, and http.host is the Host
// header. A forwarded header counts only from a trusted proxy. Core walks
// X-Forwarded-For from the right, and takes the first address that is not itself a
// trusted proxy, so a client cannot prepend a claim of its own.
//
// # Deliberate response changes
//
// wlog changes a response in two cases only. It sets the X-Request-ID response header,
// which EchoRequestID(false) turns off. A panic before any write becomes a 500 response,
// which PanicPolicy(Repanic) turns off. Every other byte and status stays as the app
// wrote it.
//
// # The emit point
//
// The event emits after the handler returns, once the route, the status, and the size are
// known. A panic still emits its event before it continues or becomes a 500.
//
// # Untrusted input
//
// An X-Request-ID counts only at 128 characters or fewer from a small alphabet, and
// traceparent follows the W3C rules, so a future version is read and a refused one is not.
// A value that breaks either rule is replaced with a generated id. Every captured value
// then passes through the redactor like any other field.
package httpcore
