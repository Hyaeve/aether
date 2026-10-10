export function documentIcon(name = '') {
  const ext = name.split('.').pop().toLowerCase()
  return ext === 'strm' ? 'FileVideo2' : ext === 'nfo' ? 'FileText' : ''
}
