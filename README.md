# Kubernetes Service Accounts
Service accounts are well known in Kubernetes to access the Kubernets API from within the cluster. This is often used for infrastructure components like operators and controllers. But we can also use service accounts to implement authentication in our own applications.

This README tries to give an overview on how service accounts work and and shows a couple of variants how you can use them for authentication. Further this repository contains an example Go service which shows how to implement the authentication in an application.

If you have questions, feedback or if you want to share your expirience using these features feel free to start a [discussion](https://github.com/dvob/k8s-s2s-auth/discussions).
# Tutorial
## Scenario
In our tutorial we look at a simple scenario with to services:
* Service 1 (Client)
* Service 2 (Server)

Service1 wants to call Service2. Service2 shall only respond to authenticated requests.

## Setup Cluster
Setup a test cluster with kind, minikube or another tool of your choice. At a later point we want to explore the TokenRequestProjection feature, thats why we have set the following options on the API server:
* `--service-account-issuer=yourIssuer`
* `--service-account-signing-key-file=pathToServiceAccountKey`

For this tutorial I used Kubernetes 1.20.2 and an earlier version also ran on 1.19.4 but for this you have to set additional options (see in Git history).

### minikube
```
minikube start --kubernetes-version v1.20.2 \
	--extra-config=apiserver.service-account-issuer=https://kubernetes.default.svc \
	--extra-config apiserver.service-account-signing-key-file=/var/lib/minikube/certs/sa.key
```

## Service 1 (Client)
First we want to deploy Service 1 and see how Service 1 can get a service account token which it later needs to authenticate itself to Service 2:
```
kubectl create ns mytest

# set context to the new namespace
kubectl config set-context $( kubectl config current-context ) --namespace mytest
```

If we create a new namespace the service account controller creates the default service account for us. Further the token controller creates a secret which contains the service account token.
We verify that the default service account and its token got created and extract the token from the secret:
```
kubectl get serviceaccount default

token_name=$( kubectl get serviceaccounts default --template '{{ ( index .secrets 0 ).name }}' )

kubectl get secret $token_name

token=$( kubectl get secret $token_name --template '{{ .data.token }}' | base64 -d )
echo $token
```

The service account token is a JWT so the claims in the payload can be inspected as follows:
```
echo $token | cut -d . -f 2 | base64 -d | jq
```
```
{
  "iss": "kubernetes/serviceaccount",
  "kubernetes.io/serviceaccount/namespace": "mytest
  "kubernetes.io/serviceaccount/secret.name": "default-token-bwsb6",
  "kubernetes.io/serviceaccount/service-account.name": "default",
  "kubernetes.io/serviceaccount/service-account.uid": "9c2dc042-3543-4cc6-a458-542c0f56a324",
  "sub": "system:serviceaccount:mytest:default"
}
```

If we create a pod without specifiyng anything concering servcie accounts the service account admission controller sets certain defaults for us:
* sets the service account itself
* adds a volume with the token to to the pod
* add volume mount for each container which makes the token available under `/var/run/secrets/kubernetes.io/serviceaccount/token`

Lets verify this by creating a pod which prints out the token:
```
kubectl create -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: read-token
spec:
  restartPolicy: Never
  containers:
  - name: read-token
    image: busybox
    command: ['cat', '/var/run/secrets/kubernetes.io/serviceaccount/token']
EOF
```

After the creation of the pod we can see that the service account, the volume and the volume mount got set:
```
# look for .spec.serviceAccountName, .spec.volumes and .spec.containers[0].volumeMounts
kubectl get pod read-token -o yaml
```

Wait until the pod has completed and then verify that the pod printed out the service account token:
```
# wait for completed state
kubectl get pod -w

# verifiy that the pod could read the token
kubectl logs read-token
```

We could see how we get obtain a token in a pod and now we start Service 1 which will read the token and send it to Service 2.
```
kubectl apply -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: service1
  name: service1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service1
  template:
    metadata:
      labels:
        app: service1
    spec:
      containers:
      - name: service1
        image: dvob/k8s-s2s-auth
        args:
        - client
        - http://service2.mytest.svc
EOF
```
Since we haven't started Service 2 yet the requests will fail, but we'll change this in a moment.

## Service 2 (Server)
Service 1 sends its token to Service 2 in the HTTP Authorization header:
```
GET / HTTP/1.1
Host: service2.mytest.svc:8080
Authorization: Bearer eyJhbGciOiJSUzI1...<shortened>...NiIsImtpZKRavXR4H3UQ
User-Agent: Go-http-client/1.1
```

Service 2 now shall authenticate the requests so it somehow has to validate the token from the HTTP header. There are multiple ways to do this:
* Token Review API
* Verify Signature of the token (JWT)

### TokenReview
The API server has the public key to verify the tokens. External parties can send tokens to the token review API to verify if they are valid. That's what we try out next. For this we send a `TokenReview` with a token (we use `$token` which we've extracted before) to the API server. Since this action is not available as part of `kubectl` we use curl.

