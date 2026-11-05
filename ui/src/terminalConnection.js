export function terminalConnection(name, namespace, token, props = {}, origin = window.location.origin) {
  const path = `${(props.url || '').replace(/\/$/, '')}/csm-api/v1/csms/${encodeURIComponent(name)}/terminal`
  const url = new URL(path, origin)
  if (namespace) url.searchParams.set('namespace', namespace)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  const protocols = ['v4.channel.k8s.io']
  const panelToken = props.paneltoken || (props.url ? localStorage.getItem('panelToken') : '')
  if (panelToken) protocols.push(`w7panel-bearer.${panelToken}`)
  protocols.push(`bearer.${token}`)
  return { url: url.href, protocols }
}
