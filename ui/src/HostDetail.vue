<script setup>
import { computed, ref } from 'vue'
import { api } from './api'

const props = defineProps({ host: { type: Object, required: true }, runtime: { type: Object, default: null }, runtimeError: { type: String, default: '' }, cloudInitExecution: { type: Object, default: null }, cloudInitError: { type: String, default: '' }, versions: { type: Array, required: true }, sshHost: { type: String, default: '' }, loading: Boolean })
const emit = defineEmits(['refresh', 'preinstall', 'upgrade', 'terminal', 'add-key', 'add-jump-key', 'delete'])
const upgradeTarget = ref('')
const publicKey = ref('')
const jumpKey = ref('')
const copyMessage = ref('')
const podViewer = ref(null)
async function openPodViewer(pod, kind) {
  podViewer.value = { name: pod.name, kind, loading: true, error: '', log: '', events: [] }
  const viewer = podViewer.value
  try {
    const namespace = props.host.metadata.namespace
    const path = `/csms/${encodeURIComponent(props.host.metadata.name)}/pods/${encodeURIComponent(pod.name)}/${kind}?namespace=${encodeURIComponent(namespace)}`
    const data = await api(path)
    if (podViewer.value === viewer) Object.assign(viewer, kind === 'logs' ? { log: data.log } : { events: data.events || [] })
  } catch (e) { if (podViewer.value === viewer) viewer.error = e.message }
  finally { if (podViewer.value === viewer) viewer.loading = false }
}
const sshCommand = computed(() => `ssh -p ${props.host.status?.sshPort} -i ~/.ssh/csm-218 root+${props.host.status?.sshHost}@${props.sshHost}`)
const sshConfig = computed(() => `Host csm-${props.host.metadata?.name}
  HostName ${props.sshHost}
  Port ${props.host.status?.sshPort}
  User root+${props.host.status?.sshHost}
  IdentityFile ~/.ssh/csm-218
  IdentitiesOnly yes`)
async function copyConnection(text) {
  try { await navigator.clipboard.writeText(text); copyMessage.value = '已复制' }
  catch { copyMessage.value = '无法自动复制，请选中上方内容手动复制' }
}
const name = row => row?.metadata?.name || '—'
const display = value => value === undefined || value === null || value === '' ? '—' : value
const dateText = value => { const date = new Date(value); return value && !Number.isNaN(date.getTime()) ? date.toLocaleString('zh-CN', { hour12: false }) : '—' }
function upgrade() {
  if (upgradeTarget.value && window.confirm(`确认将 ${name(props.host)} 升级到 ${upgradeTarget.value}？`)) emit('upgrade', upgradeTarget.value)
}
function addKey() {
  if (!publicKey.value.trim()) return
  emit('add-key', publicKey.value.trim(), () => { publicKey.value = '' })
}
function addJumpKey() {
  if (!jumpKey.value.trim()) return
  emit('add-jump-key', jumpKey.value.trim(), () => { jumpKey.value = '' })
}
</script>

