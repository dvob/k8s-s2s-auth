# Keycloak
With the following setup I tried to obtain a access_token from Keycloak with the client credentials flow using a service account token instead of a client secret.
Unfortunately this did not work becuase Keycloak requires the `jti` claim in the tokens (see details below).

Setup Kubernetes:
```
minikube start --kubernetes-version v1.19.2 \
	--feature-gates=ServiceAccountIssuerDiscovery=true  \
	--extra-config=apiserver.service-account-issuer=https://kubernetes.default.svc \
	--extra-config apiserver.service-account-signing-key-file=/var/lib/minikube/certs/sa.key

kubectl apply -f - <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:service-account-issuer-discovery
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:service-account-issuer-discovery
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: Group
  name: system:unauthenticated
EOF
```

Setup DNS record for `*.example.com` to `$( minikube ip )`.


Nginx Ingress Controller:
```
# create CA
pcert create ca --ca
pcert create server --with ca --server --dns *.example.com

kubectl create ns ingress-nginx
kubectl -n ingress-nginx create secret tls default-certificate --key server.key --cert server.crt

helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm install --namespace ingress-nginx ingress-nginx \
    --set controller.hostPort.enabled=true \
    --set controller.hostNetwork=true \
    --set controller.kind=DaemonSet \
    --set controller.admissionWebhooks.enabled=false \
    --set controller.extraArgs.default-ssl-certificate="ingress-nginx/default-certificate" \
    --set-string controller.config.force-ssl-redirect=true ingress-nginx/ingress-nginx
```

Setup Keycloak:
```
kubectl create ns keycloak
kubectl apply -n keycloak -f keycloak.yaml
```

Go to https://idp.example.com and login with user `admin` password `admin`.  Under clients create a new client and enter the service account name as `Client ID` (e.g. `system:serviceaccount:default:default`).  Configure the client as follows:
* Settings (Tab)
  * Access Type: `bearer-only`
* Credentials (Tab)
  * Authenticator: `Signed Jwt`
  * Use JWKS URL: `On`
  * JWKS URL: `https://kubernetes.default.svc/openid/v1/jwks`

Get a projected token:
```
kubectl create -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: read-token2
spec:
  restartPolicy: Never
  containers:
  - name: read-token
    image: busybox
    command: ['cat', '/var/run/secrets/tokens/service2']
    volumeMounts:
    - mountPath: /var/run/secrets/tokens
      name: service2-token
  serviceAccountName: default
  volumes:
  - name: service2-token
    projected:
      sources:
      - serviceAccountToken:
          path: service2
          expirationSeconds: 60000
          audience: https://idp.example.com/auth/realms/master
EOF
```

```
token=$( kubectl logs read-token2 )
```

Get access token from token endpoint:
```
$ curl -k https://idp.example.com/auth/realms/master/protocol/openid-connect/token \
	--data grant_type=client_credentials \
	--data client_assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer \
	--data client_assertion=$token | jq

{
  "error": "unauthorized_client",
  "error_description": "Client authentication with signed JWT failed: Missing ID on the token"
}
```
Does not work because the `jti` claim is not set by Kubernetes but required by Keycloak:
* https://github.com/kubernetes/kubernetes/blob/cdec7e8b1faaba8f8bc3af1c5d256fd7433b5631/pkg/serviceaccount/claims.go#L61
* https://github.com/keycloak/keycloak/blob/12d824728882789de63b5dd19e5d8a4a6847ffda/services/src/main/java/org/keycloak/authentication/authenticators/client/JWTClientSecretAuthenticator.java#L166

## Links
* Keycloak Custom Authenticators: https://github.com/keycloak/keycloak-documentation/blob/master/server_development/topics/auth-spi.adoc#authentication-of-clients
* Keycloak Example Authenticator: https://github.com/keycloak/keycloak/tree/master/examples/providers/authenticator
