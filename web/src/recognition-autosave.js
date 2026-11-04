const writes = new Map()

// A reopened editor waits for writes already accepted from the previous view.
// Serialize immutable snapshots so slower responses cannot reverse their order.
export function queueRecognitionSave(owner, value, write) {
  const snapshot = JSON.parse(JSON.stringify(value))
  const request = (writes.get(owner) || Promise.resolve()).catch(() => {}).then(() => write(snapshot))
  writes.set(owner, request)
  request.finally(() => { if (writes.get(owner) === request) writes.delete(owner) }).catch(() => {})
  return request
}

export async function waitForRecognitionSave(owner) {
  await writes.get(owner)?.catch(() => {})
}
