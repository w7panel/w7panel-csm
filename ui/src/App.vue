<script setup>
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, items } from './api'
import { finishLogin, loginInMicroapp, startLogin } from './auth'
import SystemTemplates from './SystemTemplates.vue'
import UpgradeVersions from './UpgradeVersions.vue'
import HostDetail from './HostDetail.vue'
const TerminalModal = defineAsyncComponent(() => import('./TerminalModal.vue'))

const tabs = [
  ['hosts', '主机'], ['templates', '系统模板'],
  ['preinstall', '预装'], ['upgrade', '升级'], ['orders', '订单'],
  ['disks', '数据管理'],
  ['versions', '版本升级'], ['pricing', '费用设置'],
]
const route = useRoute()
const router = useRouter()
const isMicroApp = Boolean(window.__POWERED_BY_WUJIE__)
const hostRouteName = computed(() => route.params.name || '')
const terminalPage = computed(() => route.name === 'terminal')
const hostLoading = ref(false)
const section = computed(() => route.meta.section || route.name || 'hosts')
const templateTab = computed(() => route.name === 'apps' ? 'apps' : 'templates')
const mobileMenuOpen = ref(false)
const loading = ref(false)
const loggedIn = ref(!isMicroApp && Boolean(localStorage.getItem('csm-token')))
const loginPending = ref(false)
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
const periodUnits = { day: '天', month: '月', year: '年' }
const periodLimits = { day: 3650, month: 120, year: 10 }
const validPeriod = (quantity, unit) => Number.isInteger(quantity) && quantity >= 1 && quantity <= periodLimits[unit]
const mountTarget = ref('')
const sshHost = ref('')
const versions = ref([])
const payment = ref(null)
const pricingFields = [
  ['cpuCents', 'CPU', '核'], ['memoryGiCents', '内存', 'GiB'],
  ['storageGiCents', '存储', 'GiB'], ['bandwidthMbpsCents', '带宽', 'Mbps'],
]
const pricing = ref(Object.fromEntries(pricingFields.map(([field]) => [field, ''])))
const pricingCombinations = ref([])
const newPricingPeriod = ref({ quantity: 3, unit: 'day' })
const activePricingPeriod = ref('')
const periodKey = item => `${item.unit}:${item.quantity}`
const pricingPeriods = computed(() => [...new Map(pricingCombinations.value.map(item => [periodKey(item), { key: periodKey(item), quantity: item.quantity, unit: item.unit }])).values()].sort((a, b) => Object.keys(periodUnits).indexOf(a.unit) - Object.keys(periodUnits).indexOf(b.unit) || a.quantity - b.quantity))
const selectedPricingPeriod = computed(() => pricingPeriods.value.some(item => item.key === activePricingPeriod.value) ? activePricingPeriod.value : pricingPeriods.value[0]?.key || '')
const selectedPricingPeriodLabel = computed(() => { const period = pricingPeriods.value.find(item => item.key === selectedPricingPeriod.value); return period ? `${period.quantity}${periodUnits[period.unit]}` : '' })
const currentPeriodRows = computed(() => pricingCombinations.value.map((item, index) => ({ item, index })).filter(row => periodKey(row.item) === selectedPricingPeriod.value))
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
let loadSequence = 0
const orderSearch = ref('')
const appliedOrderSearch = ref('')
const filteredOrders = computed(() => rows.value.filter(row => row.orderSn?.includes(appliedOrderSearch.value)))
const mountHosts = ref([])
const diskRows = computed(() => [
  ...mountHosts.value.filter(host => host.status?.systemPVCName || Number(host.spec?.systemDiskGi) > 0).map(host => ({
    systemHost: host,
    metadata: host.metadata,
    spec: { csmName: host.metadata?.name, attached: true, storageGi: host.spec?.systemDiskGi, expireTime: host.spec?.expireTime },
    status: { pvcName: host.status?.systemPVCName },
  })),
  ...rows.value,
])
const draft = ref({ name: '', cloudConfig: '#cloud-config\npackages:\n  - curl\n', shell: '', kind: 'cloud', order: 0, resourceVersion: '' })
const appDrawerOpen = ref(false)
const appSaving = ref(false)
const appError = ref('')
const terminalOpen = ref(false)
function openTerminal() {
  if (window.matchMedia('(max-width: 600px)').matches) {
    router.push({ name: 'terminal', params: { name: hostRouteName.value } })
  } else terminalOpen.value = true
}
function closeTerminal() {
  terminalOpen.value = false
}
const title = computed(() => route.name === 'apps' ? '初始化配置' : section.value === 'purchase' ? '购买' : tabs.find(([id]) => id === section.value)?.[1] || '主机')
const resource = { hosts: 'csms', templates: 'system-templates', preinstall: 'preinstall-executions', upgrade: 'upgrade-executions', orders: 'orders', disks: 'data-disks', apps: 'preinstall-applications', versions: 'upgrade-versions' }

