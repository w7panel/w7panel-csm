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

async function openCreate() {
  error.value = ''
  try { templates.value = items(await api('/system-templates')); creating.value = true }
  catch (e) { error.value = e.message }
}
async function createVersion() {
  saving.value = true; error.value = ''
  try {
    await api('/upgrade-versions', { method: 'POST', body: JSON.stringify({ metadata: { name: draft.value.name }, spec: { templateName: draft.value.templateName, version: draft.value.version } }) })
    creating.value = false
    draft.value = { name: '', templateName: '', version: '' }
    emit('saved')
  } catch (e) { error.value = e.message }
  finally { saving.value = false }
}
async function removeVersion(row) {
  if (!window.confirm(`确定删除版本 ${row.metadata.name}？`)) return
  error.value = ''
  try { await api(`/upgrade-versions/${encodeURIComponent(row.metadata.name)}`, { method: 'DELETE' }); emit('saved') }
  catch (e) { error.value = e.message }
}
</script>

<template>
  <section class="panel version-page">
    <p v-if="error" class="alert" role="alert">{{ error }}</p>
    <div class="panel-head"><button class="primary" @click="openCreate">＋ 新建版本</button><span>{{ loading ? '正在加载…' : `${rows.length} 个版本` }}</span></div>
    <div v-if="!loading && !rows.length" class="empty">暂无升级版本，请先新建版本。</div>
    <div v-else class="table-wrap"><table><thead><tr><th>版本名称</th><th>版本号</th><th>目标模板</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="row.metadata.name"><td class="mono">{{ row.metadata.name }}</td><td>{{ row.spec?.version }}</td><td>{{ row.spec?.templateName }}</td><td class="actions"><button class="text danger" @click="removeVersion(row)">删除</button></td></tr></tbody></table></div>
    <form v-if="creating" class="version-form" @submit.prevent="createVersion">
      <div class="panel-head"><h2>新建版本</h2><button type="button" class="text" @click="creating = false">关闭</button></div>
      <div class="grid"><label>版本名称<input v-model.trim="draft.name" required maxlength="63" placeholder="例如 upgrade-v1" /></label><label>版本号<input v-model.trim="draft.version" required placeholder="例如 v1.2.3" /></label><label>目标系统模板<select v-model="draft.templateName" required><option value="">请选择系统模板</option><option v-for="item in templates" :key="item.metadata.name" :value="item.metadata.name">{{ item.metadata.name }}</option></select></label></div>
      <button class="primary" :disabled="saving">{{ saving ? '保存中…' : '创建版本' }}</button>
    </form>
  </section>
</template>

<style scoped>
.version-form { margin-top: 24px; padding: 20px; border: 1px solid var(--csm-border); border-radius: 8px; background: #f9fbfe; }
@media (max-width: 600px) { .version-form { padding: 16px; } .version-page .panel-head { gap: 12px; flex-wrap: wrap; } }
</style>
