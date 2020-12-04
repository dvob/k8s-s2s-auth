package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

func getSecret(client *vault.Client, kubeToken, path, key string) (string, error) {
	data := map[string]interface{}{
		"role": "default-default",
		"jwt":  kubeToken,
	}

	loginResp, err := client.Logical().Write("auth/kubernetes/login", data)
	if err != nil {
		return "", err
	}

	if loginResp.Auth == nil {
		return "", fmt.Errorf("failed to get vault token")
	}

	token := loginResp.Auth.ClientToken
	client.SetToken(token)

	resp, err := client.Logical().Read(path)
	if err != nil {
		return "", err
	}

	if resp.Data == nil {
		return "", fmt.Errorf("no data found under '%s'", path)
	}

	m, ok := resp.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("secret not found at '%s'", path)
	}

	secret, ok := m[key].(string)
	if !ok {
		return "", fmt.Errorf("secret '%s' does no exist", key)
	}

	return secret, nil
}

func newAppCmd() *cobra.Command {
	var (
		tokenFile  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		secretPath = "kv/data/k8s/default"
		secretKey  = "password"
		mode       = "vault"
		envVar     = "SECRET"
		secretFile = "/var/run/secrets/app/password"
		addr       = ":8080"
	)
	config := vault.DefaultConfig()
	_ = config.ReadEnvironment()

	cmd := &cobra.Command{
		Use:   "app",
		Short: "An app which reads a secrets.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var secret string

			switch mode {
			case "env":
				secret = os.Getenv(envVar)
			case "file":
				raw, err := ioutil.ReadFile(secretFile)
				if err != nil {
					return err
				}
				secret = string(raw)
			case "vault":
				client, err := vault.NewClient(config)
				if err != nil {
					return err
				}

				token, err := ioutil.ReadFile(tokenFile)
				if err != nil {
					return err
				}

				secret, err = getSecret(client, string(token), secretPath, secretKey)
				if err != nil {
					return err
				}

			default:
				return fmt.Errorf("unknown mode '%s'", mode)
			}

			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, "the secret is: %s", secret)
			})

			log.Fatal(http.ListenAndServe(addr, nil))

			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", mode, "Mode to get the secret (env|file|vault).")
	cmd.Flags().StringVar(&addr, "addr", addr, "Address to listen on.")

	// env
	cmd.Flags().StringVar(&envVar, "env", envVar, "Environment variable to read the secret from.")

	// file
	cmd.Flags().StringVar(&secretFile, "file", secretFile, "File to read the secret from.")

	// vault
	cmd.Flags().StringVar(&tokenFile, "token-file", tokenFile, "Path to read the service account token from.")
	cmd.Flags().StringVar(&config.Address, "vault-address", config.Address, "Vault address")
	cmd.Flags().StringVar(&secretPath, "secret-path", secretPath, "Secret Path")
	cmd.Flags().StringVar(&secretKey, "secret-key", secretKey, "Secret Key")
	return cmd
}
