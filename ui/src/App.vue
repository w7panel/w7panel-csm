<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api, items } from './api'
import { finishLogin, startLogin } from './auth'
import SystemTemplates from './SystemTemplates.vue'
import UpgradeVersions from './UpgradeVersions.vue'

const tabs = [
  ['hosts', '主机'], ['templates', '系统模板'],
  ['preinstall', '预装'], ['upgrade', '升级'], ['orders', '订单'],
  ['disks', '数据管理'],
  ['versions', '版本升级'], ['pricing', '费用设置'],
]
const section = ref(location.hash.slice(1) === 'policies' ? 'versions' : location.hash.slice(1) === 'apps' ? 'templates' : location.hash.slice(1) === 'keys' ? 'hosts' : location.hash.slice(1) || 'hosts')
const templateTab = ref(location.hash.slice(1) === 'apps' ? 'apps' : 'templates')
const mobileMenuOpen = ref(false)
const loading = ref(false)
const loggedIn = ref(Boolean(localStorage.getItem('csm-token')))
const error = ref('')
const notice = ref('')
const rows = ref([])
const selected = ref(null)
const templates = ref([])
const appRows = ref([])
const form = ref({ kind: 'csm', action: 'buy', name: '', templateName: '', cpu: 2, memoryGi: 4, storageGi: 40, bandwidthMbps: 5, quantity: 1, unit: 'month' })
const purchasePrices = ref(null)
const purchaseResources = [
  { field: 'cpu', label: 'CPU', unit: '核', price: 'cpuCents', max: 64 },
  { field: 'memoryGi', label: '内存', unit: 'GiB', price: 'memoryGiCents', max: 256 },
  { field: 'storageGi', label: '存储', unit: 'GiB', price: 'storageGiCents', max: 2048 },
  { field: 'bandwidthMbps', label: '带宽', unit: 'Mbps', price: 'bandwidthMbpsCents', max: 1000 },
]
const visiblePurchaseResources = computed(() => purchaseResources.filter(item => form.value.kind === 'csm' || item.field === 'storageGi'))
const purchasePeriods = [
  { quantity: 1, unit: 'month' }, { quantity: 2, unit: 'month' }, { quantity: 3, unit: 'month' },
  ...Array.from({ length: 10 }, (_, index) => ({ quantity: index + 1, unit: 'year' })),
]
const mountTarget = ref('')
const hostKey = ref('')
const sshHost = ref('')
const versions = ref([])
const upgradeTarget = ref('')
const payment = ref(null)
const pricingFields = [
  ['cpuCents', 'CPU', '核'], ['memoryGiCents', '内存', 'GiB'],
  ['storageGiCents', '存储', 'GiB'], ['bandwidthMbpsCents', '带宽', 'Mbps'],
]
const pricing = ref(Object.fromEntries(pricingFields.map(([field]) => [field, ''])))
const pricingCombinations = ref([])
const purchaseCombination = computed(() => purchasePrices.value?.combinations?.find(item =>
  form.value.kind === 'csm' && form.value.action === 'buy' &&
  item.quantity === form.value.quantity && item.unit === form.value.unit &&
  item.cpu === form.value.cpu && item.memoryGi === form.value.memoryGi &&
  item.storageGi === form.value.storageGi && item.bandwidthMbps === form.value.bandwidthMbps
))
const quoteAmountCents = ref(null)
const quoteError = ref('')
const quoteLoading = ref(false)
let quoteTimer
let quoteSequence = 0
const orderSearch = ref('')
const appliedOrderSearch = ref('')
const filteredOrders = computed(() => rows.value.filter(row => row.orderSn?.includes(appliedOrderSearch.value)))
const mountHosts = ref([])
const draft = ref({ name: '', shell: '', order: 0 })
const appDrawerOpen = ref(false)
const appSaving = ref(false)
const appError = ref('')
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
const title = computed(() => section.value === 'purchase' ? '购买' : tabs.find(([id]) => id === section.value)?.[1] || '主机')
const resource = { hosts: 'csms', templates: 'system-templates', preinstall: 'preinstall-executions', upgrade: 'upgrade-executions', orders: 'orders', disks: 'data-disks', apps: 'preinstall-applications', versions: 'upgrade-versions' }

