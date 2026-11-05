export const emptyApp = () => ({ env: [], containers: [], initContainers: [] })
export const normalizeApp = app => ({
  env: app?.env || [],
  containers: (app?.containers || []).map(item => ({ ...item, command: item.command || [], args: item.args || [], env: item.env || [] })),
  initContainers: (app?.initContainers || []).map(item => ({ ...item, command: item.command || [], args: item.args || [], env: item.env || [] })),
})
const clone = value => JSON.parse(JSON.stringify(value))
const clean = value => {
  if (Array.isArray(value)) {
    const result = value.map(clean).filter(item => item !== undefined)
    return result.length ? result : undefined
  }
  if (value && typeof value === 'object') {
    const result = Object.fromEntries(Object.entries(value).map(([key, item]) => [key, clean(item)]).filter(([, item]) => item !== undefined))
    return Object.keys(result).length ? result : undefined
  }
  return value === null || value === '' ? undefined : value
}
const same = (left, right) => JSON.stringify(clean(left)) === JSON.stringify(clean(right))
const envToKubernetes = env => env.map(item => ({ name: item.name, value: item.value }))
const containerToKubernetes = item => ({ name: item.name, image: item.image, ...(item.command?.length ? { command: item.command } : {}), ...(item.args?.length ? { args: item.args } : {}), ...(item.env?.length ? { env: envToKubernetes(item.env) } : {}) })

export function templateToDeployment(editor) {
  const name = editor.name || 'csm-template'
  const app = editor.app || emptyApp()
  return {
    apiVersion: 'apps/v1', kind: 'Deployment',
    metadata: { name, annotations: { title: name }, labels: { app: name } },
    spec: { selector: { matchLabels: { app: name } }, template: {
      metadata: { labels: { app: name } },
      spec: { containers: [{ name: 'host', image: editor.image, ...(app.env?.length ? { env: envToKubernetes(app.env) } : {}) }, ...(app.containers || []).map(containerToKubernetes)], initContainers: (app.initContainers || []).map(containerToKubernetes) },
    } },
  }
}

function fromEnv(env = []) {
  if (!Array.isArray(env)) throw new Error('环境变量格式无效')
  return env.map(item => {
    if (!item || Object.keys(item).some(key => !['name', 'value'].includes(key)) || typeof item.name !== 'string' || typeof item.value !== 'string') throw new Error('仅支持直接填写的环境变量')
    return { name: item.name, value: item.value }
  })
}
function fromContainer(item, main = false) {
  if (!item || Object.keys(item).some(key => !['name', 'image', 'env', 'command', 'args'].includes(key)) ||
      typeof item.name !== 'string' || typeof item.image !== 'string' ||
      (item.command != null && (!Array.isArray(item.command) || !item.command.every(value => typeof value === 'string'))) ||
      (item.args != null && (!Array.isArray(item.args) || !item.args.every(value => typeof value === 'string')))) throw new Error(`容器 ${item?.name || ''} 包含不支持的字段`)
  if (main && (item.name !== 'host' || item.command?.length || item.args?.length)) throw new Error('CSM 主容器名称和启动命令不可修改')
  return { name: item.name, image: item.image, command: item.command || [], args: item.args || [], env: fromEnv(item.env || []) }
}

export function deploymentToTemplateApp(editor, deployment) {
  const original = templateToDeployment(editor)
  const next = clone(deployment || {})
  const baseline = clone(original)
  const containers = next.spec?.template?.spec?.containers
  const initContainers = next.spec?.template?.spec?.initContainers || []
  if (!Array.isArray(containers) || !containers.length || containers[0]?.name !== 'host' || !Array.isArray(initContainers)) throw new Error('必须保留 CSM 主容器')
  const main = fromContainer(containers[0], true)
  const app = { env: main.env, containers: containers.slice(1).map(item => fromContainer(item)), initContainers: initContainers.map(item => fromContainer(item)) }
  next.spec.template.spec.containers = baseline.spec.template.spec.containers
  next.spec.template.spec.initContainers = baseline.spec.template.spec.initContainers
  delete next.metadata
  delete baseline.metadata
  if (!same(next, baseline)) throw new Error('数据卷、调度及其他应用字段不支持保存到 CSM 系统模板')
  validateApp(app)
  return { image: main.image, app }
}

export function validateApp(app) {
  const names = new Set(['host'])
  const checkEnv = env => {
    const seen = new Set()
    for (const item of env || []) {
      if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(item.name) || item.name === 'CSM_SSH_PUBLIC_KEYS' || seen.has(item.name)) throw new Error(`环境变量 ${item.name || '（空）'} 无效或重复`)
      seen.add(item.name)
    }
  }
  checkEnv(app.env)
  for (const item of [...(app.containers || []), ...(app.initContainers || [])]) {
    if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(item.name) || names.has(item.name) || !item.image?.trim()) throw new Error(`容器 ${item.name || '（空）'} 名称、镜像无效或重复`)
    names.add(item.name)
    checkEnv(item.env)
  }
}
