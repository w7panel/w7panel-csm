<script setup>
import { ref } from 'vue'
import { api, items } from './api'

defineProps({ rows: { type: Array, required: true }, loading: Boolean })
const emit = defineEmits(['saved', 'navigate'])
const editor = ref(null)
const applications = ref([])
const saving = ref(false)
const error = ref('')

async function open(row) {
  error.value = ''
  try {
    applications.value = items(await api('/preinstall-applications'))
    editor.value = row ? {
      name: row.metadata.name,
      resourceVersion: row.metadata.resourceVersion,
      version: row.spec.version,
      source: row.spec.source,
      image: row.spec.image,
      preinstallApplications: [...(row.spec.preinstallApplications || [])],
    } : { name: '', resourceVersion: '', version: '', source: 'manual', image: '', preinstallApplications: [] }
  } catch (e) { error.value = e.message }
}

async function save() {
  if (editor.value.resourceVersion && !window.confirm('保存后，使用此模板的云主机可能自动重建并短暂中断。确定继续？')) return
  saving.value = true
  error.value = ''
  const { name, resourceVersion, ...spec } = editor.value
  try {
    await api(resourceVersion ? `/system-templates/${encodeURIComponent(name)}` : '/system-templates', {
      method: resourceVersion ? 'PUT' : 'POST',
      body: JSON.stringify({ metadata: { name, resourceVersion }, spec }),
    })
    editor.value = null
    emit('saved')
  } catch (e) { error.value = e.status === 409 ? '模板已被修改或配置有误，请刷新后重试。' : e.message }
  finally { saving.value = false }
}
</script>

<template>
  <section class="panel template-page">
    <div class="panel-head"><button class="primary" @click="open()">＋ 新建系统模板</button><span>{{ loading ? '正在加载…' : `${rows.length} 个模板` }}</span></div>
    <p class="hint">修改镜像会自动更新使用该模板的云主机；预装应用配置用于后续首次执行。</p>
    <div v-if="!loading && !rows.length" class="empty">暂无系统模板</div>
    <div v-for="row in rows" :key="row.metadata.name" class="template-row">
      <div><strong>{{ row.metadata.name }}</strong><span class="status">{{ row.spec.source === 'official' ? '官方' : '手动' }}</span><p>{{ row.spec.version }} · {{ row.spec.image }}</p><small>{{ row.spec.preinstallApplications?.length || 0 }} 项预装应用</small></div>
      <button class="light" @click="open(row)">编辑配置</button>
    </div>
  </section>
  <section v-if="editor" class="panel template-editor">
    <div class="panel-head"><h2>{{ editor.resourceVersion ? `编辑 ${editor.name}` : '新建系统模板' }}</h2><button class="text" @click="editor = null">关闭</button></div>
    <p class="hint">镜像须为可启动系统服务的 CSM 系统镜像。</p>
    <p v-if="error" class="alert" role="alert">{{ error }}</p>
    <form @submit.prevent="save">
      <div class="grid">
        <label>模板名称<input v-model.trim="editor.name" required :readonly="!!editor.resourceVersion" maxlength="63" placeholder="例如 ubuntu-24.04" /></label>
        <label>版本<input v-model.trim="editor.version" required placeholder="例如 24.04" /></label>
        <label>来源<select v-model="editor.source" :disabled="!!editor.resourceVersion"><option v-if="editor.resourceVersion" value="official">官方</option><option value="manual">手动</option></select></label>
        <label>系统镜像<input v-model.trim="editor.image" required placeholder="registry.example.com/csm-system:latest" /></label>
      </div>
      <fieldset><legend>预装应用</legend><p v-if="!applications.length" class="hint">暂无初始化配置，可从“初始化配置”页新增。</p><label v-for="app in applications" :key="app.metadata.name" class="template-check"><input v-model="editor.preinstallApplications" type="checkbox" :value="app.metadata.name" />{{ app.metadata.name }}</label></fieldset>
      <div class="template-actions"><button type="button" class="light" @click="emit('navigate', 'apps')">管理初始化配置</button><button type="button" class="light" @click="editor = null">取消</button><button class="primary" :disabled="saving">{{ saving ? '保存中…' : '保存修改' }}</button></div>
    </form>
  </section>
  <p v-else-if="error" class="alert" role="alert">{{ error }}</p>
</template>

<style scoped>
.template-row { display: flex; justify-content: space-between; align-items: center; gap: 20px; padding: 18px 0; border-top: 1px solid var(--csm-border); }
.template-row:first-of-type { border-top: 0; }
.template-row strong { margin-right: 12px; color: var(--csm-ink); font-size: 16px; }
.template-row p { margin: 7px 0; color: var(--csm-muted); overflow-wrap: anywhere; }
.template-row small { color: var(--csm-muted); }
.template-row button { flex: none; }
.template-editor fieldset { margin: 0 0 20px; padding: 14px; border: 1px solid var(--csm-border); border-radius: 7px; }
.template-editor legend { color: var(--csm-muted); font-size: 13px; }
.template-check { display: inline-flex; flex-direction: row; align-items: center; margin: 8px 18px 8px 0; }
.template-check input { min-width: auto; }
.template-actions { display: flex; justify-content: flex-end; gap: 10px; flex-wrap: wrap; }
@media (max-width: 700px) { .template-row { align-items: flex-start; flex-direction: column; gap: 10px; } .template-row button { width: 100%; } .template-actions > * { flex: 1; } }
</style>
