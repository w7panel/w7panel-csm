import assert from 'node:assert/strict'
import test from 'node:test'
import { filterHosts, hostUsername } from './hostFilters.js'

test('host filters use tenant username and exact host phase', () => {
  const rows = [
    { metadata: { namespace: 'csm-alice' }, status: { phase: 'Ready' } },
    { metadata: { namespace: 'csm-alison' }, status: { phase: 'Expired' } },
    { metadata: { namespace: 'csm-bob' }, status: { phase: 'Ready' } },
  ]
  assert.equal(hostUsername(rows[0]), 'alice')
  assert.deepEqual(filterHosts(rows, ' ALI ', 'Ready'), [rows[0]])
  assert.deepEqual(filterHosts(rows, '', 'Expired'), [rows[1]])
})
