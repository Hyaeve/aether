export function documentIcon(name = '') {
  const ext = name.split('.').pop().toLowerCase()
  return ext === 'strm' ? 'FileSymlink' : ext === 'nfo' ? 'FileText' : ''
}

export function entryIcon(file) {
  if(file.isDir)return 'Folder'
  const name=file.name||'',ext=name.split('.').pop().toLowerCase()
  return documentIcon(name) || (/^(mp3|flac|wav|m4a|m4b|aac|ogg|oga|opus|ape|wma|aiff|aif|alac)$/.test(ext)?'FileAudio2':/^(zip|rar|7z|tar|gz|bz2|xz|tgz|tbz2|txz)$/.test(ext)?'FileArchive':/^(jpg|jpeg|png|gif|webp|bmp|avif|svg)$/.test(ext)?'Image':/^(mp4|m4v|mkv|avi|mov|webm|ts|m2ts)$/.test(ext)?'FileVideo':'FileText')
}
