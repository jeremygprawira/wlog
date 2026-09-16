# Capture

The middleware records one event for each request. It reads the method, the
route, the status, and the duration. The body is captured when the capture
policy allows it, which keeps a large upload out of the event.

## Configuration

You must set the drain before the first request arrives. If the drain is not
ready, the middleware holds the event in a bounded queue, and it drops the
oldest event when the queue is full.

The redactor runs before the event reaches a drain — this is the rule that
protects a secret. Make sure that the denylist covers the keys of your service.
A key that is not on the list travels to every sink, e.g. a token in a header.

Call `WithRedactor` if you need a custom rule; the default rule handles the
usual cases, ensuring that no secret leaks.
