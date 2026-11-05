# CSM 项目约定

## 统一发布命令

在项目根目录设置 `ZPK_IDC_PASSWORD` 后执行 `make full VERSION=x.y.z`，依次构建 UI、打包 Helm、通过 ko 构建并推送镜像、推送 ZPK。单独执行阶段可用 `make build-ui`、`make build-helm VERSION=x.y.z`、`make build-image VERSION=x.y.z`、`make push-zpk VERSION=x.y.z`。`full` 仅发布制品，不部署 Helm；集群部署仍按下方发布流程执行。

- 本系统的界面、文档、日志和新增内部名称统一使用 CSM，不使用其他产品名称。
- 第三方接口要求的历史字段名仅保留在协议边界，不展示给用户；修改字段前先确认对方接口兼容。
- 本地测试前阅读 [local-test.md](local-test.md)：`make run` 在本机运行，但使用 `~/.kube/218.config` 连接 218 线上集群。
- 每次将任务改动合并回主分支（如 `dev-v1`）并解决冲突后，检查合并结果并在主分支提交；`$worktree-merge` 本身仍不自动提交，提交作为合并后的独立步骤执行。

## 集群部署流程

- 用户要求部署到集群时，在项目根目录、同一个 shell 中执行以下命令，依次构建并推送镜像、部署 Helm：

  ```sh
  export KO_IMAGE_TAG=1.0.6 && make ko-build && make deployHelm
  ```

- `KO_IMAGE_TAG` 是动态版本号，须替换为本次发布版本；`1.0.6` 仅为示例，不得固定使用。
- 构建与部署必须使用同一个 `KO_IMAGE_TAG`；保留 `&&`，构建失败时不得继续部署。

## 界面与响应式布局

- CSM 界面沿用 w7panel-ckm 与 w7panel-ui 的 Arco 视觉风格：浅色导航、蓝色主操作、面包屑和紧凑的表单及表格。
- 页面须适配手机和桌面；在 375px 手机宽度下，页面整体不得横向溢出，导航文字不得被压缩或截成单字。
- 手机端表单使用单列布局；所有列表每条记录使用多行布局展示，不横向滚动，操作入口须保持可访问。
- 修改界面后检查 375px 手机宽度和桌面宽度的布局，并验证主要导航及表单操作。

## Worktree 合并后清理

- 任务 worktree 的改动合并到主分支并完成提交后，确认主项目当前提交已包含全部任务改动，再在主项目目录执行 `$worktree-end 任务名` 删除该 worktree 和任务分支。如仍有未合并差异，停止清理；不得用 `--force` 跳过检查。
