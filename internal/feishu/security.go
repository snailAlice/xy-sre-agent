package feishu

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

func verifyToken(expected, actual string) bool {
	if expected == "" {
		return true
	}
	if actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func verifySignature(r *http.Request, body []byte, eventSecret string) bool {
	if eventSecret == "" {
		return true
	}
	signature := r.Header.Get("X-Lark-Signature")
	timestamp := r.Header.Get("X-Lark-Request-Timestamp")
	nonce := r.Header.Get("X-Lark-Request-Nonce")
	if signature == "" || timestamp == "" || nonce == "" {
		return false
	}
	sum := sha256.Sum256([]byte(timestamp + nonce + eventSecret + string(body)))
	expected := base64.StdEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1
}
