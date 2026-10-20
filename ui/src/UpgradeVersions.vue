<script setup>
import { ref } from 'vue'
import { api, items } from './api'

defineProps({ rows: { type: Array, required: true }, loading: Boolean })
const emit = defineEmits(['saved'])
const version = ref(null)
const creating = ref(false)
const templates = ref([])
const policies = ref([])
const draftVersion = ref({ name: '', templateName: '', version: '' })
const draftPolicy = ref({ name: '', order: 0, shell: '' })
const policyForm = ref(false)
const saving = ref(false)
const error = ref('')

async function openCreate() {
  error.value = ''
  try { templates.value = items(await api('/system-templates')); creating.value = true }
  catch (e) { error.value = e.message }
}
async function createVersion() {
  saving.value = true; error.value = ''
  try {
    version.value = await api('/upgrade-versions', { method: 'POST', body: JSON.stringify({ metadata: { name: draftVersion.value.name }, spec: { templateName: draftVersion.value.templateName, version: draftVersion.value.version } }) })
    creating.value = false
    draftVersion.value = { name: '', templateName: '', version: '' }
    policies.value = []
    emit('saved')
  } catch (e) { error.value = e.message }
  finally { saving.value = false }
}
async function openVersion(row) {
  error.value = ''; policyForm.value = false
  version.value = row
  try { await loadPolicies() } catch (e) { error.value = e.message }
}
async function loadPolicies() {
  const all = items(await api('/upgrade-policies'))
  policies.value = all.filter(item => item.spec?.versionName === version.value?.metadata?.name)
    .sort((a, b) => (a.spec?.order || 0) - (b.spec?.order || 0))
}
async function createPolicy() {
  saving.value = true; error.value = ''
  try {
    await api('/upgrade-policies', { method: 'POST', body: JSON.stringify({ metadata: { name: draftPolicy.value.name }, spec: { versionName: version.value.metadata.name, enabled: true, order: Number(draftPolicy.value.order), shell: draftPolicy.value.shell } }) })
    draftPolicy.value = { name: '', order: 0, shell: '' }
    policyForm.value = false
    await loadPolicies()
    emit('saved')
  } catch (e) { error.value = e.message }
  finally { saving.value = false }
}
async function removePolicy(row) {
  if (!window.confirm(`确定删除脚本 ${row.metadata.name}？`)) return
  error.value = ''
  try { await api(`/upgrade-policies/${encodeURIComponent(row.metadata.name)}`, { method: 'DELETE' }); await loadPolicies(); emit('saved') }
  catch (e) { error.value = e.message }
}
async function removeVersion(row) {
  if (!window.confirm(`确定删除版本 ${row.metadata.name}？请先删除其下的脚本。`)) return
  error.value = ''
  try { await api(`/upgrade-versions/${encodeURIComponent(row.metadata.name)}`, { method: 'DELETE' }); version.value = null; emit('saved') }
  catch (e) { error.value = e.status === 409 ? '请先删除该版本下的脚本。' : e.message }
}
</script>

<template>
  <section class="panel version-page">
    <p v-if="error" class="alert" role="alert">{{ error }}</p>
    <template v-if="!version">
      <div class="panel-head"><button class="primary" @click="openCreate">＋ 新建版本</button><span>{{ loading ? '正在加载…' : `${rows.length} 个版本` }}</span></div>
      <div v-if="!loading && !rows.length" class="empty">暂无升级版本，请先新建版本。</div>
      <div v-else class="table-wrap"><table><thead><tr><th>版本名称</th><th>版本号</th><th>目标模板</th><th>脚本数</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec?.version }}</td><td>{{ row.spec?.templateName }}</td><td>{{ row.spec?.policies?.length || 0 }}</td><td class="actions"><button class="text" @click="openVersion(row)">配置脚本</button><button class="text danger" @click="removeVersion(row)">删除</button></td></tr></tbody></table></div>
      <form v-if="creating" class="version-form" @submit.prevent="createVersion">
        <div class="panel-head"><h2>新建版本</h2><button type="button" class="text" @click="creating = false">关闭</button></div>
        <div class="grid"><label>版本名称<input v-model.trim="draftVersion.name" required maxlength="63" placeholder="例如 upgrade-v1" /></label><label>版本号<input v-model.trim="draftVersion.version" required placeholder="例如 v1.2.3" /></label><label>目标系统模板<select v-model="draftVersion.templateName" required><option value="">请选择系统模板</option><option v-for="item in templates" :key="item.metadata.name" :value="item.metadata.name">{{ item.metadata.name }}</option></select></label></div>
        <button class="primary" :disabled="saving">{{ saving ? '保存中…' : '创建版本' }}</button>
      </form>
    </template>
    <template v-else>
      <div class="panel-head"><button class="text" @click="version = null; policyForm = false">← 返回版本列表</button><span>版本升级 / {{ version.metadata.name }}</span></div>
      <h2>{{ version.spec?.version }} <small>{{ version.metadata.name }}</small></h2>
      <p class="hint">目标系统模板：{{ version.spec?.templateName }}</p>
      <div class="panel-head"><strong>升级脚本</strong><button class="primary" @click="policyForm = true">＋ 新建脚本</button></div>
      <div v-if="!policies.length" class="empty">暂无脚本，可在此版本下新建。</div>
      <div v-else class="table-wrap"><table><thead><tr><th>名称</th><th>执行顺序</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="row in policies" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec?.order }}</td><td><span class="status">{{ row.spec?.enabled ? '启用' : '停用' }}</span></td><td class="actions"><button class="text danger" @click="removePolicy(row)">删除</button></td></tr></tbody></table></div>
      <form v-if="policyForm" class="version-form" @submit.prevent="createPolicy">
        <div class="panel-head"><h2>新建升级脚本</h2><button type="button" class="text" @click="policyForm = false">关闭</button></div>
        <div class="grid"><label>脚本名称<input v-model.trim="draftPolicy.name" required maxlength="63" placeholder="例如 migrate-data" /></label><label>执行顺序<input v-model.number="draftPolicy.order" type="number" required /></label><label class="script-field">Shell 脚本<textarea v-model="draftPolicy.shell" required rows="8" placeholder="输入升级命令" /></label></div>
        <button class="primary" :disabled="saving">{{ saving ? '保存中…' : '保存脚本' }}</button>
      </form>
    </template>
  </section>
</template>

<style scoped>
.version-page { max-width: 1050px; }
.version-page h2 small { margin-left: 8px; color: var(--color-text-3, #86909c); font-size: 13px; font-weight: 400; }
.version-form { margin-top: 24px; padding: 20px; border: 1px solid var(--color-border-2, #e5e6eb); }
.version-form .script-field { grid-column: 1 / -1; }
.version-form textarea { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
@media (max-width: 600px) { .version-form { padding: 16px; } .version-page .panel-head { gap: 12px; flex-wrap: wrap; } }
</style>
