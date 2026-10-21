import {shareEntries} from './share-links.js'
import {state} from './lib.js'
import {copyText} from './clipboard.js'
const memory=new Map()
const key=entry=>{const url=new URL(entry.url);return `${entry.provider}:${url.pathname}:${url.hash.split('?')[0]}`}
function records(){const owner=state.username||'admin';if(!memory.has(owner)){let entries=[];try{entries=JSON.parse(localStorage.getItem(`aether-own-shares:${owner}`)||'[]')}catch{}memory.set(owner,new Set(Array.isArray(entries)?entries:[]))}return memory.get(owner)}
export function rememberOwnShare(text){const values=records();shareEntries(text).filter(e=>e.provider).forEach(e=>values.add(key(e)));while(values.size>1000)values.delete(values.values().next().value);try{localStorage.setItem(`aether-own-shares:${state.username||'admin'}`,JSON.stringify([...values]))}catch{}}
export function withoutOwnShares(text){return shareEntries(text).filter(e=>e.provider&&!records().has(key(e))).map(e=>e.link).join('\n')}
export function shareText(item,provider){return provider==='115'?`${item.url}\n${item.name}\n访问码：${item.code||''}\n复制这段内容可在115-Desktop中打开！`:`${item.name}\n链接: ${item.url}${item.code?`\n提取码: ${item.code}`:''}`}
export async function copyOwnShare(item,provider,content=true){let link=item.url;if(!content&&['115','quark'].includes(provider)&&item.code){const url=new URL(link);url.searchParams.set(provider==='115'?'password':'pwd',item.code);link=url.href}const text=content?shareText(item,provider):link;rememberOwnShare(item.url);await copyText(text)}
