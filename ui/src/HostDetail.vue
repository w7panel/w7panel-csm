<script setup>
import { computed, ref } from 'vue'

const props = defineProps({ host: { type: Object, required: true }, versions: { type: Array, required: true }, sshHost: { type: String, default: '' }, loading: Boolean })
const emit = defineEmits(['refresh', 'preinstall', 'upgrade', 'terminal', 'add-key', 'add-jump-key'])
const upgradeTarget = ref('')
const publicKey = ref('')
const jumpKey = ref('')
const copyMessage = ref('')
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
        <div class="host-identity"><h1>{{ name(host) }}</h1><span class="status">{{ display(host.status?.phase) }}</span></div>
        <p>{{ host.metadata?.namespace || '默认命名空间' }} <span v-if="host.status?.message">· {{ host.status.message }}</span></p>
      </div>
      <div class="host-heading-actions"><button class="light" :disabled="loading" @click="emit('refresh')">{{ loading ? '刷新中…' : '刷新状态' }}</button><button class="primary" @click="emit('terminal')">打开终端</button></div>
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

    <div class="host-columns">
      <section class="host-section" aria-labelledby="host-connect-heading">
        <h2 id="host-connect-heading">连接与公钥</h2>
        <dl class="host-facts"><div><dt>主机标识</dt><dd class="mono">{{ display(host.status?.sshHost) }}</dd></div><div><dt>网关地址</dt><dd class="mono">{{ sshHost || '—' }}</dd></div><div><dt>SSH 端口</dt><dd>{{ display(host.status?.sshPort) }}</dd></div></dl>
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
.host-detail-page{width:100%;margin-bottom:24px}.host-detail-page button,.host-detail-page select{min-height:44px}.host-heading{display:flex;justify-content:space-between;align-items:flex-end;gap:20px;padding:8px 0 24px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}
.host-identity{display:flex;align-items:center;flex-wrap:wrap;gap:12px}.host-identity h1{margin:0;overflow-wrap:anywhere}.host-heading p{margin:8px 0 0;color:var(--color-text-2,#4e5969);overflow-wrap:anywhere}.host-heading-actions{display:flex;gap:8px;flex-wrap:wrap}
.host-section{min-width:0;margin-top:22px;padding:20px;background:var(--color-bg-2,#fff)}.host-section h2{margin-bottom:18px}.host-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.host-metrics>div{min-width:0;padding:16px;background:var(--color-fill-2,#f2f3f5)}.host-metrics span{display:block;color:var(--color-text-2,#4e5969)}.host-metrics strong{display:block;margin-top:8px;font-size:22px;overflow-wrap:anywhere}.host-metrics small{font-size:13px;font-weight:400}.host-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;margin:16px 0 0}.host-facts>div{display:grid;grid-template-columns:110px minmax(0,1fr);gap:12px;padding:12px 0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.host-facts dt{color:var(--color-text-2,#4e5969)}.host-facts dd{min-width:0;margin:0;overflow-wrap:anywhere}
.host-columns{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px}.host-command{margin:16px 0;padding:12px;background:var(--color-fill-2,#f2f3f5);white-space:pre-wrap;overflow-wrap:anywhere}.host-key-form{margin-top:20px}.host-key-form button{margin-top:12px}.host-section-note{color:var(--color-text-2,#4e5969)}.host-upgrade{display:flex;align-items:end;gap:12px;margin-top:24px}.host-upgrade label{flex:1;min-width:0}.host-upgrade button{flex:none}
@media(max-width:800px){.host-columns{grid-template-columns:1fr}.host-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:600px){.host-heading{align-items:stretch;flex-direction:column}.host-heading-actions>*{flex:1}.host-section{padding:16px}.host-facts{grid-template-columns:1fr}.host-facts>div{grid-template-columns:100px minmax(0,1fr)}.host-upgrade{align-items:stretch;flex-direction:column}.host-upgrade button{align-self:flex-start}}
</style>
