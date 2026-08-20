package server

import (
	"net/http"
	"testing"
)

func TestWebSocketAcceptMatchesRFCExample(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := websocketAccept(key); got != want {
		t.Fatalf("websocketAccept() = %q, want %q", got, want)
	}
}

func TestHeaderContainsToken(t *testing.T) {
	header := make(http.Header)
	header.Set("Connection", "keep-alive, Upgrade")
	if !headerContainsToken(header, "Connection", "upgrade") {
		t.Fatal("expected token to be detected case-insensitively")
	}
}
