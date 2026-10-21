# CSM 本地测试

## 测试环境

- `~/.kube/218.config` 连接 **218 线上集群**。执行 `make run` 时，CSM 进程在本机运行，但 Kubernetes 读写操作会作用于该线上集群。
- `*.fan.b2.sz.w7.com` 支持泛域名解析。已验证 `csm-local-test.fan.b2.sz.w7.com` 解析到 `218.23.2.48`；该域名指向 218 集群，不指向本机的 `make run` 服务。
- 本地服务使用 `http://172.16.1.3:8082`；OIDC 使用 218 集群的 `default` 客户端。浏览器需能访问 `172.16.1.3`。本地测试统一使用这个局域网地址，不使用 `localhost` 或 `127.0.0.1`。
- SSH 网关在 218 集群的 CSM 服务中运行，公网入口为 `csm.fan.b2.sz.w7.com:30000`；主机使用 `*.fan.b2.sz.w7.com` 域名区分。
- 线上 CSM 已绑定 `https://csm.fan.b2.sz.w7.com`。

## 检查连接与解析

```sh
kubectl --kubeconfig="$HOME/.kube/218.config" get namespaces
getent ahostsv4 csm-local-test.fan.b2.sz.w7.com
```

## 部署前检查

部署到 218 前先停止本机连接该集群的 `make run` / `go run .`。当前控制器没有启用 leader election；
本机旧版和集群新版同时运行会反复覆盖 Deployment，造成主机持续重建。不要在新网关运行期间启动旧版控制器。
本地页面调试应只启动前端；需要启动完整后端时，先安排唯一控制器和维护窗口。

## 启动本地测试

在项目根目录执行：

```sh
make build-ui
make run
```

`make build-ui` 生成前端静态文件；`make run` 加载这些文件并在本机启动 Go 服务。首次构建前若缺少前端依赖，先执行 `npm ci --prefix ui`。

PWA 的 Service Worker 仅能在 HTTPS 等安全上下文注册；通过上述局域网 HTTP 地址只能检查页面布局与静态资源，安装和离线行为需在 HTTPS 环境验证。

`make run` 设置 `MOCK_PAY=true`：下单不会向 W7 Cloud 发起真实支付，但会按模拟支付结果向 **218 线上集群** 应用资源。测试购买或其他写操作前，确认目标命名空间和资源允许修改。

## 验证 SSH

以下命令都在发起 SSH 连接的客户端电脑上执行。若 `~/.ssh/csm-218` 已存在，跳过生成密钥，避免覆盖原有私钥；否则生成一对密钥：

```sh
mkdir -p "$HOME/.ssh"
ssh-keygen -t ed25519 -f "$HOME/.ssh/csm-218" -C csm-218
cat "$HOME/.ssh/csm-218.pub"
```

把 `cat` 输出的**公钥**添加到目标 CSM 主机详情的“SSH 登录公钥”。私钥只留在客户端。
新网关部署并完成维护切换后，连接方式为：

```sh
ssh -p 30000 -i ~/.ssh/csm-218 root+<主机标识>@csm.fan.b2.sz.w7.com
```

主机标识为详情页的 `status.sshHost`。从页面复制 SSH 配置到 `~/.ssh/config` 后，
可用同一别名连接终端、SFTP/SCP 和 VS Code Remote SSH。VS Code 使用默认 TCP 模式，
不启用 `remote.SSH.remoteServerListenOnSocket`。首次连接核对网关指纹；
原网关 Secret 保留时，指纹仍为 `SHA256:U6lUlqDAmopw6XEXUuyebmI493vJPZmszMkB437A2PM`。

## 隔离测试（不连接 218）

```sh
go test ./...
CSM_SSH_TEST_IMAGE=ccr.ccs.tencentyun.com/afan-public/w7panel-csm:system-ubuntu-24.04-test-20261019 go test ./pkg/sshjump -run TestOpenSSHIntegration -v -count=1 -timeout=120s
```

集成测试要求本机 Docker、ssh、sftp、scp，以及本地已有的 CSM 系统镜像。
测试只创建临时容器、临时密钥和回环监听端口，使用内存 Kubernetes 客户端，并在结束时删除容器。
覆盖真实 OpenSSH 的终端尺寸、Ctrl+C、root 协议信号拒绝、命令退出码、标准输出/错误、SFTP、新旧 SCP、回环转发、
撤销公钥后新通道拒绝、后端主机密钥不匹配和连接关闭。未设置镜像变量时跳过该集成测试。
VS Code 客户端仍需按页面配置做人工实机验收。

## 218 验证记录（2026-10-21）

- 当前测试镜像：`docker.cnb.cool/i0358/w7panel-csm:ssh-direct-20261021-032036-r2`。
- Helm release：`default/w7panel-csm`，revision `37`，`ssh.prepareOnly=false`。
- `csm-admin/test`、`csm-admin/test2` 均已完成切换；公网 SSH 直连和 root 身份验证通过。
- 已验证 PTY、stdout/stderr 与退出码、SFTP 上传下载、新旧 SCP、目标回环转发、非回环转发拒绝和错误公钥拒绝。
- `https://csm.fan.b2.sz.w7.com/healthz` 返回 200，线上前端资源哈希与测试构建一致；网关指纹保持不变。
- 发现并修复 Sysbox Secret 挂载目录为 `1777` 导致 OpenSSH 拒绝 principal 文件的问题：启动时复制到 root 所有、权限 `600` 的 `/etc/ssh/csm-principals`，保留 StrictModes 检查，并增加对应真实 OpenSSH 回归测试。
- VS Code Remote SSH 客户端尚未实机验收，所需的命令执行、文件传输和 TCP 转发已验证。
- 回滚资料与验证结果保存在本机 `/tmp/csm-ssh-218-deploy/`；Secret 备份权限为 `600`。原 `1.0.5` 可使用 Helm revision `32` 回滚；回滚会重启主机，并恢复旧的 ProxyJump 登录方式。
