<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api, items } from './api'
import { finishLogin, startLogin } from './auth'

const tabs = [
  ['hosts', '主机'], ['purchase', '购买'], ['templates', '系统模板'],
  ['preinstall', '预装'], ['upgrade', '升级'], ['orders', '订单'],
  ['disks', '数据盘'], ['keys', 'SSH 公钥'],
  ['apps', '初始化配置'], ['versions', '升级版本'], ['policies', '升级策略'],
]
const section = ref(location.hash.slice(1) || 'hosts')
const loading = ref(false)
const loggedIn = ref(Boolean(localStorage.getItem('csm-token')))
const error = ref('')
const notice = ref('')
const rows = ref([])
const selected = ref(null)
const templates = ref([])
const form = ref({ kind: 'csm', action: 'buy', name: '', templateName: '', cpu: 2, memoryGi: 4, storageGi: 40, bandwidthMbps: 5, quantity: 1, unit: 'month' })
const key = ref({ name: '', publicKey: '' })
const mountTarget = ref('')
const hostKey = ref('')
const versions = ref([])
const upgradeTarget = ref('')
const payment = ref(null)
const draft = ref({ name: '', shell: '', order: 0, versionName: '', templateName: '', version: '', image: '', hash: '' })
const terminalOpen = ref(false)
const terminalText = ref('')
let terminalSocket
function openTerminal() {
  const name = nameOf(selected.value)
  const token = localStorage.getItem('csm-token')
  if (!token) { error.value = '请先登录后打开终端'; return }
  terminalSocket?.close()
  const base = window.$wujie?.props?.url || window.location.origin
  const url = new URL(`/csm-api/v1/csms/${encodeURIComponent(name)}/terminal`, base)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  terminalText.value = ''; terminalOpen.value = true
  terminalSocket = new WebSocket(url, ['v4.channel.k8s.io', `bearer.${token}`])
  terminalSocket.binaryType = 'arraybuffer'
  terminalSocket.onmessage = event => {
    const data = new Uint8Array(event.data)
    if (data[0] === 1 || data[0] === 2) terminalText.value += new TextDecoder().decode(data.slice(1))
    if (data[0] === 3) terminalText.value += '\n' + new TextDecoder().decode(data.slice(1))
  }
  terminalSocket.onerror = () => { error.value = '终端连接失败'; terminalOpen.value = false }
  terminalSocket.onclose = () => { terminalText.value += '\n[连接已关闭]' }
}
function approveUpgrade() { if (window.confirm('确认对这台主机执行所选版本？')) run(`/csms/${nameOf(selected.value)}/upgrade`, { versionName: upgradeTarget.value }) }
function terminalKey(event) {
  if (terminalSocket?.readyState !== WebSocket.OPEN) return
  const key = event.key === 'Enter' ? '\r' : event.key === 'Backspace' ? '\x7f' : event.key.length === 1 ? event.key : ''
  if (!key) return
  event.preventDefault()
  const data = new TextEncoder().encode(key)
  terminalSocket.send(new Uint8Array([0, ...data]))
}
const title = computed(() => tabs.find(([id]) => id === section.value)?.[1] || '主机')
const resource = { hosts: 'csms', templates: 'system-templates', preinstall: 'preinstall-executions', upgrade: 'upgrade-executions', orders: 'orders', disks: 'data-disks', keys: 'ssh-keys', apps: 'preinstall-applications', versions: 'upgrade-versions', policies: 'upgrade-policies' }

