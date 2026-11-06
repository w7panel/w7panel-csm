# W7Panel CSM

Container cloud hosts for W7Panel.

## Development

```sh
make test
npm ci --prefix ui && make build-ui
make build
```

`make run` uses `$HOME/.kube/218.config` and the `default` OIDC client for local
testing. It uses the built UI in the main project. `make run` sets
`MOCK_PAY=true`: orders are priced by CSM and immediately applied without
calling W7 Cloud. Set monthly prices on 费用设置 before buying. This target
explicitly permits HTTP OIDC endpoints and a local HTTP redirect URI.
`make run` defaults to the `higress-system/gateway` Gateway; override
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
`/sbin/init`, `ssh-keygen`, `/usr/sbin/sshd`, and an enabled SSH service. The
`ubuntu-24.04-cloudinit` template also requires cloud-init with the NoCloud
datasource; CSM publishes its seed through a per-host Secret before starting
the Pod. New purchases use this template. The original `ubuntu-24.04` template
remains available to existing hosts and is excluded from new purchases.
Build the cloud-init image with `make docker-build-system SYSTEM_IMG=<registry>/w7panel-csm:system-ubuntu-24.04-cloudinit`
and publish it before setting `system.cloudInitImage` in Helm values. This Helm
value defaults to empty; CSM creates the cloud-init template only after it is
configured with the published image.
Apply the updated CRDs (`make install`) before upgrading the manager; Helm does
not update resources stored under `crds/` during a release upgrade.

Initialization entries for cloud-init hosts contain `#cloud-config` YAML.
Selected entries merge by configured order: mappings merge recursively, lists
append, and later scalar values replace earlier values. cloud-init itself runs
modules in its own stages. Host status becomes Ready after `cloud-init status`
reports success; failures and the bounded cloud-init output log appear in the
initialization task list. Existing Shell preinstall tasks remain supported for
hosts on the original template.
The sample manifest is in `config/samples/`.
The deployed manager requires in-cluster Kubernetes credentials and Helm values for
`oidc.discoveryURL`, `oidc.audience`, `oidc.clientID`, `oidc.redirectURI`,
`ssh.publicHost`, `ssh.domainSuffix`, `ssh.jumpPort`, and `ingress.host`.
With `ssh.jumpEnabled=true`, one LoadBalancer Service exposes the CSM SSH gateway
at `ssh.publicHost:ssh.jumpPort`. Add your public key once in the host detail page:

```sh
ssh -p 30000 -i ~/.ssh/csm-218 root+<host.status.sshHost>@csm.fan.b2.sz.w7.com
```

The gateway authorizes the key for that specific, paid, unexpired Ready host, then
uses a five-minute SSH user certificate to connect as root. The certificate is
restricted to the host UID. Host Services still map port 22 to container port 2222.
Shell, commands, SFTP, both SCP protocols and target-loopback forwarding are supported.
Remote listeners, agent forwarding, X11 and environment requests are rejected.
SSH signal requests preserve the backend response: the current OpenSSH image rejects
protocol signals for root sessions; interactive Ctrl+C works through the PTY.
Use the SSH configuration shown in the host page for VS Code Remote SSH (TCP mode;
Unix-socket mode is not supported). Only `localhost`, `127.0.0.1` and `::1` forwarding
destinations are accepted, and they refer to the target host, not the gateway.
This limits gateway forwarding; a root shell can still originate its own network connections.

The existing `jumpPublicKeys` field and `/jump-keys` endpoint now mean gateway login
keys. Existing `sshPublicKeys` remain installed for direct internal access. The old
`ssh -J ...` entrypoint is replaced. Each process allows 32 connections per source
IP, 512 in total, and 32 concurrent channels per connection. NAT clients share the
source-IP limit. Each new channel rechecks authorization; revoking a key prevents
new channels but does not terminate an already running shell. Certificate expiry
also does not terminate established connections. Logs contain host UID, source,
public-key fingerprint, result and duration, never commands or session contents.

### SSH gateway migration

Schedule maintenance: host Deployments use Recreate, so adding CA trust and fixed
host keys restarts hosts and interrupts existing sessions. Back up the gateway
identity Secret `csm-ssh-jump-hostkey`, the user CA Secret `csm-ssh-user-ca` in the
pricing namespace, and per-host `csm-ssh-<UID>` Secrets. The gateway fingerprint
is retained; host keys become fixed per-host identities pinned by the gateway.
Existing internal direct clients may need to verify the new host fingerprint.

1. Upgrade with `ssh.jumpEnabled=true,ssh.prepareOnly=true`. This runs host
   reconciliation and prepares Secrets/CA trust, but does not listen for SSH.
   Wait for all affected host Deployments to finish rolling out and return Ready.
2. Upgrade with `ssh.prepareOnly=false` to open the new direct-login gateway.
   Verify a test host using the new command before ending maintenance.

For a non-Helm process the equivalent flag is `CSM_SSH_PREPARE_ONLY=true`.
To roll back, restore the previous release and let host Deployments reconcile;
clients must return to the old ProxyJump command and require both legacy key entries.
Do not delete the identity Secrets during rollback. CA rotation is a maintenance
operation: replace the CA Secret, restart the manager and wait for public trust
updates before admitting new connections. Keep CA private keys out of host Pods.
The gateway shares the manager ServiceAccount and its existing cluster-wide Secret
permissions; restrict access to this workload and its service account. A compromised
gateway/CA can impersonate users to hosts, and the proxy sees session plaintext.

By default, Discovery, issuer and JWKS URLs may use HTTP only on a cluster
Service or loopback host; the browser authorization endpoint and redirect URI
must use HTTPS. `CSM_OIDC_ALLOW_INSECURE_HTTP=true` permits HTTP for local
testing.
When the issuer is a cluster Service, set `oidc.publicAuthorizationURL` to the
HTTPS authorization endpoint exposed to browsers. Its path must match discovery.
The founder sets CPU, memory, storage, and bandwidth monthly prices on the
费用设置 page. The founder can also set combination discounts for an exact
duration, CPU, memory, storage, and bandwidth match, with separate new purchase
and renewal percentages. Expansion and unmatched orders use unit prices. CSM
calculates quotes on the server; an unset price blocks orders.

CSM reads each account's W7 credentials from the cluster-scoped User CRD under
`spec.cloud`, plus `spec.cloudOpenid` and the site AppID/AppSecret from the
existing License. It obtains the product ID from W7's signed `prepare2` endpoint;
`payment.productID` remains an optional override. `payment.callbackHost` defaults
to `ingress.host`. Refreshed order tokens are written back to that User. The W7
gateway checks payment status before `KubeStore` applies a Csm or DataDisk change.
For local simulation, set `MOCK_PAY=true` (Helm: `payment.mockPay=true`). It
creates a local order and immediately marks it paid; the chart defaults to false.

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
In Wujie, CSM hides its own navigation and requests an OIDC code from the host's
`getOidcCode` bus event. CSM exchanges that code through `/auth/login` for its
own verified token; requests use the host's `props.url` proxy path and
`X-W7Panel-Token` header.
The host's `routeChange` event selects CSM pages. Standalone CSM keeps its
browser redirect login and navigation.
