import { api, state } from './lib'

// A newly opened clipboard batch must wait for the previous dialog's selection.
let pendingSave = Promise.resolve()

export async function loadSharePreferences() {
  await pendingSave
  return api('/files/share-preferences')
}

export function saveSharePreferences(value) {
  const owner = state.username
  const request = pendingSave.then(() => {
    if (state.authenticated && state.username === owner) return api('/files/share-preferences', 'PUT', value)
  })
  pendingSave = request.catch(() => {})
  return request
}
