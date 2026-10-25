import {spawnSync} from 'node:child_process'
import {closeSync, mkdirSync, openSync, readFileSync, writeFileSync} from 'node:fs'
import path from 'node:path'
import {fileURLToPath} from 'node:url'

function events(output) {
  return output.split(/\r?\n/).filter(line=>line.trim()).map(line=>{
    try { return JSON.parse(line) }
    catch { throw new Error(`Invalid Go JSON event: ${line.slice(0,200)}`) }
  })
}

export function discoverTests(output) {
  const tests=new Map()
  for(const event of events(output)){
    if(event.Action==='fail')throw new Error(`Go discovery failed: ${event.Package}`)
    if(event.Action!=='output'||!event.Package)continue
    const name=event.Output?.trim()
    if(!/^(Test|Example|Fuzz)[\p{L}\p{N}_]*$/u.test(name))continue
    const key=`${event.Package}/${name}`
    if(tests.has(key))throw new Error(`Duplicate discovered test: ${key}`)
    tests.set(key,{pkg:event.Package,name})
  }
  if(!tests.size)throw new Error('No tests discovered')
  return [...tests.values()].sort((a,b)=>a.name.localeCompare(b.name,'en')||a.pkg.localeCompare(b.pkg,'en'))
}

export function partitionTests(tests,count) {
  if(!Number.isSafeInteger(count)||count<1||count>64)throw new Error('Shard count must be 1..64')
  const names=[...new Set(tests.map(t=>t.name))].sort()
  const assignments=new Map(names.map((name,i)=>[name,i%count]))
  const shards=Array.from({length:count},()=>[])
  // Equal names in different packages must share a shard: -run applies to ./....
  for(const test of [...tests].sort((a,b)=>a.name.localeCompare(b.name,'en')||a.pkg.localeCompare(b.pkg,'en'))){
    shards[assignments.get(test.name)].push(test)
  }
  return shards
}

export function shardArguments(tests,race=true) {
  if(!tests.length)throw new Error('Refusing an empty shard')
  const names=[...new Set(tests.map(t=>t.name))]
  const escaped=names.map(name=>name.replace(/[.*+?^${}()|[\]\\]/g,'\\$&'))
  return ['test',...(race?['-race']:[]),'-count=1','-json',`-run=^(${escaped.join('|')})$`,'./...']
}

export function verifyResults(output,tests) {
  const expected=new Set(tests.map(t=>`${t.pkg}/${t.name}`)),finished=new Set()
  for(const event of events(output)){
    if(event.Action==='fail')throw new Error(`Go test failed: ${event.Package}/${event.Test||''}`)
    if(!['pass','skip'].includes(event.Action)||!event.Test||event.Test.includes('/'))continue
    const key=`${event.Package}/${event.Test}`
    if(!expected.has(key))throw new Error(`Unexpected test: ${key}`)
    if(finished.has(key))throw new Error(`Duplicate test result: ${key}`)
    finished.add(key)
  }
  const missing=[...expected].filter(key=>!finished.has(key))
  if(missing.length)throw new Error(`Missing test results: ${missing.join(', ')}`)
}

function main(args) {
  let index,count,output,race=true,plan=false
  for(let i=0;i<args.length;i++){
    switch(args[i]){
      case '--index': index=Number(args[++i]);break
      case '--count': count=Number(args[++i]);break
      case '--output': output=args[++i];break
      case '--no-race': race=false;break
      case '--plan': plan=true;break
      default: throw new Error(`Unknown option: ${args[i]}`)
    }
  }
  if(!Number.isSafeInteger(index)||!Number.isSafeInteger(count)||index<0||index>=count||count>64){
    throw new Error('Usage: --index 0 --count 8 [--output DIRECTORY] [--plan] [--no-race]')
  }
  const root=fileURLToPath(new URL('../',import.meta.url))
  const dir=path.resolve(root,output||`test-results/go-shard-${index}`)
  mkdirSync(dir,{recursive:true})
  const discovery=spawnSync('go',['test',...(race?['-race']:[]),'-list','.','-json','./...'],{
    cwd:root,encoding:'utf8',maxBuffer:16<<20,
  })
  writeFileSync(path.join(dir,'discovery.jsonl'),discovery.stdout||'')
  writeFileSync(path.join(dir,'discovery-stderr.txt'),discovery.stderr||'')
  if(discovery.error)throw discovery.error
  if(discovery.status!==0){
    process.stdout.write(discovery.stdout||'')
    process.stderr.write(discovery.stderr||'')
    return discovery.status||1
  }
  const tests=discoverTests(discovery.stdout),shards=partitionTests(tests,count),selected=shards[index]
  const command=shardArguments(selected,race)
  const manifest={index,count,race,totalTests:tests.length,shardSizes:shards.map(s=>s.length),selected,command}
  writeFileSync(path.join(dir,'manifest.json'),JSON.stringify(manifest,null,2))
  console.log(`Go shard ${index+1}/${count}: ${selected.length}/${tests.length} tests; race=${race}`)
  if(plan)return 0
  const stdout=path.join(dir,'tests.jsonl'),stderr=path.join(dir,'stderr.txt')
  const out=openSync(stdout,'w'),err=openSync(stderr,'w')
  const start=Date.now()
  let result
  try { result=spawnSync('go',command,{cwd:root,stdio:['inherit',out,err]}) }
  finally { closeSync(out);closeSync(err) }
  process.stdout.write(readFileSync(stdout))
  process.stderr.write(readFileSync(stderr))
  if(result.error)throw result.error
  if(result.status!==0)return result.status||1
  // A zero exit code is insufficient if a future filter accidentally omits a test.
  verifyResults(readFileSync(stdout,'utf8'),selected)
  writeFileSync(path.join(dir,'result.json'),JSON.stringify({passed:true,tests:selected.length,seconds:(Date.now()-start)/1000}))
  return 0
}

if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)){
  try { process.exitCode=main(process.argv.slice(2)) }
  catch(error){ console.error(error.message);process.exitCode=1 }
}