async function login() { try { await startLogin() } catch (e) { error.value = e.message } }
function go(id) { if (id === 'apps') { templateTab.value = 'apps'; id = 'templates' } section.value = id; location.hash = id; selected.value = null; mobileMenuOpen.value = false }
function orderForDisk(action, disk) {
  form.value = { kind: 'dataDisk', action, name: disk?.metadata?.name || '', templateName: '', cpu: 0, memoryGi: 0, storageGi: action === 'expand' ? Number(disk.spec?.storageGi || 0) + 10 : action === 'buy' ? 10 : 0, bandwidthMbps: 0, quantity: 1, unit: 'month' }
  go('purchase')
}
function orderStatus(row) { return ({ pending: '待支付', paid: '已支付', applying: '生效中', failed: '失败' })[row.status] || row.status || '—' }
function diskStatus(row) { return ({ PendingPayment: '待支付', Provisioning: '创建中', Available: '可挂载', Mounting: '挂载中', Mounted: '已挂载', Unmounting: '卸载中', Detaching: '分离中', Deleting: '删除中', Expired: '已到期', Failed: '失败' })[row.status?.phase] || row.status?.phase || '—' }
function dateText(value) { const date = new Date(value); return value && !Number.isNaN(date.getTime()) ? date.toLocaleString('zh-CN', { hour12: false }) : '—' }
async function refreshOrder(row) {
  error.value = ''; notice.value = ''
  try {
    const updated = await api(`/orders/${encodeURIComponent(row.orderSn)}/refresh`, { method: 'POST' })
    notice.value = updated.status === 'paid' ? '订单已支付，资源正在生效。' : '尚未查询到支付成功，请稍后重试。'
    await load()
  } catch (e) { if (e.status === 409 && e.message === 'payment is not confirmed') notice.value = '尚未查询到支付成功，请稍后重试。'; else error.value = e.message }
}
async function deleteDisk(row) {
  if (!confirm(`确定删除数据盘 ${nameOf(row)}？盘内数据将永久丢失。`)) return
  try { await api(`/data-disks/${encodeURIComponent(nameOf(row))}`, { method: 'DELETE' }); selected.value = null; await load() }
  catch (e) { error.value = e.message }
}
function unmountDisk(row) {
  if (window.confirm('卸载会中断使用该盘的业务，确定继续？')) run(`/data-disks/${encodeURIComponent(nameOf(row))}/unmount`)
}
function orderRequest() {
  const request = { ...form.value }
  if (request.kind === 'dataDisk') { request.templateName = ''; request.cpu = 0; request.memoryGi = 0; request.bandwidthMbps = 0 }
  if (request.action === 'renew') { request.templateName = ''; request.cpu = 0; request.memoryGi = 0; request.storageGi = 0; request.bandwidthMbps = 0 }
  if (request.action === 'expand') { request.templateName = ''; request.quantity = 0; request.unit = '' }
  return request
}
function linePrice(item) {
  if (!purchasePrices.value || form.value.action !== 'buy') return '—'
  const months = form.value.quantity * (form.value.unit === 'year' ? 12 : 1)
  return `¥${((purchasePrices.value[item.price] * Number(form.value[item.field] || 0) * months) / 100).toFixed(2)}`
}
function scheduleQuote() {
  clearTimeout(quoteTimer)
  const sequence = ++quoteSequence
  quoteAmountCents.value = null
  quoteError.value = ''
  quoteLoading.value = false
  if (!loggedIn.value || section.value !== 'purchase' || !form.value.name || (form.value.kind === 'csm' && form.value.action === 'buy' && !form.value.templateName)) return
  quoteLoading.value = true
  quoteTimer = setTimeout(async () => {
    try {
      const quote = await api('/orders/quote', { method: 'POST', body: JSON.stringify(orderRequest()) })
      if (sequence === quoteSequence) quoteAmountCents.value = quote.amountCents
    } catch (e) { if (sequence === quoteSequence) quoteError.value = e.status === 503 ? '尚未设置资源单价，无法报价。' : e.message }
    finally { if (sequence === quoteSequence) quoteLoading.value = false }
  }, 350)
}
function addCombination() {
  pricingCombinations.value.push({ quantity: 1, unit: 'month', cpu: 2, memoryGi: 4, storageGi: 40, bandwidthMbps: 5, discountNew: 100, discountRenew: 100 })
}
async function savePricing() {
  const prices = {}
  for (const [field] of pricingFields) {
    const value = String(pricing.value[field]).trim()
    if (!/^\d+(\.\d{1,2})?$/.test(value)) { error.value = '单价须为非负金额，最多两位小数。'; return }
    prices[field] = Math.round(Number(value) * 100)
    if (!Number.isSafeInteger(prices[field])) { error.value = '单价超出范围。'; return }
  }
  const combinations = pricingCombinations.value.map(item => ({ ...item }))
  const seen = new Set()
  for (const item of combinations) {
    const validDuration = item.unit === 'month' ? item.quantity >= 1 && item.quantity <= 120 : item.unit === 'year' && item.quantity >= 1 && item.quantity <= 10
    if (!validDuration || !['quantity', 'cpu', 'memoryGi', 'storageGi', 'bandwidthMbps', 'discountNew', 'discountRenew'].every(field => Number.isInteger(item[field])) ||
      !['cpu', 'memoryGi', 'storageGi', 'bandwidthMbps'].every(field => item[field] >= 1 && item[field] <= 1000000) ||
      item.discountNew < 1 || item.discountNew > 100 || item.discountRenew < 1 || item.discountRenew > 100) {
      error.value = '组合规格和时长须为正整数，折扣须为 1–100%。'; return
    }
    const key = [item.quantity, item.unit, item.cpu, item.memoryGi, item.storageGi, item.bandwidthMbps].join(':')
    if (seen.has(key)) { error.value = '同一时长和规格不能重复配置。'; return }
    seen.add(key)
  }
  prices.combinations = combinations
  error.value = ''; notice.value = ''
  try {
    await api('/pricing', { method: 'PUT', body: JSON.stringify(prices) })
    notice.value = '费用配置已保存。'
    await load()
  } catch (e) { error.value = e.message }
}
async function load() {
  if (!loggedIn.value) return
  error.value = ''; loading.value = true
  try {
    rows.value = section.value === 'purchase' || section.value === 'pricing' ? [] : items(await api('/' + (resource[section.value] || 'csms')))
    if (section.value === 'orders') rows.value.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
    if (section.value === 'purchase') {
      templates.value = items(await api('/system-templates'))
      purchasePrices.value = await api('/pricing').catch(e => { if (e.status === 404) return null; throw e })
      scheduleQuote()
    }
    if (section.value === 'pricing') {
      const saved = await api('/pricing').catch(e => { if (e.status === 404) return null; throw e })
      pricing.value = Object.fromEntries(pricingFields.map(([field]) => [field, saved ? (saved[field] / 100).toFixed(2) : '']))
      pricingCombinations.value = saved?.combinations?.map(item => ({ ...item })) || []
    }
    if (section.value === 'hosts') versions.value = items(await api('/upgrade-versions'))
    if (section.value === 'templates') appRows.value = items(await api('/preinstall-applications'))
    if (section.value === 'disks') mountHosts.value = items(await api('/csms'))
  } catch (e) { error.value = e.message; rows.value = []; if (e.status === 401) loggedIn.value = false }
  finally { loading.value = false }
}
async function detail(kind, row) {
  const name = row.metadata?.name || row.name
  mountTarget.value = ''
  try { selected.value = await api(`/${kind}/${encodeURIComponent(name)}`) }
  catch (e) { error.value = e.message }
}
async function run(path, body) {
  error.value = ''; notice.value = ''; payment.value = null
  try {
    const result = await api(path, { method: 'POST', body: JSON.stringify(body || {}) })
    notice.value = result?.status === 'paid' ? `订单 ${result.orderSn} 已支付，资源正在生效。` : result?.orderSn ? `订单 ${result.orderSn} 已创建，支付确认后生效。` : '操作已提交。'
    if (result?.ticket) payment.value = result
    if (section.value === 'disks') selected.value = null
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
  await run('/orders', orderRequest())
}
function openAppDrawer() {
  draft.value = { name: '', shell: '', order: 0 }
  appError.value = ''
  appDrawerOpen.value = true
}
async function createConfig() {
  appSaving.value = true
  appError.value = ''
  try {
    await api('/preinstall-applications', { method: 'POST', body: JSON.stringify({ metadata: { name: draft.value.name }, spec: { enabled: true, order: Number(draft.value.order), shell: draft.value.shell } }) })
    appDrawerOpen.value = false
    notice.value = '初始化配置已创建。'
    await load()
  } catch (e) { appError.value = e.message }
  finally { appSaving.value = false }
}
function nameOf(row) { return row?.metadata?.name || row?.name || row?.orderSn || '—' }
function phaseOf(row) { return row.status?.phase || row.status || row.spec?.order?.status || '—' }
watch(section, load)
watch(form, scheduleQuote, { deep: true })
watch(() => form.value.kind, kind => {
  if (kind === 'csm') {
    if (!form.value.cpu) form.value.cpu = 2
    if (!form.value.memoryGi) form.value.memoryGi = 4
    if (!form.value.bandwidthMbps) form.value.bandwidthMbps = 5
  }
})
onMounted(async () => { try { if (await finishLogin()) loggedIn.value = true; sshHost.value = (await api('/auth/config')).sshHost || ''; if (loggedIn.value) await load() } catch (e) { error.value = e.message } })
</script>

<template>
  <div class="shell">
    <aside class="rail" aria-label="CSM 功能导航">
      <div class="brand">CSM 管理</div>
      <a-menu class="app-menu" :selected-keys="[section === 'purchase' ? (form.kind === 'dataDisk' ? 'disks' : 'hosts') : section]" @menu-item-click="go">
        <a-menu-item v-for="[id, label] in tabs" :key="id">{{ label }}</a-menu-item>
      </a-menu>
    </aside>
    <div class="mobile-bar"><button type="button" class="mobile-menu-trigger" aria-label="打开导航菜单" @click="mobileMenuOpen = true">☰</button><strong>CSM 管理</strong></div>
    <a-drawer v-model:visible="mobileMenuOpen" title="CSM 管理" placement="left" width="min(280px, 88vw)" :footer="false" :unmount-on-close="true">
      <a-menu class="app-menu" :selected-keys="[section === 'purchase' ? (form.kind === 'dataDisk' ? 'disks' : 'hosts') : section]" @menu-item-click="go">
        <a-menu-item v-for="[id, label] in tabs" :key="id">{{ label }}</a-menu-item>
      </a-menu>
    </a-drawer>
    <main>
      <a-breadcrumb class="breadcrumb"><a-breadcrumb-item>CSM 管理</a-breadcrumb-item><a-breadcrumb-item>{{ title }}</a-breadcrumb-item></a-breadcrumb>
      <header><h1>{{ title }}</h1><div class="header-actions"><a-button v-if="section === 'hosts'" type="primary" @click="go('purchase')">购买资源</a-button><a-button v-if="loggedIn" @click="load">刷新</a-button></div></header>
      <p v-if="error" class="alert" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }} <a v-if="payment?.ticket" :href="`https://ip.w7.cc/pay/${encodeURIComponent(payment.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">前往 W7 云支付 ↗</a></p>
      <section v-if="!loggedIn" class="panel"><h2>登录 CSM</h2><p class="hint">使用 W7Panel 账户授权后管理云主机。</p><button class="primary" @click="login">登录</button></section>
      <template v-else><section v-if="section === 'purchase'" class="panel purchase-page">
        <p class="hint">按资源规格和购买时长配置订单，支付确认后生效。</p>
        <div class="purchase-meta">
          <label>资源类型<select v-model="form.kind"><option value="csm">云主机</option><option value="dataDisk">独立数据盘</option></select></label>
          <label>购买方式<select v-model="form.action"><option value="buy">购买</option><option value="renew">续费</option><option value="expand">扩容</option></select></label>
          <label>名称<input v-model.trim="form.name" placeholder="例如 my-host" /></label>
          <label v-if="form.kind === 'csm' && form.action === 'buy'">系统模板<select v-model="form.templateName" required><option value="">请选择系统模板</option><option v-for="item in templates" :key="nameOf(item)" :value="nameOf(item)">{{ nameOf(item) }}</option></select></label>
        </div>
        <p v-if="form.kind === 'csm' && form.action === 'buy' && !loading && !templates.length" class="alert" role="alert">暂无可用系统模板，请先创建系统模板。</p>
        <div class="purchase-table">
          <div class="purchase-row purchase-row-head"><span>名称</span><span>配置</span><span>价格</span></div>
          <div v-for="item in visiblePurchaseResources" :key="item.field" class="purchase-row">
            <span class="purchase-label">{{ item.label }}</span>
            <div v-if="form.action !== 'renew'" class="purchase-control">
              <input v-model.number="form[item.field]" type="range" min="1" :max="Math.max(item.max, Number(form[item.field]) || 1)" :aria-label="`${item.label}配置滑块`" />
              <label class="purchase-number"><input v-model.number="form[item.field]" type="number" min="1" :aria-label="`${item.label}配置`" /><span>{{ item.unit }}</span></label>
            </div>
            <span v-else class="purchase-muted">沿用当前配置</span>
            <span class="purchase-price">{{ linePrice(item) }}<small v-if="purchasePrices && form.action === 'buy'">¥{{ (purchasePrices[item.price] / 100).toFixed(2) }} / {{ item.unit }} / 月</small></span>
          </div>
        </div>
        <div v-if="form.action !== 'expand'" class="purchase-duration">
          <strong>购买时长</strong>
          <div class="purchase-periods">
            <button v-for="period in purchasePeriods" :key="`${period.quantity}-${period.unit}`" type="button" class="light" :class="{ chosen: form.quantity === period.quantity && form.unit === period.unit }" @click="form.quantity = period.quantity; form.unit = period.unit">{{ period.quantity }}{{ period.unit === 'year' ? '年' : '月' }}</button>
          </div>
          <div class="purchase-custom"><label>自定义时长<input v-model.number="form.quantity" type="number" min="1" :max="form.unit === 'year' ? 10 : 120" /></label><label>单位<select v-model="form.unit"><option value="month">月</option><option value="year">年</option></select></label></div>
        </div>
        <div class="purchase-total"><span>总价</span><strong v-if="quoteAmountCents !== null">¥{{ (quoteAmountCents / 100).toFixed(2) }}</strong><span v-else>—</span></div>
        <p v-if="quoteAmountCents !== null && purchaseCombination && purchaseCombination.discountNew < 100" class="hint" role="status">已计入组合优惠：{{ purchaseCombination.discountNew }}%（{{ purchaseCombination.discountNew / 10 }} 折）</p>
        <p v-if="quoteLoading" class="hint" role="status">正在报价…</p><p v-else-if="quoteError" class="alert" role="alert">{{ quoteError }}</p>
        <div class="purchase-actions"><button class="primary" :disabled="quoteAmountCents === null || quoteLoading || (form.kind === 'csm' && form.action === 'buy' && !form.templateName)" @click="buy">{{ form.action === 'renew' ? '续费' : form.action === 'expand' ? '扩容' : '购买' }}</button></div>
      </section>
      <section v-else-if="section === 'pricing'" class="panel form-panel">
        <p class="hint">设置各项资源的月单价；购买页按实际资源和时长向服务端获取报价。只有创始人可保存。</p>
        <div class="grid"><label v-for="[field, label, unit] in pricingFields" :key="field">{{ label }}（元 / {{ unit }} / 月）<input v-model.trim="pricing[field]" type="number" min="0" step="0.01" required placeholder="0.00" /></label></div>
        <div class="pricing-combinations">
          <div class="panel-head"><strong>组合折扣</strong><button type="button" class="light" :disabled="pricingCombinations.length >= 100" @click="addCombination">＋ 添加组合</button></div>
          <p class="hint">时长和 CPU、内存、存储、带宽全部匹配时生效；新购与续费分别设置，扩容仍按单价计算。100% 表示原价。</p>
          <div v-for="(item, index) in pricingCombinations" :key="index" class="combination-card">
            <div class="panel-head"><strong>组合 {{ index + 1 }}</strong><button type="button" class="text danger" @click="pricingCombinations.splice(index, 1)">删除</button></div>
            <div class="grid">
              <label>时长<input v-model.number="item.quantity" type="number" min="1" :max="item.unit === 'year' ? 10 : 120" step="1" /></label>
              <label>单位<select v-model="item.unit"><option value="month">月</option><option value="year">年</option></select></label>
              <label>CPU（核）<input v-model.number="item.cpu" type="number" min="1" step="1" /></label>
              <label>内存（GiB）<input v-model.number="item.memoryGi" type="number" min="1" step="1" /></label>
              <label>存储（GiB）<input v-model.number="item.storageGi" type="number" min="1" step="1" /></label>
              <label>带宽（Mbps）<input v-model.number="item.bandwidthMbps" type="number" min="1" step="1" /></label>
              <label>新购折扣（%）<input v-model.number="item.discountNew" type="number" min="1" max="100" step="1" /></label>
              <label>续费折扣（%）<input v-model.number="item.discountRenew" type="number" min="1" max="100" step="1" /></label>
            </div>
          </div>
        </div>
        <button class="primary" @click="savePricing">保存费用配置</button>
      </section>
      <template v-if="section === 'templates'">
        <div class="page-tabs" role="tablist" aria-label="系统模板管理"><button type="button" role="tab" :aria-selected="templateTab === 'templates'" :class="{ active: templateTab === 'templates' }" @click="templateTab = 'templates'">系统模板</button><button type="button" role="tab" :aria-selected="templateTab === 'apps'" :class="{ active: templateTab === 'apps' }" @click="templateTab = 'apps'">初始化配置</button></div>
        <SystemTemplates v-if="templateTab === 'templates'" :rows="rows" :loading="loading" @saved="load" @navigate="go" />
        <section v-else class="panel form-panel"><div class="panel-head"><h2>初始化配置</h2><button class="primary" @click="openAppDrawer">＋ 新建初始化配置</button></div><div class="table-wrap"><table><thead><tr><th>名称</th><th>执行顺序</th><th>操作</th></tr></thead><tbody><tr v-for="row in appRows" :key="nameOf(row)"><td>{{ nameOf(row) }}</td><td>{{ row.spec?.order }}</td><td class="actions"><button class="text danger" @click="remove(`/preinstall-applications/${encodeURIComponent(nameOf(row))}`)">删除</button></td></tr></tbody></table></div><div v-if="!appRows.length" class="empty">暂无初始化配置</div></section>
      </template>
      <a-drawer v-model:visible="appDrawerOpen" title="新建初始化配置" width="min(560px, 100vw)" :footer="false" :unmount-on-close="true">
        <form @submit.prevent="createConfig"><p v-if="appError" class="alert" role="alert">{{ appError }}</p><div class="grid"><label>名称<input v-model.trim="draft.name" required maxlength="63" placeholder="唯一名称" /></label><label>执行顺序<input v-model.number="draft.order" type="number" required /></label><label class="script-field">Shell 脚本<textarea v-model="draft.shell" required rows="10" /></label></div><div class="app-drawer-actions"><button type="button" class="light" @click="appDrawerOpen = false">取消</button><button class="primary" :disabled="appSaving">{{ appSaving ? '保存中…' : '保存配置' }}</button></div></form>
      </a-drawer>
      <UpgradeVersions v-if="section === 'versions'" :rows="rows" :loading="loading" @saved="load" />
      <section v-if="section === 'orders'" class="panel">
        <div class="panel-head"><strong>全部订单 {{ rows.length }}</strong><form class="inline-form" @submit.prevent="appliedOrderSearch = orderSearch.trim()"><label>订单号<input v-model="orderSearch" placeholder="请输入订单号" /></label><button class="light">查询</button><button type="button" class="light" @click="orderSearch = ''; appliedOrderSearch = ''">重置</button></form></div>
        <div v-if="!loading && !filteredOrders.length" class="empty">暂无订单</div>
        <div v-else class="table-wrap order-table"><table><thead><tr><th>订单号</th><th>关联资源</th><th>订单类型</th><th>资源配置</th><th>金额</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="row in filteredOrders" :key="row.orderSn"><td class="mono">{{ row.orderSn }}</td><td><button class="text" @click="go(row.kind === 'dataDisk' ? 'disks' : 'hosts')">{{ row.name }}</button><small>{{ row.kind === 'dataDisk' ? '数据盘' : '云主机' }}</small></td><td>{{ ({ buy: '购买', renew: '续费', expand: '扩容' })[row.action] || row.action }}</td><td>{{ row.action === 'renew' ? `${row.quantity} ${row.unit === 'year' ? '年' : '月'}` : row.kind === 'dataDisk' ? `${row.storageGi} GiB` : `${row.cpu}核 / ${row.memoryGi} GiB / ${row.storageGi} GiB / ${row.bandwidthMbps} Mbps` }}</td><td>¥{{ (row.amountCents / 100).toFixed(2) }}</td><td><span class="status">{{ orderStatus(row) }}</span></td><td>{{ dateText(row.createdAt) }}</td><td class="actions"><a v-if="row.status === 'pending' && row.ticket" class="text" :href="`https://ip.w7.cc/pay/${encodeURIComponent(row.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">支付 ↗</a><button v-if="row.status === 'pending'" class="text" @click="refreshOrder(row)">刷新状态</button><span v-else>—</span></td></tr></tbody></table></div>
      </section>
      <section v-else-if="section === 'disks'" class="panel">
        <div class="panel-head"><button class="primary" @click="orderForDisk('buy')">＋ 购买数据盘</button><span>{{ loading ? '正在加载…' : `${rows.length} 块数据盘` }}</span></div>
        <div v-if="!loading && !rows.length" class="empty">暂无数据盘</div>
        <div v-else class="table-wrap disk-table"><table><thead><tr><th>名称 / PVC</th><th>所属主机</th><th>容量</th><th>到期时间</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="nameOf(row)"><td><strong>{{ nameOf(row) }}</strong><small>{{ row.status?.pvcName || 'PVC 待创建' }}</small></td><td>{{ row.spec?.attached ? row.spec?.csmName : '未挂载' }}</td><td>{{ row.spec?.storageGi || 0 }} GiB</td><td>{{ dateText(row.spec?.expireTime) }}</td><td><span class="status">{{ diskStatus(row) }}</span></td><td class="actions"><button class="text" @click="detail('data-disks', row)">详情</button><button v-if="row.spec?.paid && !row.spec?.attached && row.status?.phase === 'Available'" class="text" @click="detail('data-disks', row)">挂载</button><button v-if="row.spec?.attached" class="text" @click="detail('data-disks', row)">卸载</button><button v-if="row.spec?.paid" class="text" @click="orderForDisk('renew', row)">续费</button><button v-if="row.spec?.paid" class="text" @click="orderForDisk('expand', row)">扩容</button><button v-if="!row.spec?.attached" class="text danger" @click="deleteDisk(row)">删除</button></td></tr></tbody></table></div>
      </section>
      <section v-else-if="!['purchase', 'pricing', 'templates', 'versions'].includes(section)" class="panel">
        <div class="panel-head"><span>{{ loading ? '正在加载…' : `${rows.length} 条记录` }}</span></div>
        <div v-if="!loading && rows.length === 0" class="empty">暂无记录。{{ section === 'hosts' ? '可从购买页面创建第一台主机。' : '' }}</div>
        <div v-else class="table-wrap"><table><thead><tr><th>名称 / 订单号</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="nameOf(row)"><td class="mono">{{ nameOf(row) }}</td><td><span class="status">{{ phaseOf(row) }}</span></td><td class="actions">
          <button v-if="section === 'hosts'" class="text" @click="detail(resource[section], row)">详情</button>
          <button v-if="section === 'hosts'" class="text" @click="run(`/csms/${nameOf(row)}/preinstall`)">预装</button>
          <button v-if="section === 'hosts'" class="text" @click="detail('csms', row)">升级</button>
          <button v-if="section === 'hosts'" class="text" @click="detail('csms', row)">终端入口</button>
        </td></tr></tbody></table></div>
      </section>
      <section v-if="selected" class="panel detail"><div class="panel-head"><h2>{{ nameOf(selected) }}</h2><button class="text" @click="selected = null">关闭</button></div>
        <dl v-if="section !== 'disks'"><dt>状态</dt><dd>{{ phaseOf(selected) }}</dd><dt>系统模板</dt><dd>{{ selected.spec?.templateName || '—' }}</dd><dt>SSH 端口</dt><dd>{{ selected.status?.sshPort || '—' }}</dd><template v-if="section === 'hosts' && sshHost && selected.status?.sshPort"><dt>SSH 连接</dt><dd class="mono">ssh -p {{ selected.status.sshPort }} root@{{ sshHost }}</dd></template></dl>
        <div v-if="section === 'hosts'" class="upgrade-form"><label>升级目标版本<select v-model="upgradeTarget"><option value="">选择版本</option><option v-for="version in versions" :key="nameOf(version)" :value="nameOf(version)">{{ nameOf(version) }}</option></select></label><button class="light" :disabled="!upgradeTarget" @click="approveUpgrade">确认升级</button></div>
        <div v-if="section === 'hosts'" class="actions"><button class="light" @click="openTerminal">打开网页终端</button></div><div v-if="section === 'hosts'" class="actions"><input v-model.trim="hostKey" aria-label="SSH 公钥" placeholder="粘贴 SSH 公钥" /><button class="light" @click="run(`/csms/${nameOf(selected)}/ssh-keys`, { publicKey: hostKey })">添加到主机</button></div>
        <template v-if="section === 'disks'"><dl><dt>容量</dt><dd>{{ selected.spec?.storageGi || 0 }} GiB</dd><dt>到期时间</dt><dd>{{ dateText(selected.spec?.expireTime) }}</dd><dt>PVC</dt><dd>{{ selected.status?.pvcName || '待创建' }}</dd><dt>挂载路径</dt><dd>{{ selected.spec?.attached ? selected.spec?.mountPath || '—' : '未挂载' }}</dd></dl><div class="actions" v-if="selected.spec?.paid"><template v-if="!selected.spec?.attached && selected.status?.phase === 'Available'"><label>目标主机<select v-model="mountTarget"><option value="">选择主机</option><option v-for="host in mountHosts.filter(host => host.spec?.paid)" :key="nameOf(host)" :value="nameOf(host)">{{ nameOf(host) }}</option></select></label><button class="light" :disabled="!mountTarget" @click="run(`/data-disks/${encodeURIComponent(nameOf(selected))}/mount`, { csmName: mountTarget })">挂载</button></template><button v-if="selected.spec?.attached" class="light" @click="unmountDisk(selected)">卸载</button></div></template>
      </section>
      <section v-if="terminalOpen" class="panel"><div class="panel-head"><h2>网页终端</h2><button class="text" @click="terminalSocket?.close(); terminalOpen = false">关闭</button></div><pre class="terminal" tabindex="0" @keydown="terminalKey">{{ terminalText || '连接中…' }}</pre><p class="hint">点击终端区域后输入命令。</p></section>
      </template>
    </main>
  </div>
