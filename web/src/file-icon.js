export function documentIcon(name = '') {
  const ext = name.split('.').pop().toLowerCase()
  return ext === 'strm' ? 'FileSymlink' : ext === 'nfo' ? 'FileText' : ''
}
