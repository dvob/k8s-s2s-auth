package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	authv1int "k8s.io/client-go/kubernetes/typed/authentication/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func loadKubeClient(overrides *clientcmd.ConfigOverrides) (*kubernetes.Clientset, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil && !errors.Is(rest.ErrNotInCluster, err) {
		return nil, err
	}

	// fall back to out of cluster config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

type TokenReviewConfig struct {
	log           *log.Logger
	tokenReviewer authv1int.TokenReviewInterface
	audiences     []string
}

func TokenReviewAuth(cfg *TokenReviewConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := getToken(r)
		if rawToken == "" {
			log.Print("no token")
			http.Error(w, http.StatusText(401)+": could not read token", 401)
			return
		}
		tokenReview := &authv1.TokenReview{
			Spec: authv1.TokenReviewSpec{
				Token:     rawToken,
				Audiences: cfg.audiences,
			},
		}
		tokenReview, err := cfg.tokenReviewer.Create(r.Context(), tokenReview, metav1.CreateOptions{})
		if err != nil {
			log.Print("token review failed:", err)
			http.Error(w, http.StatusText(500)+": "+err.Error(), 500)
			return
		}
		if !tokenReview.Status.Authenticated {
			log.Print("token is not authenticated:", tokenReview.Status.Error)
			http.Error(w, http.StatusText(401)+": "+tokenReview.Status.Error, 401)
			return
		}

		ctx := context.WithValue(r.Context(), "subject", tokenReview.Status.User.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})

}
