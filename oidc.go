package main

import (
	"context"
	"log"
	"net/http"

	"github.com/coreos/go-oidc"
)

type OIDCDiscoveryConfig struct {
	log      *log.Logger
	verifier *oidc.IDTokenVerifier
}

func OIDCDiscoveryAuth(cfg *OIDCDiscoveryConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := getToken(r)
		if rawToken == "" {
			cfg.log.Print("no token found in request")
			http.Error(w, http.StatusText(401)+": no token", 401)
			return
		}
		token, err := cfg.verifier.Verify(r.Context(), rawToken)
		if err != nil {
			cfg.log.Print("token verification failed:", err)
			http.Error(w, http.StatusText(401)+": "+err.Error(), 401)
			return
		}
		ctx := context.WithValue(r.Context(), "subject", token.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