async function login() {
  error.value = ''
  loginPending.value = true
  try {
    if (!isMicroApp) { await startLogin(); return }
    const config = await loginInMicroapp()
    loggedIn.value = true
    sshHost.value = config.sshHost || ''
    await Promise.all([load(), hostRouteName.value ? loadHost(hostRouteName.value) : null])
  } catch (e) { error.value = e.message }
  finally { loginPending.value = false }
}
function go(id) { notice.value = ''; payment.value = null; selected.value = null; terminalOpen.value = false; mobileMenuOpen.value = false; router.push({ name: id }) }
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
  const months = form.value.quantity * (form.value.unit === 'day' ? 1 / 30 : form.value.unit === 'year' ? 12 : 1)
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
  if (pricingCombinations.value.length >= 100 || !selectedPricingPeriod.value) return
  const [unit, quantity] = selectedPricingPeriod.value.split(':')
  let cpu = 2
  while (pricingCombinations.value.some(item => item.unit === unit && item.quantity === Number(quantity) && item.cpu === cpu && item.memoryGi === 4 && item.storageGi === 40 && item.bandwidthMbps === 5)) cpu++
  pricingCombinations.value.push({ quantity: Number(quantity), unit, cpu, memoryGi: 4, storageGi: 40, bandwidthMbps: 5, discountNew: 100, discountRenew: 100 })
}
function addPricingPeriod() {
  const { quantity, unit } = newPricingPeriod.value
  if (!validPeriod(quantity, unit)) { error.value = '周期须为正整数：最多 3650 天、120 月或 10 年。'; return }
  const key = periodKey(newPricingPeriod.value)
  if (!pricingPeriods.value.some(item => item.key === key)) {
    if (pricingCombinations.value.length >= 100) { error.value = '最多可设置 100 条组合配置。'; return }
    pricingCombinations.value.push({ quantity, unit, cpu: 2, memoryGi: 4, storageGi: 40, bandwidthMbps: 5, discountNew: 100, discountRenew: 100 })
  }
  error.value = ''
  activePricingPeriod.value = key
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
    if (!validPeriod(item.quantity, item.unit) || !['cpu', 'memoryGi', 'storageGi', 'bandwidthMbps', 'discountNew', 'discountRenew'].every(field => Number.isInteger(item[field])) ||
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
  const sequence = ++loadSequence
  error.value = ''; loading.value = true
  try {
    const currentSection = section.value
    const requests = [currentSection === 'purchase' || currentSection === 'pricing' ? Promise.resolve([]) : api('/' + (resource[currentSection] || 'csms'))]
    if (currentSection === 'purchase') requests.push(api('/system-templates'), api('/pricing').catch(e => { if (e.status === 404) return null; throw e }))
    if (currentSection === 'pricing') requests.push(api('/pricing').catch(e => { if (e.status === 404) return null; throw e }))
    if (currentSection === 'hosts') requests.push(api('/upgrade-versions'))
    if (currentSection === 'templates') requests.push(api('/preinstall-applications'))
    if (currentSection === 'disks') requests.push(api('/csms'))
    const [list, extra, prices] = await Promise.all(requests)
    if (sequence !== loadSequence) return
    rows.value = items(list)
    if (currentSection === 'orders') rows.value.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
    if (currentSection === 'purchase') {
      templates.value = items(extra).filter(item => item.spec?.initMode === 'cloudInit')
      purchasePrices.value = prices
      scheduleQuote()
    }
    if (currentSection === 'pricing') {
      pricing.value = Object.fromEntries(pricingFields.map(([field]) => [field, extra ? (extra[field] / 100).toFixed(2) : '']))
      pricingCombinations.value = extra?.combinations?.map(item => ({ ...item })) || []
    }
    if (currentSection === 'hosts') versions.value = items(extra)
    if (currentSection === 'templates') appRows.value = items(extra)
    if (currentSection === 'disks') mountHosts.value = items(extra)
  } catch (e) { if (sequence === loadSequence) { error.value = e.message; rows.value = []; if (e.status === 401) loggedIn.value = false } }
  finally { if (sequence === loadSequence) loading.value = false }
}
async function detail(kind, row) {
  const name = row.metadata?.name || row.name
  mountTarget.value = ''
  try { selected.value = await api(`/${kind}/${encodeURIComponent(name)}`) }
  catch (e) { error.value = e.message }
}
async function loadHost(name) {
  error.value = ''; hostLoading.value = true
  try { const host = await api(`/csms/${encodeURIComponent(name)}`); if (hostRouteName.value === name) selected.value = host }
  catch (e) { if (hostRouteName.value === name) { selected.value = null; error.value = e.message } }
  finally { if (hostRouteName.value === name) hostLoading.value = false }
}
function openHost(row) {
  const name = nameOf(row)
  if (hostRouteName.value === name && route.name === 'host') loadHost(name)
  else router.push({ name: 'host', params: { name } })
}
function syncRoute() {
  notice.value = ''; payment.value = null
  const name = hostRouteName.value
  terminalOpen.value = false
  if (name) { if (nameOf(selected.value) !== name) { selected.value = null; loadHost(name) } }
  else selected.value = null
}
async function run(path, body) {
  error.value = ''; notice.value = ''; payment.value = null
  try {
    const result = await api(path, { method: 'POST', body: JSON.stringify(body || {}) })
    notice.value = result?.status === 'paid' ? `订单 ${result.orderSn} 已支付，资源正在生效。` : result?.orderSn ? `订单 ${result.orderSn} 已创建，支付确认后生效。` : '操作已提交。'
    if (result?.ticket) payment.value = result
    if (section.value === 'disks') selected.value = null
    await Promise.all([load(), hostRouteName.value ? loadHost(hostRouteName.value) : null])
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
  draft.value = { name: '', cloudConfig: '#cloud-config\npackages:\n  - curl\n', shell: '', kind: 'cloud', order: 0, resourceVersion: '' }
  appError.value = ''
  appDrawerOpen.value = true
}
function editConfig(row) {
  draft.value = { name: nameOf(row), cloudConfig: row.spec?.cloudConfig || '', shell: row.spec?.shell || '', kind: row.spec?.shell ? 'shell' : 'cloud', order: row.spec?.order ?? 0, resourceVersion: row.metadata.resourceVersion }
  appError.value = ''
  appDrawerOpen.value = true
}
async function saveConfig() {
  appSaving.value = true
  appError.value = ''
  try {
    const editing = Boolean(draft.value.resourceVersion)
    const spec = { order: Number(draft.value.order), ...(draft.value.kind === 'shell' ? { shell: draft.value.shell } : { cloudConfig: draft.value.cloudConfig }) }
    if (!editing) spec.enabled = true
    await api(`/preinstall-applications${editing ? `/${encodeURIComponent(draft.value.name)}` : ''}`, { method: editing ? 'PUT' : 'POST', body: JSON.stringify({ metadata: { name: draft.value.name, ...(editing ? { resourceVersion: draft.value.resourceVersion } : {}) }, spec }) })
    appDrawerOpen.value = false
    notice.value = editing ? '初始化配置已保存。' : '初始化配置已创建。'
    await load()
  } catch (e) { appError.value = e.status === 409 && draft.value.resourceVersion ? '配置已变更或内容无效，请刷新列表后重试。' : e.message }
  finally { appSaving.value = false }
}
function nameOf(row) { return row?.metadata?.name || row?.name || row?.orderSn || '—' }
function phaseOf(row) { return row.status?.phase || row.status || row.spec?.order?.status || '—' }
watch(section, load)
watch(() => route.fullPath, syncRoute)
watch(form, scheduleQuote, { deep: true })
watch(() => form.value.kind, kind => {
  if (kind === 'csm') {
    if (!form.value.cpu) form.value.cpu = 2
    if (!form.value.memoryGi) form.value.memoryGi = 4
    if (!form.value.bandwidthMbps) form.value.bandwidthMbps = 5
  }
})
function onHostRoute(path, data) { if (!data?.fromSubPanel) router.push(path || '/hosts') }
onMounted(async () => {
  if (isMicroApp) window.$wujie?.bus?.$on?.('routeChange', onHostRoute)
  try {
    await router.isReady()
    if (isMicroApp) { await login(); return }
    const redirect = await finishLogin()
    if (redirect) { loggedIn.value = true; await router.replace(redirect) }
    const [config] = await Promise.all([api('/auth/config'), loggedIn.value ? load() : null, loggedIn.value && hostRouteName.value ? loadHost(hostRouteName.value) : null])
    sshHost.value = config.sshHost || ''
  } catch (e) { error.value = e.message }
})
onUnmounted(() => { if (isMicroApp) window.$wujie?.bus?.$off?.('routeChange', onHostRoute) })
</script>

<template>
  <div class="shell" :class="{ 'shell--micro': isMicroApp }">
    <div v-if="!isMicroApp" class="topbar"><div class="topbar-brand"><button type="button" class="mobile-menu-trigger" aria-label="打开导航菜单" @click="mobileMenuOpen = true">☰</button><span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span><span>CSM</span><small>云资源管理</small></div><span class="topbar-context">资源控制台</span></div>
    <aside v-if="!isMicroApp" class="rail" aria-label="CSM 功能导航">
      <div class="brand">功能导航</div>
      <a-menu class="app-menu" :selected-keys="[section === 'purchase' ? (form.kind === 'dataDisk' ? 'disks' : 'hosts') : section]" @menu-item-click="go">
        <a-menu-item v-for="[id, label] in tabs" :key="id">{{ label }}</a-menu-item>
      </a-menu>
    </aside>
    <a-drawer v-if="!isMicroApp" v-model:visible="mobileMenuOpen" title="CSM 管理" placement="left" width="min(280px, 88vw)" :footer="false" :unmount-on-close="true">
      <a-menu class="app-menu" :selected-keys="[section === 'purchase' ? (form.kind === 'dataDisk' ? 'disks' : 'hosts') : section]" @menu-item-click="go">
        <a-menu-item v-for="[id, label] in tabs" :key="id">{{ label }}</a-menu-item>
      </a-menu>
    </a-drawer>
    <main>
      <header class="page-header" :class="{ 'page-header--detail': hostRouteName }">
        <div class="page-heading"><a-breadcrumb v-if="hostRouteName" class="breadcrumb"><a-breadcrumb-item><a :href="router.resolve({ name: 'hosts' }).href" @click.prevent="go('hosts')">主机</a></a-breadcrumb-item><a-breadcrumb-item v-if="terminalPage"><a :href="router.resolve({ name: 'host', params: { name: hostRouteName } }).href" @click.prevent="router.push({ name: 'host', params: { name: hostRouteName } })">{{ hostRouteName }}</a></a-breadcrumb-item><a-breadcrumb-item v-else>{{ hostRouteName }}</a-breadcrumb-item><a-breadcrumb-item v-if="terminalPage">终端</a-breadcrumb-item></a-breadcrumb><h1 v-if="!hostRouteName">{{ title }}</h1></div>
        <div v-if="!hostRouteName && loggedIn" class="page-actions-row"><a-button v-if="section === 'hosts'" type="primary" @click="go('purchase')">＋ 购买资源</a-button><a-button @click="load">刷新</a-button></div>
      </header>
      <p v-if="error" class="alert" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }} <a v-if="payment?.ticket" :href="`https://ip.w7.cc/pay/${encodeURIComponent(payment.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">前往 W7 云支付 ↗</a></p>
      <section v-if="!loggedIn" class="panel"><h2>{{ isMicroApp ? '连接 CSM' : '登录 CSM' }}</h2><p class="hint">{{ isMicroApp ? '正在通过宿主授权访问 CSM。' : '使用 W7Panel 账户授权后管理云主机。' }}</p><button v-if="!loginPending" class="primary" @click="login">{{ isMicroApp ? '重试授权' : '登录' }}</button></section>
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
            <button v-for="period in purchasePeriods" :key="`${period.quantity}-${period.unit}`" type="button" class="light" :class="{ chosen: form.quantity === period.quantity && form.unit === period.unit }" @click="form.quantity = period.quantity; form.unit = period.unit">{{ period.quantity }}{{ periodUnits[period.unit] }}</button>
          </div>
          <div class="purchase-custom"><label>自定义时长<input v-model.number="form.quantity" type="number" min="1" :max="periodLimits[form.unit]" /></label><label>单位<select v-model="form.unit"><option value="day">天</option><option value="month">月</option><option value="year">年</option></select></label></div>
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
          <div class="panel-head"><strong>组合折扣</strong></div>
          <p class="hint">时长和 CPU、内存、存储、带宽全部匹配时生效；新购与续费分别设置，扩容仍按单价计算。100% 表示原价。</p>
          <div class="pricing-period-create"><label>添加周期<input v-model.number="newPricingPeriod.quantity" type="number" min="1" :max="periodLimits[newPricingPeriod.unit]" step="1" /></label><label>单位<select v-model="newPricingPeriod.unit"><option value="day">天</option><option value="month">月</option><option value="year">年</option></select></label><button type="button" class="light" @click="addPricingPeriod">＋ 添加周期</button></div>
          <div v-if="pricingPeriods.length" class="page-tabs" role="tablist" aria-label="组合折扣周期"><button v-for="period in pricingPeriods" :key="period.key" type="button" role="tab" :aria-selected="selectedPricingPeriod === period.key" :class="{ active: selectedPricingPeriod === period.key }" @click="activePricingPeriod = period.key">{{ period.quantity }}{{ periodUnits[period.unit] }}</button></div>
          <div v-if="selectedPricingPeriod" class="panel-head"><strong>{{ selectedPricingPeriodLabel }}的配置</strong><button type="button" class="light" :disabled="pricingCombinations.length >= 100" @click="addCombination">＋ 添加配置</button></div>
          <div v-if="selectedPricingPeriod" class="table-wrap combination-list"><table><thead><tr><th>CPU（核）</th><th>内存（GiB）</th><th>存储（GiB）</th><th>带宽（Mbps）</th><th>新购折扣（%）</th><th>续费折扣（%）</th><th>操作</th></tr></thead><tbody><tr v-for="{ item, index } in currentPeriodRows" :key="index"><td><input v-model.number="item.cpu" type="number" min="1" step="1" aria-label="CPU 核数" /></td><td><input v-model.number="item.memoryGi" type="number" min="1" step="1" aria-label="内存 GiB" /></td><td><input v-model.number="item.storageGi" type="number" min="1" step="1" aria-label="存储 GiB" /></td><td><input v-model.number="item.bandwidthMbps" type="number" min="1" step="1" aria-label="带宽 Mbps" /></td><td><input v-model.number="item.discountNew" type="number" min="1" max="100" step="1" aria-label="新购折扣百分比" /></td><td><input v-model.number="item.discountRenew" type="number" min="1" max="100" step="1" aria-label="续费折扣百分比" /></td><td><button type="button" class="text danger" :aria-label="`删除第 ${index + 1} 条配置`" @click="pricingCombinations.splice(index, 1)">删除</button></td></tr></tbody></table></div>
          <p v-else class="hint">暂无组合折扣。先添加一个周期，再设置该周期的资源配置。</p>
        </div>
        <button class="primary" @click="savePricing">保存费用配置</button>
      </section>
      <template v-if="section === 'templates'">
        <div class="page-tabs" role="tablist" aria-label="系统模板管理"><button type="button" role="tab" :aria-selected="templateTab === 'templates'" :class="{ active: templateTab === 'templates' }" @click="go('templates')">系统模板</button><button type="button" role="tab" :aria-selected="templateTab === 'apps'" :class="{ active: templateTab === 'apps' }" @click="go('apps')">初始化配置</button></div>
        <SystemTemplates v-if="templateTab === 'templates'" :rows="rows" :loading="loading" @saved="load" @navigate="go" />
        <section v-else class="panel form-panel"><div class="panel-head"><button class="primary" @click="openAppDrawer">＋ 新建初始化配置</button></div><div class="table-wrap app-config-table"><table><thead><tr><th>名称</th><th>执行顺序</th><th>类型</th><th>操作</th></tr></thead><tbody><tr v-for="row in appRows" :key="nameOf(row)"><td>{{ nameOf(row) }}</td><td>{{ row.spec?.order }}</td><td>{{ row.spec?.cloudConfig || row.spec?.hasCloudConfig ? 'cloud-init' : '旧版 Shell' }}</td><td class="actions"><button v-if="row.spec?.cloudConfig || row.spec?.shell" class="text" @click="editConfig(row)">编辑</button><button class="text danger" @click="remove(`/preinstall-applications/${encodeURIComponent(nameOf(row))}`)">删除</button></td></tr></tbody></table></div><div v-if="!appRows.length" class="empty">暂无初始化配置</div></section>
      </template>
      <a-drawer v-model:visible="appDrawerOpen" :title="draft.resourceVersion ? '编辑初始化配置' : '新建初始化配置'" width="min(560px, 100vw)" :footer="false" :unmount-on-close="true">
        <form @submit.prevent="saveConfig"><p v-if="appError" class="alert" role="alert">{{ appError }}</p><div class="grid"><label>名称<input v-model.trim="draft.name" required maxlength="63" placeholder="唯一名称" :readonly="!!draft.resourceVersion" /></label><label>合并顺序<input v-model.number="draft.order" type="number" required /></label><label v-if="draft.kind === 'shell'" class="script-field">Shell 脚本<textarea v-model="draft.shell" required rows="10" spellcheck="false" /></label><label v-else class="script-field">cloud-config YAML<textarea v-model="draft.cloudConfig" required rows="10" spellcheck="false" /></label></div><p v-if="draft.kind === 'cloud'" class="hint">以 #cloud-config 开头；同名字段以后项为准，列表按顺序追加，实际执行遵循 cloud-init 模块阶段。</p><div class="app-drawer-actions"><button type="button" class="light" @click="appDrawerOpen = false">取消</button><button class="primary" :disabled="appSaving">{{ appSaving ? '保存中…' : '保存配置' }}</button></div></form>
      </a-drawer>
      <UpgradeVersions v-if="section === 'versions'" :rows="rows" :loading="loading" @saved="load" />
      <section v-if="section === 'orders'" class="panel">
        <div class="panel-head order-panel-head"><span>{{ loading ? '正在加载…' : `${rows.length} 条订单` }}</span><form class="inline-form" @submit.prevent="appliedOrderSearch = orderSearch.trim()"><label>订单号<input v-model="orderSearch" placeholder="请输入订单号" /></label><button class="light">查询</button><button type="button" class="light" @click="orderSearch = ''; appliedOrderSearch = ''">重置</button></form></div>
        <div v-if="!loading && !filteredOrders.length" class="empty">暂无订单</div>
        <div v-else class="table-wrap order-table"><table><thead><tr><th>订单号</th><th>关联资源</th><th>订单类型</th><th>资源配置</th><th>金额</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="row in filteredOrders" :key="row.orderSn"><td class="mono">{{ row.orderSn }}</td><td><button class="text" @click="go(row.kind === 'dataDisk' ? 'disks' : 'hosts')">{{ row.name }}</button><small>{{ row.kind === 'dataDisk' ? '数据盘' : '云主机' }}</small></td><td>{{ ({ buy: '购买', renew: '续费', expand: '扩容' })[row.action] || row.action }}</td><td>{{ row.action === 'renew' ? `${row.quantity} ${periodUnits[row.unit] || row.unit}` : row.kind === 'dataDisk' ? `${row.storageGi} GiB` : `${row.cpu}核 / ${row.memoryGi} GiB / ${row.storageGi} GiB / ${row.bandwidthMbps} Mbps` }}</td><td>¥{{ (row.amountCents / 100).toFixed(2) }}</td><td><span class="status">{{ orderStatus(row) }}</span></td><td>{{ dateText(row.createdAt) }}</td><td class="actions"><a v-if="row.status === 'pending' && row.ticket" class="text" :href="`https://ip.w7.cc/pay/${encodeURIComponent(row.ticket)}?header=false&footer=false`" target="_blank" rel="noopener noreferrer">支付 ↗</a><button v-if="row.status === 'pending'" class="text" @click="refreshOrder(row)">刷新状态</button><span v-else>—</span></td></tr></tbody></table></div>
      </section>
      <section v-else-if="section === 'disks'" class="panel">
        <div class="panel-head"><button class="primary" @click="orderForDisk('buy')">＋ 购买数据盘</button><span>{{ loading ? '正在加载…' : `${diskRows.length} 块磁盘` }}</span></div>
        <div v-if="!loading && !diskRows.length" class="empty">暂无磁盘</div>
        <div v-else class="table-wrap disk-table"><table><thead><tr><th>PVC / 类型</th><th>所属主机</th><th>容量</th><th>到期时间</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in diskRows" :key="`${row.systemHost ? 'system' : 'data'}:${row.metadata?.namespace || ''}:${nameOf(row)}`"><td><strong>{{ row.status?.pvcName || 'PVC 待创建' }}</strong><small>{{ row.systemHost ? '系统盘' : '数据盘' }} · {{ nameOf(row) }}</small></td><td>{{ row.spec?.attached ? row.spec?.csmName : '未挂载' }}</td><td>{{ row.spec?.storageGi || 0 }} GiB</td><td>{{ dateText(row.spec?.expireTime) }}</td><td><span class="status">{{ row.systemHost ? phaseOf(row.systemHost) : diskStatus(row) }}</span></td><td class="actions"><button v-if="row.systemHost" class="text" @click="openHost(row.systemHost)">主机详情</button><template v-else><button class="text" @click="detail('data-disks', row)">详情</button><button v-if="row.spec?.paid && !row.spec?.attached && row.status?.phase === 'Available'" class="text" @click="detail('data-disks', row)">挂载</button><button v-if="row.spec?.attached" class="text" @click="detail('data-disks', row)">卸载</button><button v-if="row.spec?.paid" class="text" @click="orderForDisk('renew', row)">续费</button><button v-if="row.spec?.paid" class="text" @click="orderForDisk('expand', row)">扩容</button><button v-if="!row.spec?.attached" class="text danger" @click="deleteDisk(row)">删除</button></template></td></tr></tbody></table></div>
      </section>
      <section v-else-if="section === 'hosts' && !hostRouteName" class="panel">
        <div class="panel-head"><span>{{ loading ? '正在加载…' : `${rows.length} 台主机` }}</span></div>
        <div v-if="!loading && !rows.length" class="empty">暂无记录。可从购买页面创建第一台主机。</div>
        <div v-else class="table-wrap host-table"><table><thead><tr><th>主机名称</th><th>系统模板</th><th>CPU</th><th>内存</th><th>系统盘</th><th>带宽</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="nameOf(row)"><td><a class="table-link mono" :href="router.resolve({ name: 'host', params: { name: nameOf(row) } }).href" @click.prevent="openHost(row)">{{ nameOf(row) }}</a></td><td><span class="host-cell-label">系统模板</span>{{ row.spec?.templateName || '—' }}</td><td><span class="host-cell-label">CPU</span>{{ row.spec?.cpu == null ? '—' : `${row.spec.cpu} 核` }}</td><td><span class="host-cell-label">内存</span>{{ row.spec?.memory || '—' }}</td><td><span class="host-cell-label">系统盘</span>{{ row.spec?.systemDiskGi == null ? '—' : `${row.spec.systemDiskGi} GiB` }}</td><td><span class="host-cell-label">带宽</span>{{ row.spec?.bandwidthMbps == null ? '—' : `${row.spec.bandwidthMbps} Mbps` }}</td><td><span class="status">{{ phaseOf(row) }}</span></td><td class="actions"><button v-if="row.status?.initMode !== 'cloudInit'" class="text" @click="run(`/csms/${encodeURIComponent(nameOf(row))}/preinstall`)">预装</button><button class="text" @click="openHost(row)">详情</button></td></tr></tbody></table></div>
      </section>
      <section v-else-if="!hostRouteName && !['purchase', 'pricing', 'templates', 'versions', 'hosts'].includes(section)" class="panel">
        <div class="panel-head"><span>{{ loading ? '正在加载…' : `${rows.length} 条记录` }}</span></div>
        <div v-if="!loading && rows.length === 0" class="empty">暂无记录。</div>
        <div v-else class="table-wrap execution-table"><table><thead><tr><th>名称 / 订单号</th><th>状态</th></tr></thead><tbody><tr v-for="row in rows" :key="nameOf(row)"><td class="mono">{{ nameOf(row) }}<details v-if="row.status?.log"><summary>初始化日志</summary><pre style="white-space:pre-wrap;overflow-wrap:anywhere">{{ row.status.log }}</pre></details></td><td><span class="status">{{ phaseOf(row) }}</span></td></tr></tbody></table></div>
      </section>
      <HostDetail v-if="section === 'hosts' && hostRouteName && !terminalPage && selected" :host="selected" :versions="versions" :ssh-host="sshHost" :loading="hostLoading" @refresh="loadHost(hostRouteName)" @preinstall="run(`/csms/${encodeURIComponent(hostRouteName)}/preinstall`)" @upgrade="versionName => run(`/csms/${encodeURIComponent(hostRouteName)}/upgrade`, { versionName })" @terminal="openTerminal" @add-key="async (publicKey, done) => { if ((await run(`/csms/${encodeURIComponent(hostRouteName)}/ssh-keys`, { publicKey })) !== undefined) done() }" @add-jump-key="async (publicKey, done) => { if ((await run(`/csms/${encodeURIComponent(hostRouteName)}/jump-keys`, { publicKey })) !== undefined) done() }" />
      <section v-else-if="section === 'hosts' && hostRouteName && !terminalPage && hostLoading" class="panel"><p class="hint">正在加载主机详情…</p></section>
      <section v-if="selected && section === 'disks'" class="panel detail"><div class="panel-head"><h2>{{ nameOf(selected) }}</h2><button class="text" @click="selected = null">关闭</button></div><dl><dt>容量</dt><dd>{{ selected.spec?.storageGi || 0 }} GiB</dd><dt>到期时间</dt><dd>{{ dateText(selected.spec?.expireTime) }}</dd><dt>PVC</dt><dd>{{ selected.status?.pvcName || '待创建' }}</dd><dt>挂载路径</dt><dd>{{ selected.spec?.attached ? selected.spec?.mountPath || '—' : '未挂载' }}</dd></dl><div class="actions" v-if="selected.spec?.paid"><template v-if="!selected.spec?.attached && selected.status?.phase === 'Available'"><label>目标主机<select v-model="mountTarget"><option value="">选择主机</option><option v-for="host in mountHosts.filter(host => host.spec?.paid)" :key="nameOf(host)" :value="nameOf(host)">{{ nameOf(host) }}</option></select></label><button class="light" :disabled="!mountTarget" @click="run(`/data-disks/${encodeURIComponent(nameOf(selected))}/mount`, { csmName: mountTarget })">挂载</button></template><button v-if="selected.spec?.attached" class="light" @click="unmountDisk(selected)">卸载</button></div></section>
      <TerminalModal v-if="hostRouteName" :visible="terminalOpen || terminalPage" :page="terminalPage" :name="hostRouteName" @close="closeTerminal" />
      </template>
    </main>
  </div>