<template>
  <div class="host-detail-page">
    <section class="host-heading">
      <div class="host-heading-main">
        <strong class="host-name">{{ name(host) }}</strong><span class="status">{{ display(host.status?.phase) }}</span>
        <p>{{ host.metadata?.namespace || '默认命名空间' }} <span v-if="host.status?.message">· {{ host.status.message }}</span></p>
      </div>
      <div class="host-heading-actions"><button class="light" :disabled="loading" @click="emit('refresh')">{{ loading ? '刷新中…' : '刷新状态' }}</button><button class="primary" @click="emit('terminal')">打开终端</button><button class="text danger" :disabled="loading" @click="emit('delete')">删除主机</button></div>
    </section>

    <section class="host-section" aria-labelledby="host-resource-heading">
      <h2 id="host-resource-heading">资源配置</h2>
      <div class="host-metrics">
        <div><span>CPU</span><strong>{{ display(host.spec?.cpu) }} <small>核</small></strong></div>
        <div><span>内存</span><strong>{{ display(host.spec?.memory) }}</strong></div>
        <div><span>系统盘</span><strong>{{ display(host.spec?.systemDiskGi) }} <small>GiB</small></strong></div>
        <div><span>带宽</span><strong>{{ display(host.spec?.bandwidthMbps) }} <small>Mbps</small></strong></div>
      </div>
      <dl class="host-facts"><div><dt>系统模板</dt><dd>{{ display(host.spec?.templateName) }}</dd></div><div><dt>系统盘 PVC</dt><dd>{{ display(host.status?.systemPVCName) }}</dd></div><div><dt>存储类</dt><dd>{{ display(host.spec?.storageClassName) }}</dd></div><div><dt>到期时间</dt><dd>{{ dateText(host.spec?.expireTime) }}</dd></div></dl>
    </section>

    <section class="host-section" aria-labelledby="host-runtime-heading">
      <h2 id="host-runtime-heading">运行状态</h2>
      <p v-if="runtimeError" class="host-section-note" role="alert">运行信息读取失败：{{ runtimeError }}</p>
      <p v-else-if="!runtime" class="host-section-note">正在读取运行信息…</p>
      <template v-else>
        <dl v-if="runtime.deployment" class="host-facts"><div><dt>Deployment</dt><dd class="mono">{{ runtime.deployment.name }}</dd></div><div><dt>就绪副本</dt><dd>{{ runtime.deployment.ready }} / {{ runtime.deployment.desired }}</dd></div><div><dt>已更新</dt><dd>{{ runtime.deployment.updated }}</dd></div><div><dt>可用副本</dt><dd>{{ runtime.deployment.available }}</dd></div></dl>
        <p v-else class="host-section-note">Deployment 尚未创建。</p>
        <h3 class="host-pods-heading">Pod</h3>
        <div v-if="runtime.pods?.length" class="host-pods">
          <article v-for="pod in runtime.pods" :key="pod.name" class="host-pod">
            <div class="host-pod-heading"><strong class="mono">{{ pod.name }}</strong><span class="status">{{ display(pod.phase) }}</span></div>
            <dl class="host-pod-facts"><div><dt>Ready</dt><dd>{{ pod.ready }} / {{ pod.total }}</dd></div><div><dt>IP</dt><dd class="mono">{{ display(pod.ip) }}</dd></div><div><dt>节点</dt><dd class="mono">{{ display(pod.node) }}</dd></div><div><dt>重启</dt><dd>{{ pod.restarts }}</dd></div></dl>
            <div class="host-pod-actions"><button class="light" @click="openPodViewer(pod, 'logs')">日志</button><button class="light" @click="openPodViewer(pod, 'events')">事件</button></div>
          </article>
        </div>
        <p v-else class="host-section-note">暂无关联 Pod。</p>
      </template>
    </section>

    <a-modal :visible="!!podViewer" :title="`${podViewer?.name || 'Pod'} · ${podViewer?.kind === 'logs' ? '日志' : '事件'}`" width="min(900px, calc(100vw - 24px))" :footer="false" :unmount-on-close="true" @cancel="podViewer = null">
      <p v-if="podViewer?.loading" class="host-section-note">正在读取…</p>
      <p v-else-if="podViewer?.error" class="host-section-note" role="alert">{{ podViewer.error }}</p>
      <pre v-else-if="podViewer?.kind === 'logs'" class="host-command mono">{{ podViewer.log || '暂无日志' }}</pre>
      <div v-else-if="podViewer?.events?.length" class="host-events"><article v-for="event in podViewer.events" :key="event.metadata?.uid || event.metadata?.name"><strong>{{ event.type || '事件' }} · {{ event.reason || '—' }}</strong><p>{{ event.message || '—' }}</p><small>{{ dateText(event.lastTimestamp || event.eventTime || event.metadata?.creationTimestamp) }} · {{ event.count || 1 }} 次</small></article></div>
      <p v-else class="host-section-note">暂无事件。</p>
    </a-modal>

    <section v-if="host.status?.initMode === 'cloudInit'" class="host-section" aria-labelledby="host-cloud-init-heading">
      <h2 id="host-cloud-init-heading">cloud-init 执行日志</h2>
      <p v-if="cloudInitError" class="host-section-note" role="alert">日志读取失败：{{ cloudInitError }}</p>
      <template v-else-if="cloudInitExecution">
        <p class="host-section-note">状态：{{ display(cloudInitExecution.status?.phase) }}</p>
        <p v-if="cloudInitExecution.status?.message" class="host-section-note" role="alert">{{ cloudInitExecution.status.message }}</p>
        <pre v-if="cloudInitExecution.status?.log" class="host-command mono">{{ cloudInitExecution.status.log }}</pre>
        <p v-else class="host-section-note">暂无执行日志；cloud-init 结束后刷新状态查看。</p>
      </template>
      <p v-else class="host-section-note">暂无执行记录。</p>
    </section>

    <div class="host-columns">
      <section class="host-section" aria-labelledby="host-connect-heading">
        <h2 id="host-connect-heading">连接与公钥</h2>
        <dl class="host-facts"><div><dt>主机标识</dt><dd class="mono">{{ display(host.status?.sshHost) }}</dd></div><div><dt>网关地址</dt><dd class="mono">{{ sshHost || '—' }}</dd></div><div><dt>SSH 端口</dt><dd>{{ display(host.status?.sshPort) }}</dd></div></dl>
        <details class="host-connection-guide"><summary>SSH 连接设置</summary>
        <template v-if="host.status?.sshHost">
          <p class="host-section-note">1. 在自己的电脑上准备 SSH 密钥；已有 ~/.ssh/csm-218 时跳过生成，避免覆盖私钥。</p>
          <pre class="host-command mono">ssh-keygen -t ed25519 -f ~/.ssh/csm-218 -C csm-218
