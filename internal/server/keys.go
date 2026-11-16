package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
)

// A named run's API key, as Stripe's keys name their account: sk_test_<run>_<signature> (pk_ and rk_ alike).
// The signature is the run's HMAC under the configured key secret, so whoever holds the secret (the
// emulator, and the platform that hands keys out) can make and check a key, and nothing is stored.
var runKeyPattern = regexp.MustCompile(`^(?:sk|pk|rk)_test_([a-z0-9][a-z0-9._-]*)_([0-9a-f]{24})$`)

// RunKey is the secret key of a run under secret: what a platform gives the account's owner.
func RunKey(secret, run string) string {
	return "sk_test_" + run + "_" + runKeySignature(secret, run)
}

func runKeySignature(secret, run string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(run))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

// requestKey is the API key a request carries: a bearer token, or Basic auth's user, as Stripe's SDKs send it.
func requestKey(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if encoded, ok := strings.CutPrefix(auth, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return ""
		}
		user, _, _ := strings.Cut(string(decoded), ":")
		return user
	}
	return ""
}

// runOfKey is the run a request's key selects. ok is false for a key of no run (sk_test_emulator, or any key
// while no secret is configured): the request keeps the default run. valid is false for a run key whose
// signature does not match: Stripe refuses such a key.
func runOfKey(secret string, r *http.Request) (run string, ok bool, valid bool) {
	if secret == "" {
		return "", false, true
	}
	match := runKeyPattern.FindStringSubmatch(requestKey(r))
	if match == nil {
		return "", false, true
	}
	run, err := normalizeRunID(match[1])
	if err != nil || run == DefaultRun {
		return "", false, true
	}
	return run, true, hmac.Equal([]byte(match[2]), []byte(runKeySignature(secret, run)))
}

// writeInvalidKey answers as Stripe does to a key it does not know.
func writeInvalidKey(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	shown := key
	if len(key) > 4 {
		shown = key[:strings.Index(key, "_test_")+6] + strings.Repeat("*", 12) + key[len(key)-4:]
	}
	w.Header().Set("Www-Authenticate", `Basic realm="Stripe"`)
	writeRunError(w, r, http.StatusUnauthorized, "Invalid API Key provided: "+shown)
}
