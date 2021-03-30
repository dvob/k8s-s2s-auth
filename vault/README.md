# Vault
The following tutorial describes how to use a service account to access a secret from a Hashicorp Vault.

# Setup
## Prepare Certificate
* Create CA
```
mkdir ~/.pcert
cd ~/.pcert
pcert create ca --ca --subject "/CN=My CA"
```

* Add to trusted certificates
```
mkdir /usr/local/share/ca-certificates/local
cp ~/.pcert/ca.crt /usr/local/share/ca-certificates/local/
update-ca-certificates
```

* (optional) Add to minikube. If you do this the control plane (api-server, ...) also trusts the certificate which can be useful for certain experiments.
```
mkdir -p $HOME/.minikube/certs
cp ~/.pcert/ca.crt ~/.minikube/certs/local.crt
```

## Start Minikube and Ingress Controller
* Start minikube
```
minikube start
```

* Install NGINX (we don't use the minikube addon that we're able to configure nginx according to our needs)
```
kubectl create ns ingress
pcert create ingress --server --with ~/.pcert/ca --dns *.k8s.example.com
kubectl create secret --namespace=ingress tls default-certificate --key=ingress.key --cert=ingress.crt

helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm install --namespace ingress nginx ingress-nginx/ingress-nginx \
    --set controller.admissionWebhooks.enabled=false \
    --set controller.hostPort.enabled=true \
    --set controller.kind=DaemonSet \
    --set controller.extraArgs.default-ssl-certificate="ingress/default-certificate" \
    --set-string controller.config.force-ssl-redirect=true
```

* Configure DNS wildcard record for `*.k8s.example.com` to `$( minikube ip )`

## Install Vault
* https://www.vaultproject.io/docs/platform/k8s/helm
```
kubectl create ns vault

helm repo add hashicorp https://helm.releases.hashicorp.com
helm install --namespace vault vault hashicorp/vault --values values.yaml
```

* Initialize the vault and save the root token (`$ROOT_TOKEN`) and the unseal key (`$UNSEAL_KEY`) somewhere:
```
kubectl -n vault exec -ti vault-0 -- vault operator init -key-shares=1 -key-threshold=1
ROOT_TOKEN=...
UNSEAL_KEY=...
```
* Unseal
```
kubectl -n vault exec -ti vault-0 -- vault operator unseal $UNSEAL_KEY
```

## Configure Vault
* Login
```
export VAULT_ADDR=https://vault.k8s.example.com
vault login
# enter $ROOT_TOKEN
```

* Enable KV secret engine
```
vault secrets enable -version=2 kv
```

* Store example secret
```
vault kv put kv/k8s/default password="secret from vault"
```

* Enable Kubernetes Auth
https://www.vaultproject.io/docs/auth/kubernetes
```
vault auth enable kubernetes
```

* Create reader policy
```
vault policy write reader - <<EOF
path "kv/data/k8s/*" {
  capabilities = ["read"]
}
EOF
```

* Configure Kubernetes Auth
https://www.vaultproject.io/api/auth/kubernetes
```
vault write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc
```

* Create role for kubernetes auth
```
vault write auth/kubernetes/role/default-default \
    bound_service_account_names=default \
    bound_service_account_namespaces=default \
    policies=reader \
	token_num_uses=1 \
    ttl=1m
```

* Deploy example app
```
kubectl apply -f app.yaml
```