async function login() { try { await startLogin() } catch (e) { error.value = e.message } }
function go(id) { section.value = id; location.hash = id; selected.value = null }
async function load() {
  if (!loggedIn.value) return
  error.value = ''; loading.value = true
  try {
    rows.value = section.value === 'purchase' ? [] : items(await api('/' + (resource[section.value] || 'csms')))
    if (section.value === 'purchase') templates.value = items(await api('/system-templates'))
    if (section.value === 'hosts') versions.value = items(await api('/upgrade-versions'))
  } catch (e) { error.value = e.message; rows.value = []; if (e.status === 401) loggedIn.value = false }
  finally { loading.value = false }
}
async function detail(kind, row) {
  const name = row.metadata?.name || row.name
  try { selected.value = await api(`/${kind}/${encodeURIComponent(name)}`) }
  catch (e) { error.value = e.message }
}
async function run(path, body) {
  error.value = ''; notice.value = ''; payment.value = null
  try {
    const result = await api(path, { method: 'POST', body: JSON.stringify(body || {}) })
    notice.value = result?.orderSn ? `订单 ${result.orderSn} 已创建，支付确认后生效。` : '操作已提交。'
    if (result?.ticket) payment.value = result
    await load()
    return result
  } catch (e) { error.value = e.message }
}
async function remove(path) {
  if (!confirm('确定删除？')) return
  try { await api(path, { method: 'DELETE' }); selected.value = null; await load() }
  catch (e) { error.value = e.message }
}
async function buy() {
  const request = { ...form.value }
  if (request.kind === 'dataDisk') { request.templateName = ''; request.cpu = 0; request.memoryGi = 0; request.bandwidthMbps = 0 }
  if (request.action === 'renew') { request.templateName = ''; request.cpu = 0; request.memoryGi = 0; request.storageGi = 0; request.bandwidthMbps = 0 }
  if (request.action === 'expand') { request.templateName = ''; request.quantity = 0; request.unit = '' }
  await run('/orders', request)
}
async function createConfig() {
  const kind = resource[section.value]
  const spec = section.value === 'apps' ? { enabled: true, order: Number(draft.value.order), shell: draft.value.shell }
    : section.value === 'policies' ? { versionName: draft.value.versionName, enabled: true, order: Number(draft.value.order), shell: draft.value.shell }
    : { templateName: draft.value.templateName, version: draft.value.version, policies: draft.value.policies?.split(',').map(s => s.trim()).filter(Boolean) || [] }
  await run(`/${kind}`, { metadata: { name: draft.value.name }, spec })
}
function nameOf(row) { return row.metadata?.name || row.name || row.orderSn || '—' }
function phaseOf(row) { return row.status?.phase || row.status || row.spec?.order?.status || '—' }
watch(section, load)
onMounted(async () => { try { if (await finishLogin()) loggedIn.value = true; if (loggedIn.value) await load() } catch (e) { error.value = e.message } })
</script>