</template>

<style>
:root {
  font-family: 'PingFang SC', 'Microsoft YaHei', 'Segoe UI', sans-serif;
  color: var(--color-text-1, #1d2129);
  background: var(--color-fill-2, #f2f3f5);
  font-size: 14px;
}
* { box-sizing: border-box; }
body { margin: 0; }
button, input, select, textarea { font: inherit; }
button { cursor: pointer; }
button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, a:focus-visible { outline: 2px solid rgb(var(--primary-6, 22, 93, 255)); outline-offset: 2px; }
.shell { display: grid; grid-template-columns: 208px minmax(0, 1fr); grid-template-rows: 60px minmax(calc(100vh - 60px), auto); min-height: 100vh; background: var(--color-fill-2, #f2f3f5); }
.topbar { grid-column: 1 / -1; display: flex; align-items: center; justify-content: space-between; min-width: 0; padding: 0 24px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); background: var(--color-bg-2, #fff); }
.topbar-brand { display: flex; align-items: center; gap: 10px; color: var(--color-text-1, #1d2129); font-family: 'Segoe UI', 'PingFang SC', sans-serif; font-size: 19px; font-weight: 700; letter-spacing: .02em; }
.topbar-brand small { margin-left: 2px; padding-left: 12px; border-left: 1px solid var(--color-border-2, #e5e6eb); color: var(--color-text-3, #86909c); font-family: 'PingFang SC', 'Microsoft YaHei', sans-serif; font-size: 12px; font-weight: 400; letter-spacing: 0; }
.brand-mark { display: grid; align-content: center; gap: 3px; width: 29px; height: 29px; padding: 6px; border-radius: 4px; background: rgb(var(--primary-6, 22, 93, 255)); }
.brand-mark i { display: block; height: 3px; border-radius: 2px; background: #fff; }
.brand-mark i:nth-child(2) { width: 75%; }
.topbar-context { color: var(--color-text-3, #86909c); font-size: 12px; }
.rail { grid-column: 1; grid-row: 2; min-width: 0; background: var(--color-bg-2, #fff); border-right: 1px solid var(--color-border-2, #e5e6eb); }
.brand { padding: 22px 20px 9px; color: var(--color-text-3, #86909c); font-size: 12px; }
.app-menu { padding: 4px 8px; background: transparent; }
.app-menu .arco-menu-item { height: 40px; margin: 2px 0; border-radius: 4px; }
.app-menu .arco-menu-item.arco-menu-selected { background: var(--color-primary-light-1, #e8f3ff); }
.mobile-menu-trigger { display: none; }
main { grid-column: 2; grid-row: 2; min-width: 0; padding: 24px; }
.shell--micro { display: block; }
.shell--micro main { min-height: 100vh; padding: 20px; }
.page-header { display: flex; align-items: end; justify-content: space-between; gap: 16px; min-height: 58px; margin-bottom: 20px; }
.page-header--detail { min-height: 0; margin-bottom: 0; }
.page-heading { min-width: 0; }
.page-header .breadcrumb { min-width: 0; overflow-wrap: anywhere; }
.page-actions-row { display: flex; align-items: center; gap: 8px; margin-left: auto; padding-bottom: 2px; }
h1 { margin: 6px 0 0; color: var(--color-text-1, #1d2129); font-size: 20px; font-weight: 600; line-height: 30px; }
h2 { margin: 0 0 16px; font-size: 16px; font-weight: 600; }
.panel { min-width: 0; margin-bottom: 20px; padding: 20px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 4px; background: var(--color-bg-2, #fff); }
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
.form-panel { padding: 20px; }
.pricing-combinations { max-width: 1100px; margin-bottom: 20px; }
.pricing-period-create { display: flex; align-items: end; flex-wrap: wrap; gap: 12px; margin-bottom: 20px; }
.pricing-period-create label { width: 120px; }
.combination-list table { min-width: 820px; }
.combination-list input { min-width: 80px; }
.page-tabs { display: flex; gap: 20px; margin-bottom: 20px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); overflow-x: auto; }
.page-tabs button { flex: none; padding: 10px 2px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--color-text-2, #4e5969); }
.page-tabs button.active { border-bottom-color: rgb(var(--primary-6, 22, 93, 255)); color: rgb(var(--primary-6, 22, 93, 255)); font-weight: 600; }
.script-field { grid-column: 1 / -1; }
.app-drawer-actions { display: flex; justify-content: flex-end; gap: 10px; }
.quote { color: var(--color-text-2, #4e5969); }
.quote strong { margin-left: 8px; color: var(--color-text-1, #1d2129); font-size: 20px; }
.table-wrap { max-width: 100%; overflow-x: auto; }
.table-wrap .arco-table { min-width: 560px; }
.host-table table { min-width: 900px; }
.host-table td { white-space: nowrap; }
.host-cell-label { display: none; }
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
@media (max-width: 720px) {
  .shell { grid-template-columns: 176px minmax(0, 1fr); }
  .brand { padding: 22px 16px 9px; }
}
@media (max-width: 600px) {
  .shell { display: block; }
  .rail { display: none; }
  .topbar { height: 60px; padding: 0 16px; }
  .topbar-context { display: none; }
  .mobile-menu-trigger { display: inline-flex; align-items: center; justify-content: center; width: 36px; height: 36px; margin-left: -8px; padding: 0; border: 0; background: transparent; color: var(--color-text-1, #1d2129); font-size: 20px; }
  main { min-height: calc(100vh - 60px); padding: 16px; }
  .shell--micro main { min-height: 100vh; padding: 16px; }
  .panel { padding: 16px; }
  .grid { grid-template-columns: 1fr; }
  .page-actions-row > * { min-height: 44px; }
  .page-header { flex-wrap: wrap; }
  .order-panel-head { row-gap: 12px; }
}
input[type='range'] { min-height: 0; padding: 0; border: 0; background: transparent; accent-color: rgb(var(--primary-6, 22, 93, 255)); }
input[type='checkbox'] { width: auto; min-height: 0; padding: 0; accent-color: rgb(var(--primary-6, 22, 93, 255)); }
.table-wrap table { width: 100%; min-width: 560px; border-collapse: collapse; color: var(--color-text-1, #1d2129); text-align: left; }
.order-table table, .disk-table table { min-width: 900px; }
.table-wrap th { padding: 10px 12px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); background: var(--color-fill-2, #f2f3f5); color: var(--color-text-2, #4e5969); font-weight: 500; white-space: nowrap; }
.table-wrap td { padding: 12px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); vertical-align: middle; }
.table-wrap tbody tr:hover { background: var(--color-fill-1, #f7f8fa); }
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
@media (max-width: 600px) {
  .panel-head { flex-wrap: wrap; }
  .inline-form { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; width: 100%; align-items: end; }
  .inline-form label { min-width: 0; }
  .page-tabs { flex-wrap: wrap; overflow: visible; gap: 4px 16px; }
  .table-wrap { overflow-x: visible; }
  .table-wrap table, .table-wrap tbody { display: block; width: 100%; min-width: 0; }
  .table-wrap thead { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .table-wrap tbody tr { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 16px 0; border-bottom: 1px solid var(--color-border-2, #e5e6eb); }
  .table-wrap tbody tr:last-child { border-bottom: 0; }
  .table-wrap td { display: block; min-width: 0; padding: 0; border: 0; white-space: normal; overflow-wrap: anywhere; }
  .table-wrap td:first-child { grid-column: 1 / -1; font-weight: 600; }
  .table-wrap td:last-child { grid-column: 1 / -1; }
  .table-wrap td.actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 4px; padding-top: 8px; border-top: 1px solid var(--color-border-2, #e5e6eb); text-align: right; white-space: normal; }
  .table-wrap td.actions .text { min-height: 40px; }
  .table-wrap td::before { display: block; margin-bottom: 3px; color: var(--color-text-3, #86909c); font-size: 12px; font-weight: 400; }
  .order-table td:nth-child(2)::before { content: '关联资源'; }
  .order-table td:nth-child(3)::before { content: '订单类型'; }
  .order-table td:nth-child(4)::before { content: '资源配置'; }
  .order-table td:nth-child(5)::before { content: '金额'; }
  .order-table td:nth-child(6)::before { content: '状态'; }
  .order-table td:nth-child(7)::before { content: '创建时间'; }
  .disk-table td:nth-child(2)::before { content: '所属主机'; }
  .disk-table td:nth-child(3)::before { content: '容量'; }
  .disk-table td:nth-child(4)::before { content: '到期时间'; }
  .disk-table td:nth-child(5)::before { content: '状态'; }
  .combination-list td:nth-child(1)::before { content: 'CPU（核）'; }
  .combination-list td:nth-child(2)::before { content: '内存（GiB）'; }
  .combination-list td:nth-child(3)::before { content: '存储（GiB）'; }
  .combination-list td:nth-child(4)::before { content: '带宽（Mbps）'; }
  .combination-list td:nth-child(5)::before { content: '新购折扣（%）'; }
  .combination-list td:nth-child(6)::before { content: '续费折扣（%）'; }
  .combination-list input { min-width: 0; }
  .app-config-table td:nth-child(2)::before { content: '执行顺序'; }
  .app-config-table td:nth-child(3)::before { content: '类型'; }
  .app-config-table td:nth-child(4)::before { content: '操作'; }
  .execution-table td:nth-child(2)::before { content: '状态'; }
  .version-page td:nth-child(2)::before { content: '版本号'; }
  .version-page td:nth-child(3)::before { content: '目标模板'; }
  .host-table td:first-child { grid-column: 1; grid-row: 1; font-size: 16px; font-weight: 600; }
  .host-table td:nth-child(7) { grid-column: 2; grid-row: 1; text-align: right; }
  .host-table td:nth-child(2) { grid-column: 1 / -1; color: var(--color-text-2, #4e5969); }
  .host-cell-label { display: block; margin-bottom: 3px; color: var(--color-text-3, #86909c); font-size: 12px; font-weight: 400; }
}
</style>