cat ~/.ssh/csm-218.pub</pre>
          <p class="host-section-note">2. 将公钥添加到下方。私钥只保存在自己的电脑上。</p>
          <form class="host-key-form" @submit.prevent="addJumpKey"><label>SSH 登录公钥<textarea v-model="jumpKey" required rows="3" placeholder="粘贴 ssh-ed25519 公钥" /></label><button class="light" :disabled="loading || !jumpKey.trim()">添加登录公钥</button></form>
          <template v-if="sshHost && host.status?.sshPort">
            <p class="host-section-note">3. 连接主机：</p>
            <pre class="host-command mono">{{ sshCommand }}</pre>
            <button class="light" @click="copyConnection(sshCommand)">复制连接命令</button>
            <p class="host-section-note">使用 VS Code Remote SSH 时，将以下配置添加到 ~/.ssh/config，再选择对应的 CSM 主机连接：</p>
            <pre class="host-command mono">{{ sshConfig }}</pre>
            <button class="light" @click="copyConnection(sshConfig)">复制 SSH 配置</button>
            <p role="status">{{ copyMessage }}</p>
          </template>
          <p v-else class="host-section-note">SSH 入口尚未就绪，请刷新主机状态后重试。</p>
        </template>
        <template v-else>
          <p v-if="sshHost && host.status?.sshPort" class="host-command mono">ssh -p {{ host.status.sshPort }} root@{{ sshHost }}</p>
          <form class="host-key-form" @submit.prevent="addKey"><label>SSH 登录公钥<textarea v-model="publicKey" required rows="3" placeholder="粘贴 ssh-ed25519 公钥" /></label><button class="light" :disabled="loading || !publicKey.trim()">添加登录公钥</button></form>
        </template>
        </details>
      </section>
      <section class="host-section" aria-labelledby="host-operation-heading">
        <h2 id="host-operation-heading">主机操作</h2>
        <p class="host-section-note">{{ host.status?.initMode === 'cloudInit' ? 'cloud-init 在首次启动时自动运行；升级前请选择目标版本并确认。' : '预装会提交初始化任务；升级前请选择目标版本并确认。' }}</p>
        <button v-if="host.status?.initMode !== 'cloudInit'" class="light" @click="emit('preinstall')">执行预装</button>
        <div class="host-upgrade"><label>升级目标版本<select v-model="upgradeTarget"><option value="">请选择版本</option><option v-for="item in versions" :key="name(item)" :value="name(item)">{{ item.spec?.version || name(item) }}</option></select></label><button class="primary" :disabled="!upgradeTarget" @click="upgrade">确认升级</button></div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.host-detail-page{width:100%;margin-bottom:24px}.host-detail-page button,.host-detail-page select{min-height:32px}.host-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:16px}
