import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {discoverTests, partitionTests, shardArguments, verifyResults} from './go-test-shards.mjs'

const event = value => JSON.stringify(value)
const listed = (pkg, ...names) => names.map(name => event({Action:'output',Package:pkg,Output:`${name}\n`})).join('\n')
const finished = tests => tests.map(({pkg,name}) => event({Action:'pass',Package:pkg,Test:name})).join('\n')

test('discovery includes tests, examples and fuzz seeds but not benchmark-only workloads', () => {
  const result = discoverTests([
    event({Action:'start',Package:'aether/a'}),
    listed('aether/a','TestFirst','Example','ExampleRead','FuzzParse','BenchmarkScan'),
    event({Action:'output',Package:'aether/a',Output:'ok\taether/a\t0.1s\n'}),
    event({Action:'pass',Package:'aether/a'}),
  ].join('\n'))
  assert.deepEqual(result.map(t => t.name), ['Example','ExampleRead','FuzzParse','TestFirst'])
  assert.throws(() => discoverTests('not JSON'), /JSON/)
  assert.throws(() => discoverTests(event({Action:'fail',Package:'aether/a'})), /discovery failed/)
  assert.throws(() => discoverTests(event({Action:'pass',Package:'aether/a'})), /No tests/)
  assert.throws(() => discoverTests(listed('aether/a','TestFirst','TestFirst')), /Duplicate/)
})

test('all package and test pairs belong to exactly one deterministic shard', () => {
  const tests = discoverTests([listed('aether/a',...Array.from({length:604},(_,i)=>`TestCase${i}`)),listed('aether/b','TestCase0','ExampleOutput','FuzzInput')].join('\n'))
  const shards = partitionTests(tests,8)
  const keys = shards.flat().map(t => `${t.pkg}/${t.name}`)
  assert.equal(keys.length,tests.length)
  assert.equal(new Set(keys).size,tests.length)
  assert.deepEqual(new Set(keys),new Set(tests.map(t => `${t.pkg}/${t.name}`)))
  assert.deepEqual(partitionTests([...tests].reverse(),8),shards)
  const containing = shards.filter(s => s.some(t => t.name === 'TestCase0'))
  assert.equal(containing.length,1)
  assert.equal(containing[0].filter(t => t.name === 'TestCase0').length,2)
  assert.ok(Math.max(...shards.map(s=>s.length))-Math.min(...shards.map(s=>s.length))<=2)
  for(const invalid of [0,-1,1.5,65,NaN]) assert.throws(()=>partitionTests(tests,invalid))
})

test('shard commands retain race, all packages, default timeout and every selected subtest', () => {
  const shards=partitionTests(discoverTests(listed('aether/a','TestFirst','TestSecond','ExampleOutput','FuzzParse')),2)
  for(const tests of shards){
    const args=shardArguments(tests)
    assert.equal(args[0],'test')
    assert.ok(args.includes('-race'))
    assert.ok(args.includes('-count=1'))
    assert.ok(args.includes('-json'))
    assert.equal(args.at(-1),'./...')
    assert.ok(!args.some(a=>/timeout|skip|short|parallel/.test(a)))
    const run=args.find(a=>a.startsWith('-run=')).slice(5)
    assert.ok(run.startsWith('^(')&&run.endsWith(')$'))
    assert.ok(!run.includes('/'))
    for(const test of tests)assert.ok(new RegExp(run).test(test.name))
    for(const other of shards.flat().filter(t=>!tests.some(s=>s.name===t.name)))assert.ok(!new RegExp(run).test(other.name))
    assert.ok(!shardArguments(tests,false).includes('-race'))
  }
  assert.throws(()=>shardArguments([]),/empty/)
})

test('successful Go exit still fails coverage audit when any discovered test is missing', () => {
  const tests=discoverTests(listed('aether/a','TestFirst','TestSecond','ExampleOutput','FuzzParse'))
  verifyResults(finished(tests),tests)
  const skipping=tests.map((t,i)=>event({Action:i===0?'skip':'pass',Package:t.pkg,Test:t.name})).join('\n')
  verifyResults(skipping,tests)
  verifyResults(`${finished(tests)}\n${event({Action:'pass',Package:'aether/a',Test:'TestFirst/nested'})}`,tests)
  assert.throws(()=>verifyResults(finished(tests.slice(1)),tests),/Missing/)
  assert.throws(()=>verifyResults(`${finished(tests)}\n${listed('aether/a','TestAnother')}\n${event({Action:'pass',Package:'aether/a',Test:'TestAnother'})}`,tests),/Unexpected/)
  assert.throws(()=>verifyResults(`${finished(tests)}\n${event({Action:'fail',Package:'aether/a'})}`,tests),/failed/)
  assert.throws(()=>verifyResults(`${finished(tests)}\n${finished(tests.slice(0,1))}`,tests),/Duplicate/)
})

test('image publication waits for every race shard and CI always uploads Go diagnostics', () => {
  const workflow=readFileSync(new URL('../.github/workflows/docker-amd64.yml',import.meta.url),'utf8')
  assert.match(workflow,/go-tests:[\s\S]*fail-fast: false/)
  assert.match(workflow,/shard: \[0, 1, 2, 3, 4, 5, 6, 7\]/)
  assert.match(workflow,/node scripts\/go-test-shards\.mjs --index \$\{\{ matrix\.shard \}\} --count 8/)
  assert.doesNotMatch(workflow,/--no-race|-timeout|continue-on-error: true/)
  assert.match(workflow,/if: always\(\)[\s\S]*go-test-results-/)
  assert.match(workflow,/image:[\s\S]*needs: \[verify, go-tests\]/)
})