For that we first have to extract the pathes to the credentials from the kube config:
```
URL=$( kubectl config view --raw -o json | jq -r '.clusters[] | select( .name == "minikube") | .cluster.server' )
CA=$( kubectl config view --raw -o json | jq -r '.clusters[] | select( .name == "minikube") | .cluster["certificate-authority"]' )
CERT=$( kubectl config view --raw -o json | jq -r '.users[] | select( .name == "minikube") | .user["client-certificate"]' )
KEY=$( kubectl config view --raw -o json | jq -r '.users[] | select( .name == "minikube") | .user["client-key"]' )
```

Then we can send the token (`$token`) as part of the `TokenReview` to the API server:
```
curl --cacert "$CA" --cert "$CERT" --key "$KEY" --request POST --header "Content-Type: application/json" "$URL/apis/authentication.k8s.io/v1/tokenreviews" --data @- <<EOF
{
  "kind": "TokenReview",
  "apiVersion": "authentication.k8s.io/v1",
  "spec": {                                
    "token": "$token"
  }
}
EOF
```

As a response we get a TokenReview again but with the result of the review in the status. We see that the token is authenticated together with information about the token owner like its username and groups:
```json
{
  "kind": "TokenReview",
  "apiVersion": "authentication.k8s.io/v1",
  "spec": {
    "token": "eyJhbGciOiJSUzI1NiIsImtpZCI6Imp4cWhWd2xGal82eTNWQ1loZ25TSG5SVFZxa1llZEZySjNRVW5WV1F2XzgifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJzMSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VjcmV0Lm5hbWUiOiJkZWZhdWx0LXRva2VuLWJ3c2I2Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQubmFtZSI6ImRlZmF1bHQiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiI5YzJkYzA0Mi0zNTQzLTRjYzYtYTQ1OC01NDJjMGY1NmEzMjQiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6czE6ZGVmYXVsdCJ9.CggLOjeMQjh_GoLkUHSyLJqC2jYlKyZHRzT86GrfmgYP3uMgNGfkUk7j2llOQEGLko-TRmTCby3PrioZd-3MdM2vYa9ZQBsP8GcZZNEHvQ8zY5qEiBpsh_KFuMoXb--nNVlz6uyZbZEo5bewWCzdFRKTU8OuZZGAnkDQXNB3E_WzB2KunIjmIZ_iUdWYwaTUmslvu7TBNV27PhQf1krj6Go0TwlhFoWd_wXEYiSl6qOkkCqzaACnKcJ2aKzaPPdOWj0GOR6PsdkN-LQcpr5-M3Pw3rihjQgB34HUNCHc_du3xeQRSsRM4Q1iU-mS-vLZ5VXs2KHnedOcngVCOp_NAQ"
  },
  "status": {
    "authenticated": true,
    "user": {
      "username": "system:serviceaccount:mytest:default",
      "uid": "9c2dc042-3543-4cc6-a458-542c0f56a324",
      "groups": [
        "system:serviceaccounts",
        "system:serviceaccounts:mytest",
        "system:authenticated"                                                       
      ]
    }        
  }          
}
```

If the `--service-account-lookup` option is enabled on the API server (which is the default), the token review API does not only verify the signature of the token but also checks if the service account still exists.
If we remove the default service account and call the token review API again we get back an error in the status of the token review:
```
kubectl delete sa default
```
```
  "kind": "TokenReview",
  "apiVersion": "authentication.k8s.io/v1",
  ...
  "status": {
    "user": {},
    "error": "[invalid bearer token, Token has been invalidated]"
  }
```

Now start Service 2 to see the token review in action. For this we create a deployment, a service and the appropriate clusterrolebinding which lets us use the token review API:
```
kubectl apply -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service2
  labels:
    app: service2
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service2
  template:
    metadata:
      labels:
        app: service2
    spec:
      containers:
      - name: service2
        image: dvob/k8s-s2s-auth
        args:
        - server
        - --mode
        - tokenreview
---
apiVersion: v1
kind: Service
metadata:
  name: service2
  labels:
    app: service2
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 8080
  selector:
    app: service2
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  creationTimestamp: null
  name: auth-delegator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
- kind: ServiceAccount
  name: default
  namespace: mytest
EOF
```

