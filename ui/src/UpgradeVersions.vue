<script setup>
import { ref } from 'vue'
import { api, items } from './api'

defineProps({ rows: { type: Array, required: true }, loading: Boolean })
const emit = defineEmits(['saved'])
const creating = ref(false)
const templates = ref([])
const draft = ref({ name: '', templateName: '', version: '' })
const saving = ref(false)
const error = ref('')
const selected = ref(null)
const view = ref('')
const policies = ref([])
const executions = ref([])
const policyForm = ref(null)
const policyDrawerOpen = ref(false)
const policySaving = ref(false)
const formError = ref('')

async function openCreate() {
  error.value = ''
  formError.value = ''
  try { templates.value = items(await api('/system-templates')); draft.value = { name: '', templateName: '', version: '' }; creating.value = true }
  catch (e) { error.value = e.message }
}
async function createVersion() {
  saving.value = true; formError.value = ''
  try {
    await api('/upgrade-versions', { method: 'POST', body: JSON.stringify({ metadata: { name: draft.value.name }, spec: { templateName: draft.value.templateName, version: draft.value.version } }) })
    creating.value = false
    draft.value = { name: '', templateName: '', version: '' }
    emit('saved')
  } catch (e) { formError.value = e.message }
  finally { saving.value = false }
}
async function removeVersion(row) {
  if (!window.confirm(`确定删除版本 ${row.metadata.name}？`)) return
  error.value = ''
  try { await api(`/upgrade-versions/${encodeURIComponent(row.metadata.name)}`, { method: 'DELETE' }); emit('saved') }
  catch (e) { error.value = e.message }
}
async function openView(row, next) {
  selected.value = row; view.value = next; policyForm.value = null; policyDrawerOpen.value = false; error.value = ''
  policies.value = []; executions.value = []
  try {
    if (next === 'policies') policies.value = items(await api('/upgrade-policies')).filter(item => item.spec?.versionName === row.metadata.name)
    else executions.value = items(await api('/upgrade-executions')).filter(item => item.spec?.versionName === row.metadata.name)
  } catch (e) { error.value = e.message }
}
function editPolicy(row) {
  policyForm.value = row ? { name: row.metadata.name, resourceVersion: row.metadata.resourceVersion, enabled: !!row.spec.enabled, order: row.spec.order ?? 0, shell: row.spec.shell || '' } : { name: '', resourceVersion: '', enabled: true, order: policies.value.length, shell: '' }
  formError.value = ''
  policyDrawerOpen.value = true
}
async function savePolicy() {
  policySaving.value = true; formError.value = ''
  try {
    const form = policyForm.value
    await api(form.resourceVersion ? `/upgrade-policies/${encodeURIComponent(form.name)}` : '/upgrade-policies', { method: form.resourceVersion ? 'PUT' : 'POST', body: JSON.stringify({ metadata: { name: form.name, resourceVersion: form.resourceVersion }, spec: { versionName: selected.value.metadata.name, enabled: form.enabled, order: form.order, shell: form.shell } }) })
    policyForm.value = null
    await openView(selected.value, 'policies')
  } catch (e) { formError.value = e.message }
  finally { policySaving.value = false }
}
async function removePolicy(row) {
  if (!window.confirm(`确定删除脚本 ${row.metadata.name}？`)) return
  error.value = ''
  try { await api(`/upgrade-policies/${encodeURIComponent(row.metadata.name)}`, { method: 'DELETE' }); await openView(selected.value, 'policies') }
  catch (e) { error.value = e.message }
}
function time(value) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—' }
const phase = { running: '执行中', applying: '应用中', succeeded: '成功', failed: '失败' }
</script>