<template>
  <div class="shell">
    <aside class="rail">
      <div class="brand"><span class="brand-mark">CSM</span><small>CONTAINER HOSTS</small></div>
      <nav aria-label="主导航"><button v-for="[id, label] in tabs" :key="id" :class="{ active: section === id }" @click="go(id)">{{ label }}</button></nav>
      <div class="rail-foot">W7PANEL · COMPUTE</div>
    </aside>
    <main>
      <header><div><p class="eyebrow">容器云主机 / {{ title }}</p><h1>{{ title }}</h1></div><button v-if="loggedIn" class="light" @click="load">刷新</button></header>
      <p v-if="error" class="alert" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }} <a v-if="payment?.ticket" :href="`https://ip.w7.cc/pay/${encodeURIComponent(payment.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">前往 W7 云支付 ↗</a></p>
      <section v-if="!loggedIn" class="panel"><h2>登录 CSM</h2><p class="hint">使用 W7Panel 账户授权后管理云主机。</p><button class="primary" @click="login">登录</button></section>
      <template v-else><section v-if="section === 'purchase'" class="panel form-panel">
        <p class="hint">创建订单后，资源将在云支付确认时生效。</p>
        <div class="grid">
          <label>资源类型<select v-model="form.kind"><option value="csm">云主机</option><option value="dataDisk">独立数据盘</option></select></label>
          <label>购买方式<select v-model="form.action"><option value="buy">购买</option><option value="renew">续费</option><option value="expand">扩容</option></select></label>
          <label>名称<input v-model.trim="form.name" placeholder="例如 my-host" /></label>
          <label v-if="form.kind === 'csm' && form.action === 'buy'">系统模板<select v-model="form.templateName"><option value="">选择模板</option><option v-for="item in templates" :key="nameOf(item)" :value="nameOf(item)">{{ nameOf(item) }}</option></select></label>
          <template v-if="form.action !== 'renew'">
            <label v-if="form.kind === 'csm'">CPU 核数<input v-model.number="form.cpu" type="number" min="1" /></label>
            <label v-if="form.kind === 'csm'">内存 GiB<input v-model.number="form.memoryGi" type="number" min="1" /></label>
            <label>存储 GiB<input v-model.number="form.storageGi" type="number" min="1" /></label>
            <label v-if="form.kind === 'csm'">带宽 Mbps<input v-model.number="form.bandwidthMbps" type="number" min="1" /></label>
          </template>
          <template v-if="form.action !== 'expand'"><label>时长<input v-model.number="form.quantity" type="number" min="1" /></label><label>单位<select v-model="form.unit"><option value="month">月</option><option value="year">年</option></select></label></template>
        </div><button class="primary" @click="buy">创建订单</button>
      </section>
      <section v-else-if="['apps','versions','policies'].includes(section)" class="panel form-panel">
        <h2>新增{{ title }}</h2><div class="grid">
          <label>名称<input v-model.trim="draft.name" placeholder="唯一名称" /></label>
          <label v-if="section === 'versions'">目标模板名<input v-model.trim="draft.templateName" /></label>
          <label v-if="section === 'versions'">版本<input v-model.trim="draft.version" /></label>
          <label v-if="section === 'versions'">策略名称（逗号分隔）<input v-model="draft.policies" /></label>
          <label v-if="section === 'policies'">所属版本<input v-model.trim="draft.versionName" /></label>
          <label v-if="section !== 'versions'">执行顺序<input v-model.number="draft.order" type="number" /></label>
          <label v-if="section !== 'versions'">Shell 脚本<textarea v-model="draft.shell" /></label>
        </div><button class="primary" @click="createConfig">保存配置</button>
      </section>
      <section v-else-if="section === 'keys'" class="panel">
        <form class="key-form" @submit.prevent="run('/ssh-keys', key)"><label>名称<input v-model.trim="key.name" required /></label><label>SSH 公钥<textarea v-model.trim="key.publicKey" required placeholder="ssh-ed25519 ..." /></label><button class="primary">添加公钥</button></form>
      </section>
      <section v-if="section !== 'purchase'" class="panel">
        <div class="panel-head"><span>{{ loading ? '正在加载…' : `${rows.length} 条记录` }}</span><button v-if="section === 'hosts' || section === 'disks'" class="text" @click="go('purchase')">购买资源 →</button></div>
        <div v-if="!loading && rows.length === 0" class="empty">暂无记录。{{ section === 'hosts' ? '可从购买页面创建第一台主机。' : '' }}</div>
        <div v-else class="table-wrap"><table><thead><tr><th>名称 / 订单号</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="nameOf(row)"><td class="mono">{{ nameOf(row) }}</td><td><span class="status">{{ phaseOf(row) }}</span></td><td class="actions">
          <button v-if="['hosts','templates','disks'].includes(section)" class="text" @click="detail(resource[section], row)">详情</button>
          <button v-if="section === 'hosts'" class="text" @click="run(`/csms/${nameOf(row)}/preinstall`)">预装</button>
          <button v-if="section === 'hosts'" class="text" @click="detail('csms', row)">升级</button>
          <button v-if="section === 'hosts'" class="text" @click="detail('csms', row)">终端入口</button>
          <a v-if="section === 'orders' && row.ticket && row.status !== 'paid'" class="text" :href="`https://ip.w7.cc/pay/${encodeURIComponent(row.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">支付 ↗</a>
          <button v-if="section === 'orders' && row.status !== 'paid'" class="text" @click="run(`/orders/${row.orderSn}/refresh`)">刷新支付</button>
          <button v-if="['apps','versions','policies'].includes(section)" class="text danger" @click="remove(`/${resource[section]}/${nameOf(row)}`)">删除</button>
          <button v-if="section === 'keys'" class="text danger" @click="remove(`/ssh-keys/${nameOf(row)}`)">删除</button>
          <button v-if="section === 'disks'" class="text danger" @click="remove(`/data-disks/${nameOf(row)}`)">删除</button>
        </td></tr></tbody></table></div>
      </section>
      <section v-if="selected" class="panel detail"><div class="panel-head"><h2>{{ nameOf(selected) }}</h2><button class="text" @click="selected = null">关闭</button></div>
        <dl><dt>状态</dt><dd>{{ phaseOf(selected) }}</dd><dt>系统模板</dt><dd>{{ selected.spec?.templateName || '—' }}</dd><dt>SSH 端口</dt><dd>{{ selected.status?.sshPort || '—' }}</dd></dl>
        <div v-if="section === 'hosts'" class="upgrade-form"><label>升级目标版本<select v-model="upgradeTarget"><option value="">选择版本</option><option v-for="version in versions" :key="nameOf(version)" :value="nameOf(version)">{{ nameOf(version) }}</option></select></label><button class="light" :disabled="!upgradeTarget" @click="approveUpgrade">确认升级</button></div>
        <div v-if="section === 'hosts'" class="actions"><button class="light" @click="openTerminal">打开网页终端</button><button class="light" @click="go('keys')">管理 SSH 公钥</button></div><div v-if="section === 'hosts'" class="actions"><input v-model.trim="hostKey" placeholder="粘贴 SSH 公钥" /><button class="light" @click="run(`/csms/${nameOf(selected)}/ssh-keys`, { publicKey: hostKey })">添加到主机</button></div>
        <div v-if="section === 'disks'" class="actions"><input v-model.trim="mountTarget" placeholder="目标主机名称" /><button class="light" @click="run(`/data-disks/${nameOf(selected)}/mount`, { csmName: mountTarget })">挂载</button><button class="light" @click="run(`/data-disks/${nameOf(selected)}/unmount`)">卸载</button></div>
      </section>
      <section v-if="terminalOpen" class="panel"><div class="panel-head"><h2>网页终端</h2><button class="text" @click="terminalSocket?.close(); terminalOpen = false">关闭</button></div><pre class="terminal" tabindex="0" @keydown="terminalKey">{{ terminalText || '连接中…' }}</pre><p class="hint">点击终端区域后输入命令。</p></section>
      </template>
    </main>
  </div>
