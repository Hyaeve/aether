import { api, notices, state } from './lib'

export async function transferShares(storage, parent, links, history = []) {
  const owner = state.username
  const id = Date.now() + Math.random()
  notices.push({ id, message: `${storage.name}：准备转存`, progress: 0 })
  const update = values => { const notice = notices.find(n => n.id === id); if (notice) Object.assign(notice, values) }
  const session = () => { if (!state.authenticated || state.username !== owner) throw new Error('登录状态已变化，转存已停止') }
  try {
    for (let i = 0; i < links.length; i++) {
      session()
      const plan = await api('/files/share/preview', 'POST', { storageId: storage.id, url: links[i] })
      for (let batch = 0; batch < plan.batches; batch++) {
        session()
        update({ message: `${storage.name}：链接 ${i + 1}/${links.length}，批次 ${batch + 1}/${plan.batches}` })
        const result = await api('/files/share/batch', 'POST', { storageId: storage.id, preview: plan.preview, parent, batch })
        if (storage.type === 'quark' && result.taskId) {
          let complete = false
          for (let poll = 0; poll < 120; poll++) {
            await new Promise(resolve => setTimeout(resolve, 2000))
            session()
            const status = await api('/files/share/status', 'POST', { storageId: storage.id, preview: plan.preview })
            if (status.status === 'failed') throw new Error(status.message)
            if (status.status === 'completed') { complete = true; break }
          }
          if (!complete) throw new Error('网盘仍在处理，已停止后续批次，请检查目标目录，勿重复提交')
        }
        update({ progress: (i + (batch + 1) / plan.batches) / links.length })
      }
      window.dispatchEvent(new CustomEvent('aether-files-changed', { detail: { storageId: storage.id } }))
    }
    session()
    update({ progress: 1, message: `${storage.name}：${storage.type === 'quark' ? '转存已完成' : '全部批次已提交，请检查网盘结果'}` })
    window.dispatchEvent(new CustomEvent('aether-files-changed', { detail: { storageId: storage.id, owner, destination: parent, history } }))
  } catch (e) { update({ error: true, message: e.message }) }
  finally { setTimeout(() => { const index = notices.findIndex(n => n.id === id); if (index >= 0) notices.splice(index, 1) }, 12000) }
}
