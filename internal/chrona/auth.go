package chrona

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

func authorize(secret string, next http.Handler) http.Handler {
	if secret == "" {
		return next
	}
	expected := sha256.Sum256([]byte(secret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		provided := sha256.Sum256([]byte(password))
		if !ok || user != "chrona" || subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Chrona", charset="UTF-8"`)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