</template>

<style>
:root{font-family:Inter,"Noto Sans SC",system-ui,sans-serif;color:#20304c;background:#f3f6fa}*{box-sizing:border-box}body{margin:0}button,input,select,textarea{font:inherit}button{cursor:pointer}button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid #6db6ee;outline-offset:2px}.shell{display:flex;min-height:100vh}.rail{width:220px;background:#142c4d;color:#dce8f6;padding:28px 16px;display:flex;flex-direction:column}.brand{padding:0 12px 36px;display:flex;flex-direction:column;gap:5px}.brand-mark{font-family:Georgia,serif;font-size:31px;letter-spacing:.09em;color:#fff}.brand small,.rail-foot{font-size:10px;letter-spacing:.18em;color:#91acc8}.rail nav{display:grid;gap:4px}.rail nav button{border:0;background:transparent;text-align:left;color:inherit;padding:12px 14px;border-radius:8px}.rail nav button:hover,.rail nav button.active{background:#25486c;color:white}.rail nav button.active{box-shadow:inset 3px 0 #60c6d4}.rail-foot{margin-top:auto;padding:22px 12px 0}main{flex:1;min-width:0;padding:34px clamp(20px,4vw,64px)}header,.panel-head{display:flex;align-items:center;justify-content:space-between;gap:16px}header{margin-bottom:28px}.eyebrow{font-size:12px;letter-spacing:.12em;color:#60809b;margin:0 0 8px}h1{font:normal 38px Georgia,serif;margin:0;color:#173254}h2{font-size:18px;margin:0}.panel{background:#fff;border:1px solid #dfe7ef;border-radius:10px;padding:22px;margin-bottom:18px;box-shadow:0 8px 28px #2343650a}.panel-head{font-size:13px;color:#6d7d91;margin-bottom:16px}.light,.primary{border:1px solid #bdcedc;border-radius:7px;background:white;padding:9px 16px;color:#244a70}.primary{background:#236488;border-color:#236488;color:white}.text{border:0;background:transparent;color:#176f9a;padding:5px}.danger{color:#ac4152}.alert,.notice{padding:12px 16px;border-radius:7px}.alert{background:#fff0f0;color:#a33}.notice{background:#e7f8f7;color:#17685e}.hint{margin:0 0 24px;color:#63768b}.grid{display:grid;grid-template-columns:repeat(2,minmax(190px,1fr));gap:18px;max-width:760px;margin-bottom:24px}label{display:flex;flex-direction:column;gap:7px;color:#46617b;font-size:13px}input,select,textarea{border:1px solid #cad7e2;border-radius:6px;padding:10px;color:#20304c;background:#fff;min-width:0}textarea{min-height:85px}.key-form{display:grid;gap:16px;max-width:620px}.key-form .primary{justify-self:start}.table-wrap{overflow-x:auto}table{width:100%;border-collapse:collapse;text-align:left}th{font-size:12px;color:#67809b;font-weight:600;border-bottom:1px solid #e3eaf1;padding:10px}td{border-bottom:1px solid #edf1f5;padding:13px 10px}tr:last-child td{border:0}.mono{font-family:ui-monospace,SFMono-Regular,monospace}.status{display:inline-block;background:#ecf4f7;border-radius:20px;padding:4px 9px;font-size:12px}.actions{display:flex;gap:9px;flex-wrap:wrap;align-items:center}.empty{padding:28px 8px;color:#8293a5}.detail dl{display:grid;grid-template-columns:120px 1fr;gap:12px}.detail dt{color:#8091a2}.detail dd{margin:0}.detail .actions{margin-top:20px}.upgrade-form{display:flex;align-items:end;gap:12px;margin-top:22px;flex-wrap:wrap}.upgrade-form label{min-width:220px}.upgrade-form button:disabled{opacity:.5;cursor:not-allowed}.terminal{background:#0b1c30;color:#d8f7e9;min-height:300px;padding:18px;white-space:pre-wrap;overflow:auto;outline:none;font:14px/1.6 ui-monospace,monospace}@media(max-width:700px){.shell{display:block}.rail{width:auto;padding:14px}.brand{padding:0 4px 12px}.rail nav{display:flex;overflow-x:auto}.rail nav button{white-space:nowrap}.rail-foot{display:none}main{padding:20px}.grid{grid-template-columns:1fr}h1{font-size:30px}}
</style>
