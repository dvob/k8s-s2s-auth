package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

func greetHandler(l *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, _ := r.Context().Value("subject").(string)
		if subject == "" {
			fmt.Fprintf(w, "no subject")
			return
		}
		l.Printf("got request from %s", subject)
		fmt.Fprintf(w, "hello %s", subject)
	})
}

func getToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	prefix := "Bearer "
	if len(authHeader) < len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return ""
	}
	return authHeader[len(prefix):]
}
