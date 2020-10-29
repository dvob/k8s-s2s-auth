package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"gopkg.in/square/go-jose.v2/jwt"
)

type JWTPubKeyConfig struct {
	log      *log.Logger
	pubKey   interface{}
	audience string
}

func JWTPubKeyAuth(cfg *JWTPubKeyConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := getToken(r)
		if rawToken == "" {
			cfg.log.Print("no token found in request")
			http.Error(w, http.StatusText(401)+": no token", 401)
			return
		}

		token, err := jwt.ParseSigned(rawToken)
		if err != nil {
			cfg.log.Print("failed to parse token:", err)
			http.Error(w, http.StatusText(401)+": "+err.Error(), 401)
			return
		}

		claims := jwt.Claims{}
		if err := token.Claims(cfg.pubKey, &claims); err != nil {
			cfg.log.Print("failed to verify token:", err)
			http.Error(w, http.StatusText(401)+": "+err.Error(), 401)
			return
		}

		expected := jwt.Expected{
			Time: time.Now(),
		}
		if cfg.audience != "" {
			expected.Audience = []string{cfg.audience}
		}

		err = claims.Validate(expected)
		if err != nil {
			cfg.log.Print("could not validate token:", err)
			http.Error(w, http.StatusText(401)+": "+err.Error(), 401)
			return
		}

		ctx := context.WithValue(r.Context(), "subject", claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loadPubKey(file string) (interface{}, error) {
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	return x509.ParsePKIXPublicKey(block.Bytes)
}
