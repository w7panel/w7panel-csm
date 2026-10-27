export const hostUsername = row => (row.metadata?.namespace || '').replace(/^csm-/, '')

export function filterHosts(rows, username, phase) {
  const query = username.trim().toLowerCase()
  return rows.filter(row => (!query || hostUsername(row).toLowerCase().includes(query)) && (!phase || row.status?.phase === phase))
}
