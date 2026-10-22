package web

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
)

// NoncePlaceholder in index.html is replaced with each response's nonce.
// The editor and terminal read it to tag the <style> elements they create.
const NoncePlaceholder = "__GLOWPOD_NONCE__"

// ContentSecurityPolicy allows only same-origin scripts, styles, fonts and
// connections (fetch and WebSocket). Fonts are self-hosted for this reason.
// A nonce additionally allows <style> elements carrying it; scripts never
// get one.
func ContentSecurityPolicy(nonce string) string {
	style := "style-src 'self'; "
	if nonce != "" {
		style = "style-src 'self' 'nonce-" + nonce + "'; "
	}
	return "default-src 'self'; " +
		"script-src 'self'; " +
		style +
		"img-src 'self' data:; " +
		"font-src 'self'; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'"
}

func newNonce() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err //nolint:wrapcheck // crypto/rand doesn't fail on supported platforms
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

func withNonce(page []byte, nonce string) []byte {
	return bytes.ReplaceAll(page, []byte(NoncePlaceholder), []byte(nonce))
}
