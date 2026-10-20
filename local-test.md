# CSM 本地测试

## 测试环境

- `~/.kube/218.config` 连接 **218 线上集群**。执行 `make run` 时，CSM 进程在本机运行，但 Kubernetes 读写操作会作用于该线上集群。
- `*.fan.b2.sz.w7.com` 支持泛域名解析。已验证 `csm-local-test.fan.b2.sz.w7.com` 解析到 `218.23.2.48`；该域名指向 218 集群，不指向本机的 `make run` 服务。
- 本地服务使用 `http://172.16.1.3:8082`；OIDC 使用 218 集群的 `default` 客户端。浏览器需能访问 `172.16.1.3`。
- SSH 使用 218 集群的 `higress-system/gateway`；公网入口通过 `*.fan.b2.sz.w7.com` 解析到 `218.23.2.48`。
- 线上 CSM 已绑定 `https://csm.fan.b2.sz.w7.com`，SSH 使用相同域名加主机详情中的端口。

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

`make run` 设置 `MOCK_PAY=true`：下单不会向 W7 Cloud 发起真实支付，但会按模拟支付结果向 **218 线上集群** 应用资源。测试购买或其他写操作前，确认目标命名空间和资源允许修改。

## 验证 SSH

在 CSM 主机详情添加本机公钥；主机状态为 Ready 后，使用详情显示的端口连接。218 集群的 `csm-admin/test` 已用以下命令验证：

```sh
ssh -F /dev/null -i "$HOME/.ssh/csm-218" -p 30000 root@test-csm-admin.fan.b2.sz.w7.com
```

该机器的系统 SSH 配置文件权限异常，因此示例使用 `-F /dev/null` 跳过系统配置；其他机器可省略。SSH 公钥存于 `~/.ssh/csm-218.pub`，私钥只留在本机。
