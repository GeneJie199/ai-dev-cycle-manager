const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../internal/web/static/module-nav.js'), 'utf8');
for (const origin of ['http://127.0.0.136:8766/team.html?x=1#details', 'http://127.0.0.168:8766/team.html', 'http://[::1]:8766/team.html', 'https://localhost:8766/team.html']) {
  test(`module links preserve host for ${origin}`, async () => {
    const links = [8765, 8770, 8771].map(port => ({dataset: {modulePort: String(port)},removeAttribute(name){delete this[name]}}));
    vm.runInNewContext(source, {URL, fetch: () => Promise.reject(new Error('standalone default')), window: {location: new URL(origin)}, document: {querySelectorAll: () => links}});
    await new Promise(resolve=>setImmediate(resolve));
    for (const link of links) {
      const actual = new URL(link.href), expected = new URL(origin);
      assert.equal(actual.hostname, expected.hostname);
      assert.equal(actual.protocol, expected.protocol);
      assert.equal(actual.port, link.dataset.modulePort);
      assert.equal(actual.pathname, '/');
      assert.equal(actual.hash, '');
      assert.equal(actual.searchParams.get('suite-devcycle'),new URL('/',origin).href);
      assert.equal(new URL(actual.searchParams.get('suite-infrascout')).port,'8765');
    }
  });
}
test('Suite configuration uses independent custom module ports', async () => {
  const links = [8765, 8770, 8771].map(port => ({dataset: {modulePort: String(port)},removeAttribute(name){delete this[name]}}));
  const ports = {'8765':23456,'8770':24567,'8771':25678};
  vm.runInNewContext(source, {URL,fetch:async()=>({ok:true,json:async()=>ports}),window:{location:{href:'http://127.0.0.168:18766/team.html'}},document:{querySelectorAll:()=>links}});
  await new Promise(resolve=>setImmediate(resolve));
  assert.deepEqual(links.map(link=>new URL(link.href).port),['23456','24567','25678']);
  for(const link of links){const target=new URL(link.href);assert.equal(target.searchParams.get('suite-devcycle'),'http://127.0.0.168:18766/');assert.equal(target.searchParams.get('suite-fleetscope'),'http://127.0.0.168:24567/');}
});
test('failed configuration on a custom port cannot jump to a different instance', async () => {
  const link={dataset:{modulePort:'8765'},removeAttribute(name){delete this[name]}};
  vm.runInNewContext(source,{URL,fetch:async()=>{throw new Error('offline')},window:{location:new URL('http://127.0.0.136:18766/team.html')},document:{querySelectorAll:()=>[link]}});
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(link.href,undefined);
});
