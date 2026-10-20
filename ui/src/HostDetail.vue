<script setup>
import { ref } from 'vue'

const props = defineProps({ host: { type: Object, required: true }, versions: { type: Array, required: true }, sshHost: { type: String, default: '' }, loading: Boolean })
const emit = defineEmits(['back', 'refresh', 'preinstall', 'upgrade', 'terminal', 'add-key'])
const upgradeTarget = ref('')
const publicKey = ref('')
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
</script>

<template>
  <div class="host-detail-page">
    <section class="host-heading">
      <div class="host-heading-main">
        <button class="host-back" type="button" @click="emit('back')">← 返回主机列表</button>
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
        <dl class="host-facts"><div><dt>SSH 地址</dt><dd class="mono">{{ sshHost || '—' }}</dd></div><div><dt>SSH 端口</dt><dd>{{ display(host.status?.sshPort) }}</dd></div></dl>
        <p v-if="sshHost && host.status?.sshPort" class="host-command mono">ssh -p {{ host.status.sshPort }} root@{{ sshHost }}</p>
        <form class="host-key-form" @submit.prevent="addKey"><label>添加 SSH 公钥<textarea v-model="publicKey" required rows="3" placeholder="粘贴 ssh-ed25519 公钥" /></label><button class="light">添加到主机</button></form>
      </section>
      <section class="host-section" aria-labelledby="host-operation-heading">
        <h2 id="host-operation-heading">主机操作</h2>
        <p class="host-section-note">预装会提交初始化任务；升级前请选择目标版本并确认。</p>
        <button class="light" @click="emit('preinstall')">执行预装</button>
        <div class="host-upgrade"><label>升级目标版本<select v-model="upgradeTarget"><option value="">请选择版本</option><option v-for="item in versions" :key="name(item)" :value="name(item)">{{ item.spec?.version || name(item) }}</option></select></label><button class="primary" :disabled="!upgradeTarget" @click="upgrade">确认升级</button></div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.host-detail-page{max-width:1180px;margin:0 auto 24px}.host-detail-page button,.host-detail-page select{min-height:44px}.host-heading{display:flex;justify-content:space-between;align-items:flex-end;gap:20px;padding:8px 0 24px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}
.host-back{padding:0;border:0;background:transparent;color:rgb(var(--primary-6,22,93,255));text-align:left}.host-back:hover{text-decoration:underline}.host-identity{display:flex;align-items:center;flex-wrap:wrap;gap:12px}.host-identity h1{margin:0;overflow-wrap:anywhere}.host-heading p{margin:8px 0 0;color:var(--color-text-2,#4e5969);overflow-wrap:anywhere}.host-heading-actions{display:flex;gap:8px;flex-wrap:wrap}
.host-section{min-width:0;margin-top:22px;padding:20px;border:1px solid var(--color-border-2,#e5e6eb);background:var(--color-bg-2,#fff)}.host-section h2{margin-bottom:18px}.host-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.host-metrics>div{min-width:0;padding:16px;background:var(--color-fill-2,#f2f3f5)}.host-metrics span{display:block;color:var(--color-text-2,#4e5969)}.host-metrics strong{display:block;margin-top:8px;font-size:22px;overflow-wrap:anywhere}.host-metrics small{font-size:13px;font-weight:400}.host-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;margin:16px 0 0}.host-facts>div{display:grid;grid-template-columns:110px minmax(0,1fr);gap:12px;padding:12px 0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.host-facts dt{color:var(--color-text-2,#4e5969)}.host-facts dd{min-width:0;margin:0;overflow-wrap:anywhere}
.host-columns{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px}.host-command{margin:16px 0;padding:12px;background:var(--color-fill-2,#f2f3f5);overflow-wrap:anywhere}.host-key-form{margin-top:20px}.host-key-form button{margin-top:12px}.host-section-note{color:var(--color-text-2,#4e5969)}.host-upgrade{display:flex;align-items:end;gap:12px;margin-top:24px}.host-upgrade label{flex:1;min-width:0}.host-upgrade button{flex:none}
@media(max-width:800px){.host-columns{grid-template-columns:1fr}.host-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:600px){.host-heading{align-items:stretch;flex-direction:column}.host-heading-actions>*{flex:1}.host-section{padding:16px}.host-facts{grid-template-columns:1fr}.host-facts>div{grid-template-columns:100px minmax(0,1fr)}.host-upgrade{align-items:stretch;flex-direction:column}.host-upgrade button{align-self:flex-start}}
</style>
