package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/coreos/go-oidc"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	err := newRootCmd().Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		ca string
	)
	cmd := &cobra.Command{
		Use:              "k8s-s2s-auth",
		TraverseChildren: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if ca != "" {
				rootCAs, err := x509.SystemCertPool()
				if err != nil {
					rootCAs = x509.NewCertPool()
				}

				certs, err := ioutil.ReadFile(ca)
				if err != nil {
					return fmt.Errorf("could not read ca '%s': %w", ca, err)
				}

				_ = rootCAs.AppendCertsFromPEM(certs)

				config := &tls.Config{
					RootCAs: rootCAs,
				}
				// usually we should avoid this mess with global stuff
				http.DefaultTransport.(*http.Transport).TLSClientConfig = config
			}

			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&ca, "ca", ca, "Add CA to trusted certificates")
	cmd.AddCommand(
		newServerCmd(),
		newClientCmd(),
	)
	return cmd
}

func newClientCmd() *cobra.Command {
	var (
		tokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	)
	cmd := &cobra.Command{
		Use:  "client <targetURL>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			targetURL := args[0]

			client, err := newClient(targetURL, tokenFile)
			if err != nil {
				return err
			}

			return client.run()
		},
	}
	cmd.Flags().StringVar(&tokenFile, "token-file", tokenFile, "Path to the token file")
	return cmd
}

func newServerCmd() *cobra.Command {
	var (
		handler             http.Handler
		kubeConfigOverrides = &clientcmd.ConfigOverrides{}
		mode                = "tokenreview"
		jwtPubKeyFile       = "sa.pub"
		oidcIssuerURL       = "https://kubernetes.default.svc"
		audience            string
		listenAddr          = ":8080"
	)
	cmd := &cobra.Command{
		Use: "server",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := log.New(os.Stderr, "", log.LstdFlags)
			appHandler := greetHandler(logger)

			switch mode {
			case "tokenreview":
				client, err := loadKubeClient(kubeConfigOverrides)
				if err != nil {
					return err
				}

				config := &TokenReviewConfig{
					log:           logger,
					tokenReviewer: client.AuthenticationV1().TokenReviews(),
				}

				if audience != "" {
					config.audiences = []string{audience}
				}

				handler = TokenReviewAuth(config, appHandler)

			case "jwt-pubkey":

				pubKey, err := loadPubKey(jwtPubKeyFile)
				if err != nil {
					return err
				}

				config := &JWTPubKeyConfig{
					pubKey:   pubKey,
					log:      logger,
					audience: audience,
				}

				handler = JWTPubKeyAuth(config, appHandler)

			case "oidc-discovery":
				provider, err := oidc.NewProvider(context.Background(), oidcIssuerURL)
				if err != nil {
					return err
				}

				oidcConfig := &oidc.Config{
					ClientID: audience,
				}

				if audience == "" {
					oidcConfig.SkipClientIDCheck = true
				}

				config := &OIDCDiscoveryConfig{
					log:      logger,
					verifier: provider.Verifier(oidcConfig),
				}

				handler = OIDCDiscoveryAuth(config, appHandler)

			default:
				return fmt.Errorf("unknown mode: %s", mode)

			}

			return http.ListenAndServe(listenAddr, handler)
		},
	}
	clientcmd.BindOverrideFlags(kubeConfigOverrides, cmd.Flags(), clientcmd.RecommendedConfigOverrideFlags("kube-"))
	cmd.Flags().StringVar(&mode, "mode", mode, "Authentication mode (tokenreview, jwt-pubkey, oidc-discovery")
	cmd.Flags().StringVar(&oidcIssuerURL, "issuer-url", oidcIssuerURL, "Issuer Discovery URL for oidc-discovery mode")
	cmd.Flags().StringVar(&audience, "audience", audience, "The audience to check")
	cmd.Flags().StringVar(&listenAddr, "addr", listenAddr, "Listen address of the server")
	cmd.Flags().StringVar(&jwtPubKeyFile, "pub-key", jwtPubKeyFile, "Public Key to verify JWT")
	return cmd
}
