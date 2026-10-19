# W7Panel CSM

Container cloud hosts for W7Panel.

## Development

```sh
go test ./...
cd ui && npm install && npm run build
```

## Runtime integration

The multi-stage `Dockerfile` builds the Go manager and the Vite UI at `/app/ui/dist`.
Build and publish that image before setting the chart's `image.repository` and
`image.tag`; an older manager-only image does not contain the UI assets.
CSM creates the `ubuntu-24.04` SystemTemplate on startup if it is absent.
Its image comes from `system.image` (default `w7panel-csm-system:ubuntu-24.04`).
Build and publish this system image with `Dockerfile.system` before buying a host.
It uses a systemd Ubuntu base and enables OpenSSH. Host Pods prepare SSH keys,
then run `/sbin/init` as PID 1 under Sysbox; they do not install a Kubernetes
distribution. The system disk PVC supplies the writable rootfs layer.
Additional Ubuntu, CentOS, or other Linux versions can be added as
`SystemTemplate` resources with systemd-ready images. Each image must provide
`/sbin/init`, `ssh-keygen`, `/usr/sbin/sshd`, and an enabled SSH service.
The sample manifest is in `config/samples/`.
The manager requires in-cluster Kubernetes credentials and the Helm values for
`oidc.discoveryURL`, `oidc.audience`, `oidc.clientID`, `oidc.redirectURI`,
`ssh.gatewayNamespace`, `ssh.gatewayName`, `payment.productID`,
`payment.callbackHost`, `payment.quoteURL`, and `payment.quoteSecretName`.
Discovery, issuer and JWKS URLs may use HTTP only on an explicitly configured
cluster Service or loopback host. The browser authorization endpoint and
redirect URI must use HTTPS.
The quote Secret key defaults to `token`. The quote service must be a trusted
HTTPS endpoint accepting an authenticated JSON `order.Request` and returning
`{"amountCents":1234,"params":{"hour":"720","cpu":"2","memory":"4","storage":"40","bandwidth":"5"}}`.
It calculates the price on the server; the browser cannot submit a price.
An explicit `"free":true` is required when `amountCents` is zero.

Each tenant namespace needs a Secret named by `payment.credentialSecretName`
(default `csm-w7-order`) containing `account`, `appId`, `clusterId`, and either
`token` or `openId`. Optional `expiresAt` uses RFC3339. The W7 gateway checks
payment status before `KubeStore` applies a Csm or DataDisk change.

The UI can call `GET /csm-api/v1/auth/config` for the OIDC authorization URL,
client ID and fixed redirect URI. After a PKCE code flow, it sends `code`,
`code_verifier`, and `redirect_uri` to `POST /csm-api/v1/auth/login`. It can send
`refresh_token` to `POST /csm-api/v1/auth/refresh`. Both return a verified
`access_token`; other `/csm-api/v1` routes require it as an OIDC bearer token.
The current UI's `/login/config` and `/login` paths are aliases of those routes;
the config response uses `authorizationEndpoint`, `clientId` and `redirectUri`.
If the panel and CSM API have different origins, set `terminal.allowedOrigin`
to the panel's HTTPS origin so its WebSocket terminal can connect.
The host's `X-W7Panel-Token` header alone does not grant CSM access.
