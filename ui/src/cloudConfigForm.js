export function emptyCloudConfigForm() {
  return { packages: '', commands: '', files: [] }
}

export function cloudConfigFromForm(form) {
  const lines = value => value.split('\n').filter(line => line.trim())
  const config = {}
  const packages = lines(form.packages).map(line => line.trim())
  const commands = lines(form.commands)
  if (packages.length) config.packages = packages
  if (commands.length) config.runcmd = commands
  if (form.files.length) config.write_files = form.files.map(file => ({ path: file.path.trim(), content: file.content }))
  if (!Object.keys(config).length) throw new Error('请至少填写一项初始化配置。')
  if (form.files.some(file => !file.path.trim().startsWith('/'))) throw new Error('文件路径必须以 / 开头。')
  return '#cloud-config\n' + JSON.stringify(config, null, 2) + '\n'
}

// shortcut: Only form-generated JSON YAML round-trips; other YAML stays in the YAML editor until a full parser is needed.
export function formFromCloudConfig(raw) {
  if (!raw.startsWith('#cloud-config\n')) return null
  try {
    const config = JSON.parse(raw.slice('#cloud-config\n'.length))
    if (!config || Array.isArray(config) || typeof config !== 'object' ||
        Object.keys(config).some(key => !['packages', 'runcmd', 'write_files'].includes(key)) ||
        ('packages' in config && (!Array.isArray(config.packages) || !config.packages.every(item => typeof item === 'string'))) ||
        ('runcmd' in config && (!Array.isArray(config.runcmd) || !config.runcmd.every(item => typeof item === 'string'))) ||
        ('write_files' in config && (!Array.isArray(config.write_files) || !config.write_files.every(file =>
          file && typeof file === 'object' && !Array.isArray(file) && Object.keys(file).every(key => ['path', 'content'].includes(key)) && typeof file.path === 'string' && typeof file.content === 'string')))) return null
    return {
      packages: (config.packages || []).join('\n'),
      commands: (config.runcmd || []).join('\n'),
      files: (config.write_files || []).map(file => ({ path: file.path, content: file.content })),
    }
  } catch { return null }
}
