# CSM 本地测试

## 测试环境

- `~/.kube/218.config` 连接 **218 线上集群**。执行 `make run` 时，CSM 进程在本机运行，但 Kubernetes 读写操作会作用于该线上集群。
- `*.fan.b2.sz.w7.com` 支持泛域名解析。已验证 `csm-local-test.fan.b2.sz.w7.com` 解析到 `218.23.2.48`；该域名指向 218 集群，不指向本机的 `make run` 服务。
- 本地服务使用 `http://172.16.1.3:8082`；OIDC 使用 218 集群的 `default` 客户端。浏览器需能访问 `172.16.1.3`。本地测试统一使用这个局域网地址，不使用 `localhost` 或 `127.0.0.1`。
- SSH 跳板在 218 集群的 CSM 服务中运行，公网入口为 `csm.fan.b2.sz.w7.com:30000`；主机使用 `*.fan.b2.sz.w7.com` 域名区分。
- 线上 CSM 已绑定 `https://csm.fan.b2.sz.w7.com`。

## 检查连接与解析

```sh
kubectl --kubeconfig="$HOME/.kube/218.config" get namespaces
getent ahostsv4 csm-local-test.fan.b2.sz.w7.com
```

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

把 `cat` 输出的**公钥**分别粘贴到目标 CSM 主机详情的“添加跳板公钥”和“添加主机公钥”。私钥 `~/.ssh/csm-218` 只留在客户端，不要上传或发送。然后在**同一个客户端终端**运行：

```sh
eval "$(ssh-agent -s)"
ssh-add "$HOME/.ssh/csm-218"
ssh -J jump@csm.fan.b2.sz.w7.com:30000 root@<主机详情中的域名>
```

例如连接 `csm-admin/test2`，将最后一行换成 `ssh -J jump@csm.fan.b2.sz.w7.com:30000 root@test2-csm-admin-34e8a6d0f93d.fan.b2.sz.w7.com`。`ssh-agent` 只对当前终端生效；其他终端需重新加载私钥。`ssh -i` 不会自动将私钥传给 `-J` 跳板。首次连接时核对跳板主机指纹；218 跳板当前指纹为 `SHA256:U6lUlqDAmopw6XEXUuyebmI493vJPZmszMkB437A2PM`。