.host-name{margin-right:10px;font-size:20px;overflow-wrap:anywhere}.host-heading p{margin:4px 0 0;color:var(--color-text-2,#4e5969);overflow-wrap:anywhere}.host-heading-actions{display:flex;gap:8px;flex-wrap:wrap}
.host-section{min-width:0;margin-bottom:16px;padding:20px;background:var(--color-bg-2,#fff)}.host-section h2{margin-bottom:12px}.host-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.host-metrics>div{min-width:0;padding:4px 0}.host-metrics span{display:block;color:var(--color-text-2,#4e5969)}.host-metrics strong{display:block;margin-top:6px;font-size:18px;overflow-wrap:anywhere}.host-metrics small{font-size:13px;font-weight:400}.host-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;margin:16px 0 0}.host-facts>div{display:grid;grid-template-columns:110px minmax(0,1fr);gap:12px;padding:12px 0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.host-facts dt{color:var(--color-text-2,#4e5969)}.host-facts dd{min-width:0;margin:0;overflow-wrap:anywhere}
.host-columns{display:block}.host-connection-guide{margin-top:16px}.host-connection-guide summary{cursor:pointer;color:rgb(var(--primary-6,22,93,255))}.host-command{max-width:900px;margin:16px 0;padding:12px;background:var(--color-fill-2,#f2f3f5);white-space:pre-wrap;overflow-wrap:anywhere}.host-key-form{max-width:720px;margin-top:20px}.host-key-form button{margin-top:12px}.host-section-note{color:var(--color-text-2,#4e5969)}.host-upgrade{display:flex;align-items:end;gap:12px;max-width:560px;margin-top:24px}.host-upgrade label{flex:1;min-width:0}.host-upgrade button{flex:none}
.host-pods-heading{margin:20px 0 12px;font-size:16px}.host-pods{display:grid;gap:12px}.host-pod{min-width:0;padding:12px;border:1px solid var(--color-border-2,#e5e6eb)}.host-pod-heading{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.host-pod-heading strong{min-width:0;overflow-wrap:anywhere}.host-pod-facts{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin:12px 0 0}.host-pod-facts>div{min-width:0}.host-pod-facts dt{color:var(--color-text-2,#4e5969)}.host-pod-facts dd{margin:4px 0 0;overflow-wrap:anywhere}
.host-pod-actions{display:flex;gap:8px;margin-top:12px}.host-events{display:grid;gap:12px;max-height:60vh;overflow:auto}.host-events article{padding:12px;border:1px solid var(--color-border-2,#e5e6eb);overflow-wrap:anywhere}.host-events p{margin:8px 0}.host-events small{color:var(--color-text-2,#4e5969)}
@media(max-width:800px){.host-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:600px){.host-heading{align-items:stretch;flex-direction:column}.host-heading-actions>*{flex:1}.host-detail-page button,.host-detail-page select{min-height:44px}.host-section{padding:16px}.host-facts{grid-template-columns:1fr}.host-facts>div{grid-template-columns:100px minmax(0,1fr)}.host-pod-facts{grid-template-columns:repeat(2,minmax(0,1fr))}.host-upgrade{align-items:stretch;flex-direction:column}.host-upgrade button{align-self:flex-start}}
</style>