If we now take a look at the logs of Service 1 we no longer see `no such host` errors but errors that our Token has been invalidated:
```
kubectl logs -f -l app=service1
```
```
...
2021/03/30 08:34:49 Get "http://service2.mytest.svc": dial tcp: lookup service2.mytest.svc on 10.96.0.10:53: no such host
2021/03/30 08:34:54 Get "http://service2.mytest.svc": dial tcp: lookup service2.mytest.svc on 10.96.0.10:53: no such host
2021/03/30 08:34:59 target=http://service2.mytest.svc, status=401, response: 'Unauthorized: [invalid bearer token, Token has been invalidated]'
2021/03/30 08:35:04 target=http://service2.mytest.svc, status=401, response: 'Unauthorized: [invalid bearer token, Token has been invalidated]'
...
```
That is because we deleted the service account before which invalidated the token. To get rid of the error we have to restart Service 1.
```
kubectl delete pod -l app=service1
```
If we now look into the logs of Service 1 again we see that our requests get authenticated correctly and we get back a HTTP 200 from Service 2.
```
kubectl logs -f -l app=service1
```
```
2021/03/30 08:38:39 start client: http://service2.mytest.svc
2021/03/30 08:38:39 target=http://service2.mytest.svc, status=200, response: 'hello system:serviceaccount:mytest:default'
2021/03/30 08:38:44 target=http://service2.mytest.svc, status=200, response: 'hello system:serviceaccount:mytest:default'
...
```

### JWT
The token review API is not really a standard. However, JWTs is a standard and many frameworks and libraries support authentication using JWTs.
To validate a token we need the public key of the private key which was used to sign the service account token.
We find the public key on the Kube API server, since it is used there to authenticate requests from service accounts and to answer token review API requests.
The location of the public key is configured with the option `--service-account-key-file`. In a minikube setup we can find the location as follows:
```
kubectl -n kube-system get pod kube-apiserver-minikube -o yaml | grep service-account-key-file
```
```
    - --service-account-key-file=/var/lib/minikube/certs/sa.pub
```

Copy the public key to our host:
```
minikube ssh sudo cat /var/lib/minikube/certs/sa.pub > sa.pub
```

Verify the signature of the service account token we have extracted before:
```
openssl dgst -sha256 -verify sa.pub -signature <( echo -ne $token | awk -F. '{ printf("%s", $3) }' | tr '\-_' '+/' | base64 -d ) <( echo -ne $token | awk -F. '{ printf("%s.%s", $1, $2) }' )
```
The decoding of the signature shows an error `base64: invalid input` because in the JWT signature the padding (`=`) is removed and `base64` does not like that. Nevertheless the verification should still work and show `Verfied OK`. We also have to convert the signature which is base64url encoded signature to a base64 so that base64 can decode it.

Now we can start Service 2 with the `--mode` option set to `jwt-pubkey` to see the authentication with the public key in action. We do this just locally that we don't have to create a configmap for `sa.pub`.
```
docker run -it -p 8080:8080 --rm -v $(pwd)/sa.pub:/sa.pub dvob/k8s-s2s-auth server --mode jwt-pubkey --pub-key /sa.pub
```

Test it with curl:
```
curl -H "Authorization: Bearer $token" http://localhost:8080
```

### Disadvantages
We have now seen how we can use service accounts to implement authentication. But the shown methods have some drawbacks:

TokenReview:
* Needs additional request to the API server
* The application has to talk to the Kubernetes API
* Token never expires unless you delete/recreate the service account

JWT:
* Tokens never expire (no `exp` field in JWT)
* Tokens have no audience (no `aud` field in JWT)
* Copying the service account public key (`sa.pub`) from the API server to all services which have to do authentication is not optimal

### TokenRequestProjection
The TokenRequestProjection feature enables the injection of service account tokens into a Pod through a projected volume.

In contrast to the service account tokens we've used before, these tokens have the following benefits:
* Tokens expire (`exp` claim in JWT is set)
* Tokens are bound to an audience (`aud` claim in JWT)
* Tokens are never stored as secret but directly injeted into the pod from `kubelet`
* Tokens are bound to a pod. When the pod gets deleted the token review API no longer treats the token as valid

Create a pod with a projected service account volume:
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
          expirationSeconds: 600
          audience: service2
EOF
```

Wait until the Pod has completed and take a look at the claims of our new token:
```
kubectl logs read-token2 | cut -d. -f2 | base64 -d | jq
```

Since we're now able to create tokens for specific audiences we update service2 to only accept tokens for the audience 'service2':
```
kubectl apply -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service2
  labels:
    app: service2
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service2
  template:
    metadata:
      labels:
        app: service2
    spec:
      containers:
      - name: service2
        image: dvob/k8s-s2s-auth
        args:
        - server
        - --mode
        - tokenreview
        - --audience
        - service2