<template>
  <section class="panel version-page">
    <p v-if="error" class="alert" role="alert">{{ error }}</p>
    <div class="panel-head"><button class="primary" @click="openCreate">＋ 新建版本</button><span>{{ loading ? '正在加载…' : `${rows.length} 个版本` }}</span></div>
    <div v-if="!loading && !rows.length" class="empty">暂无升级版本，请先新建版本。</div>
    <div v-else class="table-wrap"><table><thead><tr><th>版本名称</th><th>版本号</th><th>目标模板</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec?.version }}</td><td>{{ row.spec?.templateName }}</td><td class="actions"><button class="text" @click="openView(row, 'policies')">升级脚本</button><button class="text" @click="openView(row, 'executions')">执行记录</button><button class="text danger" @click="removeVersion(row)">删除</button></td></tr></tbody></table></div>
    <div v-if="selected" class="version-detail">
      <div class="panel-head"><h2>{{ selected.metadata.name }} · {{ view === 'policies' ? '升级脚本' : '执行记录' }}</h2><button class="text" @click="selected = null">关闭</button></div>
      <template v-if="view === 'policies'">
        <button class="primary" @click="editPolicy(null)">＋ 新增脚本</button>
        <div v-if="!policies.length" class="empty">暂无升级脚本。</div>
        <div v-else class="table-wrap policy-table"><table><thead><tr><th>名称</th><th>顺序</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in policies" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec.order }}</td><td>{{ row.spec.enabled ? '启用' : '停用' }}</td><td class="actions"><button class="text" @click="editPolicy(row)">编辑</button><button class="text danger" @click="removePolicy(row)">删除</button></td></tr></tbody></table></div>
      </template>
      <template v-else>
        <button class="light" @click="openView(selected, 'executions')">刷新记录</button>
        <div v-if="!executions.length" class="empty">暂无执行记录。</div>
        <div v-else class="table-wrap record-table"><table><thead><tr><th>执行名称</th><th>主机</th><th>发起人</th><th>状态</th><th>开始时间</th><th>完成时间</th></tr></thead><tbody><tr v-for="row in executions" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec.csmNamespace }} / {{ row.spec.csmName }}</td><td>{{ row.spec.approvedBy }}</td><td>{{ phase[row.status?.phase] || row.status?.phase || '待执行' }}</td><td>{{ time(row.status?.startedAt || row.metadata.creationTimestamp) }}</td><td>{{ time(row.status?.completedAt) }}<details v-if="row.status?.log || row.status?.message"><summary>查看日志</summary><pre>{{ row.status.log || row.status.message }}</pre></details></td></tr></tbody></table></div>
      </template>
    </div>
  </section>
  <a-drawer v-model:visible="creating" title="新建版本" width="min(560px, 100vw)" :footer="false" :unmount-on-close="true">
    <form class="version-form" @submit.prevent="createVersion">
      <p v-if="formError" class="alert" role="alert">{{ formError }}</p>
      <div class="grid"><label>版本名称<input v-model.trim="draft.name" required maxlength="63" placeholder="例如 cloud-init-1.0.3" /></label><label>版本号<input v-model.trim="draft.version" required placeholder="例如 1.0.3" /></label><label>目标系统模板<select v-model="draft.templateName" required><option value="">请选择系统模板</option><option v-for="item in templates" :key="item.metadata.name" :value="item.metadata.name">{{ item.metadata.name }}</option></select></label></div>
      <p class="hint">cloud-init 仅在首次启动时运行；升级到 cloud-init 模板要求源模板也是 cloud-init，升级改动请配置脚本。</p>
      <div class="drawer-actions"><button type="button" class="light" @click="creating = false">取消</button><button class="primary" :disabled="saving">{{ saving ? '保存中…' : '创建版本' }}</button></div>
    </form>
  </a-drawer>
  <a-drawer v-model:visible="policyDrawerOpen" :title="policyForm?.resourceVersion ? '编辑脚本' : '新增脚本'" width="min(560px, 100vw)" :footer="false" :unmount-on-close="true">
    <form v-if="policyForm" class="version-form" @submit.prevent="savePolicy"><p v-if="formError" class="alert" role="alert">{{ formError }}</p><div class="grid"><label>脚本名称<input v-model.trim="policyForm.name" required maxlength="63" :readonly="!!policyForm.resourceVersion" /></label><label>执行顺序<input v-model.number="policyForm.order" type="number" required /></label><label class="policy-enabled"><input v-model="policyForm.enabled" type="checkbox" />启用</label><label class="script-field">Shell 脚本<textarea v-model="policyForm.shell" required rows="10" spellcheck="false" /></label></div><div class="drawer-actions"><button type="button" class="light" @click="policyDrawerOpen = false">取消</button><button class="primary" :disabled="policySaving">{{ policySaving ? '保存中…' : '保存脚本' }}</button></div></form>
  </a-drawer>
</template>

<style scoped>
.version-detail{margin-top:24px;padding:20px;background:var(--color-bg-2,#fff)}
.drawer-actions{display:flex;justify-content:flex-end;gap:10px;flex-wrap:wrap}
.version-detail>.panel-head h2{overflow-wrap:anywhere}
.version-detail>.primary,.version-detail>.light{margin-bottom:12px}
.policy-enabled{display:flex;align-items:center;gap:8px}.policy-enabled input{width:auto}
pre{max-width:100%;white-space:pre-wrap;overflow-wrap:anywhere}
@media(max-width:600px){.version-detail{padding:16px}.version-page .panel-head{gap:12px;flex-wrap:wrap}.record-table td:nth-child(2)::before{content:'主机'}.record-table td:nth-child(3)::before{content:'发起人'}.record-table td:nth-child(4)::before{content:'状态'}.record-table td:nth-child(5)::before{content:'开始时间'}.record-table td:nth-child(6)::before{content:'完成时间'}.policy-table td:nth-child(2)::before{content:'顺序'}.policy-table td:nth-child(3)::before{content:'状态'}}
</style>