</template>

<style>
:root {
  font-family: 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif;
  color: var(--color-text-1, #1d2129);
  background: var(--color-fill-1, #f7f8fa);
  font-size: 14px;
}
* { box-sizing: border-box; }
body { margin: 0; }
button, input, select, textarea { font: inherit; }
button { cursor: pointer; }
button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, a:focus-visible { outline: 2px solid rgb(var(--primary-6, 22, 93, 255)); outline-offset: 2px; }
.shell { display: flex; min-height: 100vh; background: var(--color-fill-1, #f7f8fa); }
.rail { flex: 0 0 208px; min-height: 100vh; background: var(--color-bg-2, #fff); border-right: 1px solid var(--color-border-2, #e5e6eb); }
.brand { display: flex; align-items: center; height: 56px; padding: 0 20px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); color: var(--color-text-1, #1d2129); font-size: 16px; font-weight: 600; }
.app-menu { padding: 8px; background: transparent; }
.mobile-bar { display: none; }
main { flex: 1; min-width: 0; min-height: 100vh; padding: 20px; background: var(--color-bg-2, #fff); }
.breadcrumb { margin-bottom: 20px; }
header { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 32px; margin-bottom: 20px; }
.header-actions { display: flex; align-items: center; gap: 8px; }
h1 { margin: 0; color: var(--color-text-1, #1d2129); font-size: 20px; font-weight: 600; line-height: 32px; }
h2 { margin: 0 0 16px; font-size: 16px; font-weight: 600; }
.panel { margin-bottom: 20px; background: var(--color-bg-2, #fff); }
.panel-head { display: flex; align-items: center; justify-content: space-between; margin: 0 0 12px; color: var(--color-text-3, #86909c); font-size: 13px; }
.light, .primary { min-height: 32px; padding: 0 15px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 2px; background: var(--color-fill-2, #f2f3f5); color: var(--color-text-1, #1d2129); }
.light:hover { background: var(--color-fill-3, #e5e6eb); }
.primary { border-color: rgb(var(--primary-6, 22, 93, 255)); background: rgb(var(--primary-6, 22, 93, 255)); color: #fff; }
.primary:hover { background: rgb(var(--primary-5, 64, 128, 255)); }
.primary:disabled, .light:disabled { opacity: .5; cursor: not-allowed; }
.text { min-height: 30px; padding: 3px 5px; border: 0; background: transparent; color: rgb(var(--primary-6, 22, 93, 255)); }
.text:hover, .table-link:hover { text-decoration: underline; }
.danger { color: rgb(var(--danger-6, 245, 63, 63)); }
.alert, .notice { margin: 0 0 16px; padding: 10px 16px; border-radius: 2px; }
.alert { background: var(--color-danger-light-1, #ffece8); color: rgb(var(--danger-6, 245, 63, 63)); }
.notice { background: var(--color-success-light-1, #e8ffea); color: var(--color-text-1, #1d2129); }
.notice a, .table-link { color: rgb(var(--primary-6, 22, 93, 255)); }
.hint { margin: 0 0 20px; color: var(--color-text-3, #86909c); }
.grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px 24px; max-width: 800px; margin-bottom: 20px; }
label { display: flex; flex-direction: column; gap: 7px; color: var(--color-text-2, #4e5969); }
input, select, textarea { width: 100%; min-width: 0; min-height: 32px; padding: 5px 10px; border: 1px solid transparent; border-radius: 2px; background: var(--color-fill-2, #f2f3f5); color: var(--color-text-1, #1d2129); }
input:hover, select:hover, textarea:hover { background: var(--color-fill-3, #e5e6eb); }
input:focus, select:focus, textarea:focus { border-color: rgb(var(--primary-6, 22, 93, 255)); background: var(--color-bg-2, #fff); }
textarea { min-height: 90px; resize: vertical; }
.form-panel { padding: 16px 0 8px; }
.pricing-combinations { max-width: 800px; margin-bottom: 20px; }
.combination-card { margin-bottom: 12px; padding: 16px; border: 1px solid var(--color-border-2, #e5e6eb); }
.combination-card .grid { margin-bottom: 0; }
.page-tabs { display: flex; gap: 20px; margin-bottom: 20px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); overflow-x: auto; }
.page-tabs button { flex: none; padding: 10px 2px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--color-text-2, #4e5969); }
.page-tabs button.active { border-bottom-color: rgb(var(--primary-6, 22, 93, 255)); color: rgb(var(--primary-6, 22, 93, 255)); font-weight: 600; }
.script-field { grid-column: 1 / -1; }
.app-drawer-actions { display: flex; justify-content: flex-end; gap: 10px; }
.quote { color: var(--color-text-2, #4e5969); }
.quote strong { margin-left: 8px; color: var(--color-text-1, #1d2129); font-size: 20px; }
.table-wrap { max-width: 100%; overflow-x: auto; }
.table-wrap .arco-table { min-width: 560px; }
.mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
.actions { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; }
.actions input { max-width: 320px; }
.empty { padding: 48px 16px; color: var(--color-text-3, #86909c); text-align: center; }
.detail { padding: 20px; border: 1px solid var(--color-border-2, #e5e6eb); }
.detail dl { display: grid; grid-template-columns: 120px minmax(0, 1fr); gap: 12px; }
.detail dt { color: var(--color-text-3, #86909c); }
.detail dd { margin: 0; overflow-wrap: anywhere; }
.detail .actions { margin-top: 16px; }
.upgrade-form { display: flex; align-items: end; flex-wrap: wrap; gap: 12px; margin-top: 20px; }
.upgrade-form label { min-width: 220px; }
.terminal { min-height: 300px; padding: 16px; overflow: auto; background: #1d2129; color: #f2f3f5; font: 14px/1.6 ui-monospace, Consolas, monospace; white-space: pre-wrap; }
.terminal:focus-visible { outline: 2px solid rgb(var(--primary-6, 22, 93, 255)); }
@media (max-width: 720px) {
  .rail { flex-basis: 156px; }
  .brand { padding: 0 16px; }
}
@media (max-width: 600px) {
  .shell { display: block; }
  .rail { display: none; }
  .mobile-bar { display: flex; align-items: center; gap: 12px; min-height: 48px; padding: 0 16px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); background: var(--color-bg-2, #fff); }
  .mobile-menu-trigger { width: 36px; height: 36px; padding: 0; border: 0; background: transparent; color: rgb(var(--primary-6, 22, 93, 255)); font-size: 20px; }
  main { min-height: calc(100vh - 48px); padding: 16px; }
  .grid { grid-template-columns: 1fr; }
  header { flex-wrap: wrap; }
}
input[type='range'] { min-height: 0; padding: 0; border: 0; background: transparent; accent-color: rgb(var(--primary-6, 22, 93, 255)); }
input[type='checkbox'] { width: auto; min-height: 0; padding: 0; accent-color: rgb(var(--primary-6, 22, 93, 255)); }
.table-wrap table { width: 100%; min-width: 560px; border-collapse: collapse; color: var(--color-text-1, #1d2129); text-align: left; }
.order-table table, .disk-table table { min-width: 900px; }
.table-wrap th { padding: 10px 12px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); background: var(--color-fill-2, #f2f3f5); color: var(--color-text-2, #4e5969); font-weight: 500; white-space: nowrap; }
.table-wrap td { padding: 12px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); vertical-align: middle; }
.table-wrap td small { display: block; margin-top: 4px; color: var(--color-text-3, #86909c); }
.table-wrap td.actions { display: table-cell; text-align: right; white-space: nowrap; }
.table-wrap th:last-child { text-align: right; }
.status { display: inline-block; padding: 2px 8px; border-radius: 2px; background: var(--color-primary-light-1, #e8f3ff); color: rgb(var(--primary-6, 22, 93, 255)); font-size: 12px; }
.inline-form { display: flex; align-items: end; flex-wrap: wrap; gap: 8px; }
.inline-form label { min-width: 200px; }
.panel-head strong { color: var(--color-text-1, #1d2129); }
.detail .actions label { min-width: 220px; }
.purchase-page { max-width: 1020px; margin-inline: auto; }
.purchase-meta { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-bottom: 24px; }
.purchase-table { border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 2px; overflow: hidden; }
.purchase-row { display: grid; grid-template-columns: 120px minmax(0, 1fr) 190px; gap: 18px; align-items: center; min-height: 72px; padding: 12px 18px; border-top: 1px solid var(--color-border-2, #e5e6eb); }
.purchase-row-head { min-height: 42px; border-top: 0; background: var(--color-fill-2, #f2f3f5); color: var(--color-text-3, #86909c); font-size: 12px; font-weight: 600; }
.purchase-label { font-weight: 600; }
.purchase-control { display: flex; align-items: center; gap: 20px; min-width: 0; }
.purchase-control input[type='range'] { flex: 1; min-width: 80px; }
.purchase-number { display: flex; flex: none; flex-direction: row; align-items: center; gap: 0; border: 1px solid var(--color-border-2, #e5e6eb); }
.purchase-number input { width: 90px; border: 0; text-align: right; }
.purchase-number span { align-self: stretch; display: flex; align-items: center; padding: 0 10px; background: var(--color-fill-2, #f2f3f5); color: var(--color-text-3, #86909c); }
.purchase-price { color: rgb(var(--orange-6, 255, 125, 0)); font-weight: 600; text-align: right; }
.purchase-price small { display: block; margin-top: 3px; color: var(--color-text-3, #86909c); font-size: 11px; font-weight: 400; }
.purchase-muted { color: var(--color-text-3, #86909c); }
.purchase-duration { padding: 24px 0; border-bottom: 1px solid var(--color-border-2, #e5e6eb); }
.purchase-duration strong { display: block; margin-bottom: 14px; }
.purchase-periods { display: flex; flex-wrap: wrap; gap: 8px; }
.purchase-periods button.chosen { border-color: rgb(var(--primary-6, 22, 93, 255)); background: rgb(var(--primary-6, 22, 93, 255)); color: #fff; }
.purchase-custom { display: flex; gap: 12px; margin-top: 16px; }
.purchase-custom label:first-child { width: 180px; }
.purchase-custom label:last-child { width: 100px; }
.purchase-total { display: flex; align-items: center; justify-content: space-between; padding: 22px 0; font-weight: 600; }
.purchase-total strong { color: rgb(var(--orange-6, 255, 125, 0)); font-size: 26px; }
.purchase-actions { display: flex; justify-content: center; }
.purchase-actions .primary { min-width: 160px; }
@media (max-width: 700px) {
  .purchase-meta { grid-template-columns: 1fr; }
  .purchase-row { grid-template-columns: 70px minmax(0, 1fr); gap: 10px; }
  .purchase-row-head span:last-child { display: none; }
  .purchase-price { grid-column: 2; text-align: left; }
  .purchase-control { gap: 8px; }
  .purchase-number input { width: 64px; }
  .purchase-custom label:first-child { width: 150px; }
}
</style>
