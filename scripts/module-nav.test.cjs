const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../internal/web/static/module-nav.js'), 'utf8');
for (const origin of ['http://127.0.0.136:8766/team.html?x=1#details', 'http://127.0.0.168:8766/team.html', 'http://[::1]:8766/team.html', 'https://localhost:8766/team.html']) {
  test(`module links preserve host for ${origin}`, () => {
    const links = [8765, 8770, 8771].map(port => ({dataset: {modulePort: String(port)}}));
    vm.runInNewContext(source, {URL, window: {location: {href: origin}}, document: {querySelectorAll: () => links}});
    for (const link of links) {
      const actual = new URL(link.href), expected = new URL(origin);
      assert.equal(actual.hostname, expected.hostname);
      assert.equal(actual.protocol, expected.protocol);
      assert.equal(actual.port, link.dataset.modulePort);
      assert.equal(actual.pathname, '/');
      assert.equal(actual.search + actual.hash, '');
    }
  });
}
