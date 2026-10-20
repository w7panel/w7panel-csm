# W7Panel CSM

Container cloud hosts for W7Panel.

## Development

```sh
make test
npm ci --prefix ui && make build-ui
make build
```

`make run` uses `$HOME/.kube/218.config` and the `default` OIDC client for local
testing. It uses the built UI in the main project and product ID 17057 from
W7 Cloud's prepare endpoint. The callback host is `localhost`, so payment callbacks
will not work. This target explicitly permits HTTP OIDC endpoints and a local
HTTP redirect URI.
`make run` defaults to the `higress-system/higress` Gateway; override
`CSM_SSH_GATEWAY_NAMESPACE` and `CSM_SSH_GATEWAY_NAME` for another cluster.

`make manifests` refreshes the host and data disk CRDs while preserving the
cluster-scoped template, preinstall, and upgrade CRDs. Use `make docker-build`
for the manager/UI image and `make docker-build-system` for the Ubuntu system
image; override `IMG` and `SYSTEM_IMG` with registry tags before publishing.
For deployment, use `make deployHelm HELM_ARGS='-f values.yaml'`
with real OIDC, payment, SSH, and image settings.

## Runtime integration

The multi-stage `Dockerfile` builds the Go manager and the Vite UI at `/app/ui/dist`.
Build and publish that image before setting the chart's `image.repository` and
`image.tag`; an older manager-only image does not contain the UI assets.
CSM creates the `ubuntu-24.04` SystemTemplate on startup if it is absent.
Its image comes from `system.image` (default public Ubuntu system image in CNB).
Build and publish this system image with `Dockerfile.system` before buying a host.
It uses a systemd Ubuntu base and enables OpenSSH. Host Pods prepare SSH keys,
then run `/sbin/init` as PID 1 under Sysbox; they do not install a Kubernetes
distribution. The system disk PVC supplies the writable rootfs layer.
Additional Ubuntu, CentOS, or other Linux versions can be added as
`SystemTemplate` resources with systemd-ready images. Each image must provide
`/sbin/init`, `ssh-keygen`, `/usr/sbin/sshd`, and an enabled SSH service.
The sample manifest is in `config/samples/`.
The deployed manager requires in-cluster Kubernetes credentials and Helm values for
`oidc.discoveryURL`, `oidc.audience`, `oidc.clientID`, `oidc.redirectURI`,
`ssh.gatewayNamespace`, `ssh.gatewayName`, `ssh.publicHost`,
`ssh.portStart`, `ssh.portEnd`, `payment.productID`,
`payment.callbackHost`.
CSM adds a TCP listener to the configured Gateway and a TCPRoute to each host's
ClusterIP Service. Install a Gateway implementation and the `v1alpha2` TCPRoute CRD
first. The Gateway must expose the configured port range at `ssh.publicHost`;
CSM publishes each host at `ssh.publicHost:<assigned port>`. Existing NodePort
Services are converted to ClusterIP as hosts reconcile.
By default, Discovery, issuer and JWKS URLs may use HTTP only on a cluster
Service or loopback host; the browser authorization endpoint and redirect URI
must use HTTPS. `CSM_OIDC_ALLOW_INSECURE_HTTP=true` permits HTTP for local
testing.
When the issuer is a cluster Service, set `oidc.publicAuthorizationURL` to the
HTTPS authorization endpoint exposed to browsers. Its path must match discovery.
The founder sets CPU, memory, storage, and bandwidth monthly prices on the
费用设置 page. CSM calculates quotes on the server; an unset price blocks orders.

Set `payment.credentialNamespace` to the namespace containing the existing
`<account>.w7-config` Secrets (defaults to the CSM release namespace). CSM reads
each account's `thirdparty_cd_token`, `cd_token_expire_time`, `cluster_id` and
`userinfo`, plus the site AppID from the existing License. The W7 gateway checks
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
