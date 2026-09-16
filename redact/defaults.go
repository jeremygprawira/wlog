package redact

// defaultKeys is the default key denylist. It is ported from go-echo-boilerplate's
// internal/pkg/logger/masking.go and deduplicated by tokenization: "apikey", "api_key",
// and "api-key" all tokenize to the same entry, so one is kept. See SPEC-redact.md for
// the full list and the rationale for each change from the boilerplate's list.
var defaultKeys = []string{
	"password", "passwd", "pwd", "secret", "token", "auth", "authorization", "bearer",
	"api_key", "session", "cookie", "credential", "private_key", "cert", "certificate",
	"credit_card", "card_number", "cvv", "cvc", "ssn", "social_security",
	"aws_secret_access_key", "aws_access_key_id", "aws_session_token",
	"connection_string", "db_password", "x_api_key", "pin", "otp",
	// Sessions (RED-3): the cookie names of the common frameworks, plus the two
	// request-forgery tokens.
	"sid", "session_id", "jsessionid", "phpsessid", "connect.sid", "csrf", "xsrf",
	// Keys (RED-3): the words a signing key hides behind.
	"passphrase", "access_key", "signing_key", "client_secret", "signature",
	"x_amz_signature",
	// Connection data (RED-3): a DSN carries a password in its userinfo.
	"dsn", "database_url",
}