EOF
```

Now Service 1 is no longer able to send requests to Service 2 because Service 2 requires that the audience is `service2`. You can verify this in the logs of Service 1.
We now update Service 1 so that it uses a projected service account volume with the appropriate audience:
```
kubectl apply -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: service1
  name: service1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service1
  template:
    metadata:
      labels:
        app: service1
    spec:
      containers:
      - name: service1
        image: dvob/k8s-s2s-auth
        args:
        - client
        - http://service2.mytest.svc
        - --token-file
        - /var/run/secrets/tokens/service2
        volumeMounts:
          - mountPath: /var/run/secrets/tokens
            name: service2-token
      volumes:
      - name: service2-token
        projected:
          sources:
          - serviceAccountToken:
              path: service2
              expirationSeconds: 600
              audience: service2
EOF
```
Now you can verify in the logs of Service 1 that it again is able to authenticate itself to Servcice 2.

### ServiceAccountIssuerDiscovery
The ServiceAccountIssuerDiscovery is available as a beta feature since Kubernetes 1.20. It allows to fetch the public key from the API server to verfiy the JWT signatures. For this it uses the OpenID Connect Discovery mechanism. Since this is a standard it is supported by many libraries and frameworks.

Allow access to the discovery endpoint:
```
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

Fetch the openid-configuration URL (see above on how to set the environment variables):
```
curl --cacert "$CA" --cert "$CERT" --key "$KEY" "$URL/.well-known/openid-configuration"
```
```json
{
  "issuer": "https://kubernetes.default.svc",
  "jwks_uri": "https://192.168.49.2:8443/openid/v1/jwks",
  "response_types_supported": [
    "id_token"
  ],
  "subject_types_supported": [
    "public"
  ],
  "id_token_signing_alg_values_supported": [
    "RS256"
  ]
}
```
```
jwks_uri=$(curl --cacert "$CA" --cert "$CERT" --key "$KEY" "$URL/.well-known/openid-configuration" | jq -r .jwks_uri )
```

From here we fetch the JWKS (JSON Web Key Set) URL `jwks_uri` from which we can download the actual public key:
```
curl --cacert "$CA" $jwks_uri
```
```
{
  "keys": [
    {
      "use": "sig",
      "kty": "RSA",
      "kid": "iUPSHSAprvOukTss4IlKZ8VVrMOy4G4NqXxBT-3ae-o",
      "alg": "RS256",
      "n": "pshH9GeIJcuUDbJwP9oummjXcJcMh8bIXTM9GT2sMx8P7CgyKrp0XXLghpYJB_Kqar8jHo1U-B2QWKI-rIyS7Nx9CfpENhnLWDcj2XZmC3gw2X93e9ZYM74xyvvCFGnu34RMS0TjaQtQRaFVnwmxmjK0sHbwwMq8rfqRRyr8Rg-9yz03TQdUMeSfUTE-I-bykX7F_XezFRAFwgOR-ZCMAcq4BrB4j0l2OH5v3QmNXAVr_ytSxEF-yrrP3oUwauLfIBo-xxHcWJfnOdaZ25DiH5zY3TilA3F4FP3vbiNApMZaJBwvrypUxHBnVf-cmBNbfuRl6o6ZgLYZHAMVm1mAXw",
      "e": "AQAB"
    }
  ]
}
```

To see this in action we update the Service 2 deployment and change the `--mode` option to `oidc-discovery`.
```
kubectl apply -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service2
  labels:
    app: service2
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service2
  template:
    metadata:
      labels:
        app: service2
    spec:
      containers:
      - name: service2
        image: dvob/k8s-s2s-auth
        args:
        - server
        - --mode
        - oidc-discovery
        - --issuer-url
        - https://kubernetes.default.svc
        - --ca
        - /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        - --audience
        - service2
EOF
```
We also have to set the `--ca` variable so that Service 2 trusts the certificate under https://kubernetes.default.svc. Service 2 now during the startup connects to the OIDC disovery endpoints and gets the jwks_uri. Then it downloads the public key from the JWKS URI endpoint which it then uses to validate the tokens.

This way your application can rely entirely on OIDC standards and no longer has to talk to Kubernetes APIs.

# Appendix
## Links
* Service Accounts
  * https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/
* TokenRequestProjection
  * https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection
  * https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/svcacct-token-volume-source.md
  * https://jpweber.io/blog/a-look-at-tokenrequest-api/
* ServiceAccountIssuerDiscovery
  * https://github.com/kubernetes/enhancements/blob/master/keps/sig-auth/20190730-oidc-discovery.md

## Mentioned Kubernetes Features

Feature | Stage | Version | Description
--- | --- | --- | ---
TokenRequest | GA | 1.20 | Enable the TokenRequest endpoint on service account resources.
TokenRequestProjection | GA | 1.20 | Enable the injection of service account tokens into a Pod through the projected volume.
ServiceAccountIssuerDiscovery | beta | 1.20 | Enable OIDC discovery endpoints (issuer and JWKS URLs) for the service account issuer in the API server.
