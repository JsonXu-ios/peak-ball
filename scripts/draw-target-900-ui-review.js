// Execute the report's actual inline JavaScript in a small DOM mock.
// No browser, service, or network is used.
const fs = require('fs');
const path = require('path');
const vm = require('vm');
const crypto = require('crypto');
const assert = require('assert/strict');
const root = path.resolve(__dirname, '..');
const out = path.join(root, 'reports/draw-target-900-study');
const htmlPath = path.join(out, 'report.html');
const html = fs.readFileSync(htmlPath, 'utf8');
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
const read = name => JSON.parse(fs.readFileSync(path.join(out, name), 'utf8').replace(/^\uFEFF/, ''));
const blocks = [...html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)];
const payloadBlock = blocks.find(match => /id="payload"/.test(match[1]));
const application = blocks.filter(match => !/application\/json/.test(match[1])).map(match => match[2]).join('\n');
const payload = JSON.parse(payloadBlock[2]);
const models = read('models.json');
const rules = read('rules.json');
const dataset = JSON.parse(fs.readFileSync(path.join(root, 'reports/draw-full-feature-study/dataset.json'), 'utf8'));
const csv = fs.readFileSync(path.join(out, 'rule-selections.csv'), 'utf8').replace(/^\uFEFF/, '').trim().split(/\r?\n/).map(line => line.split(','));
const header = csv.shift();
assert.equal(payload.matches.length, 4161);
assert.equal(payload.methods.length, 49);
assert.equal(new Set(payload.methods.map(method => method.id)).size, 49);
assert.equal(payload.default, rules.bestConfigurationId);
const keys = ['matchId', 'matchTime', 'league', 'home', 'guest', 'homeScore', 'awayScore', 'result'];
for (let i = 0; i < payload.matches.length; i++) {
  assert.deepEqual(payload.matches[i], Object.fromEntries(keys.map(key => [key, dataset.rows[i][key] ?? null])));
  assert.equal(csv[i][0], payload.matches[i].matchId);
  assert.equal(Number(csv[i][1]), payload.matches[i].result);
}
for (const method of payload.methods) {
  const fitted = models.models.find(model => model.id === method.id);
  const configured = rules.configurations.find(config => config.id === method.id);
  assert(fitted || configured);
  const declared = fitted ? fitted.at900 : configured.stats;
  assert.equal(method.n, declared.selected);
  assert.equal(method.d, declared.TP ?? declared.draw);
  assert.equal(method.fp, declared.FP ?? declared.falseSelections);
  assert.equal(method.precision, declared.precision);
  assert.equal(method.recall, declared.recall);
  assert.equal(method.selection.length, 4161);
  if (fitted) {
    const selected = new Set(fitted.selectedMatchIds);
    assert.equal(selected.size, method.n);
    for (let i = 0; i < 4161; i++) assert.equal(method.selection[i] === '1', selected.has(payload.matches[i].matchId));
  } else {
    const col = header.indexOf(method.id);
    assert(col >= 2);
    for (let i = 0; i < 4161; i++) assert.equal(method.selection[i], csv[i][col]);
  }
}
class Element {
  constructor(id) { this.id = id; this.value = ''; this.textContent = ''; this.innerHTML = ''; this.listeners = {}; this.dataset = {}; this.scrolls = 0; }
  addEventListener(type, handler) { (this.listeners[type] ??= []).push(handler); }
  dispatch(type) { for (const handler of this.listeners[type] ?? []) handler({ target: this }); }
  scrollIntoView() { this.scrolls++; }
}
const elements = Object.fromEntries(['payload', 'method', 'outcome', 'search', 'prev', 'next', 'totals', 'page', 'rows'].map(id => [id, new Element(id)]));
elements.payload.textContent = payloadBlock[2];
elements.outcome.value = 'selected';
const buttons = [...html.matchAll(/<button[^>]*data-method="([^"]+)"[^>]*>/g)].map((match, index) => {
  const element = new Element('button-' + index); element.dataset.method = match[1]; return element;
});
assert.equal(buttons.length, 49);
const context = vm.createContext({ document: { getElementById: id => { assert(elements[id]); return elements[id]; },
  querySelectorAll: selector => { assert.equal(selector, '[data-method]'); return buttons; } } });
new vm.Script(application, { filename: 'report-inline.js' }).runInContext(context);
const uiCount = () => {
  const match = elements.page.textContent.match(/筛选(\d+)场，第(\d+)\/(\d+)页，每页最多100场/);
  assert(match); return { n: Number(match[1]), page: Number(match[2]), pages: Number(match[3]) };
};
const shownIDs = () => [...elements.rows.innerHTML.matchAll(/<tr><td>([^<]+)<br>/g)].map(match => match[1]);
const expectedIDs = (method, mode, query = '') => {
  const q = query.trim().toLowerCase();
  return payload.matches.flatMap((row, i) => {
    const picked = method.selection[i] === '1';
    const draw = row.result === 1;
    const categories = { selected: picked, draw: picked && draw, false: picked && !draw,
                         miss: !picked && draw, removed: !picked && !draw };
    const found = !q || [row.matchId, row.home, row.guest].map(value => String(value ?? '').toLowerCase()).some(value => value.includes(q));
    return categories[mode] && found ? [row.matchId] : [];
  });
};
let combinations = 0, pagesChecked = 0, searches = 0, buttonSwitches = 0, emptyChecks = 0;
for (const method of payload.methods) {
  elements.method.value = method.id; elements.method.dispatch('change');
  const expectedTotals = `本方法总体：入选${method.n}场，平${method.d}场，误选${method.fp}场，命中率${(100 * method.precision).toFixed(2)}%；覆盖${(100 * method.recall).toFixed(2)}%。搜索不会改变总体统计。`;
  assert.equal(elements.totals.textContent, expectedTotals);
  for (const mode of ['selected', 'draw', 'false', 'miss', 'removed']) {
    elements.search.value = ''; elements.search.dispatch('input');
    elements.outcome.value = mode; elements.outcome.dispatch('change');
    const expected = expectedIDs(method, mode);
    const count = { selected: method.n, draw: method.d, false: method.fp, miss: 970 - method.d,
                    removed: 3191 - method.fp }[mode];
    assert.equal(expected.length, count);
    assert.equal(uiCount().n, count); assert.equal(uiCount().page, 1);
    const totalPages = Math.max(1, Math.ceil(expected.length / 100));
    for (let page = 1; page <= totalPages; page++) {
      assert.deepEqual(uiCount(), { n: count, page, pages: totalPages });
      assert.deepEqual(shownIDs(), expected.slice((page - 1) * 100, page * 100));
      assert(shownIDs().length <= 100); pagesChecked++;
      elements.next.dispatch('click');
    }
    assert.equal(uiCount().page, totalPages); // next beyond end is clamped
    elements.prev.dispatch('click');
    assert.equal(uiCount().page, Math.max(1, totalPages - 1));
    const queries = ['  ' + (expected[0] ?? payload.matches[0].matchId) + '  ',
                     String(payload.matches[0].home ?? '').toUpperCase(), 'NO_MATCH_900_QA_MISSING'];
    for (const query of queries) {
      elements.search.value = query; elements.search.dispatch('input');
      const found = expectedIDs(method, mode, query);
      assert.equal(uiCount().n, found.length); assert.equal(uiCount().page, 1);
      assert.deepEqual(shownIDs(), found.slice(0, 100));
      assert.equal(elements.totals.textContent, expectedTotals);
      searches++;
      if (query.startsWith('NO_MATCH_')) {
        assert.equal(elements.rows.innerHTML, ''); assert.equal(uiCount().pages, 1);
        elements.prev.dispatch('click'); elements.next.dispatch('click');
        assert.deepEqual(uiCount(), { n: 0, page: 1, pages: 1 }); emptyChecks++;
      }
    }
    combinations++;
  }
}
elements.search.value = ''; elements.search.dispatch('input');
elements.outcome.value = 'selected'; elements.outcome.dispatch('change');
for (const button of buttons) {
  const before = elements.method.scrolls;
  button.dispatch('click');
  assert.equal(elements.method.value, button.dataset.method);
  assert.equal(elements.method.scrolls, before + 1);
  const method = payload.methods.find(method => method.id === button.dataset.method);
  assert.deepEqual(shownIDs(), expectedIDs(method, 'selected').slice(0, 100));
  assert.equal(uiCount().n, method.n); assert.equal(uiCount().page, 1);
  buttonSwitches++;
}
// Rendered values must be escaped, including future source text with tags.
assert.equal(vm.runInContext('escape(' + JSON.stringify(`<script>"&'`) + ')', context), '&lt;script&gt;&quot;&amp;&#39;');
const qa = { purpose: 'Independent Node vm / mock DOM execution of the final actual inline report JavaScript; no browser or service.',
  generatedAt: new Date().toISOString(), issues: [], methods: payload.methods.length, matches: payload.matches.length,
  outcomeCombinationsChecked: combinations, pagesChecked, searchesChecked: searches, emptyResultChecks: emptyChecks,
  buttonSwitchesChecked: buttonSwitches, payloadSelectionBitsChecked: 49 * 4161,
  searchPreservesOverallStatistics: true, allPagesMax100: true, paginationBoundsClamped: true,
  HTMLSHA256: hash(fs.readFileSync(htmlPath)), applicationJSSHA256: hash(application), payloadSHA256: hash(payloadBlock[2]),
  reportScriptSHA256: hash(fs.readFileSync(path.join(root, 'scripts/draw-target-900-report.py'))),
  qaScriptSHA256: hash(fs.readFileSync(__filename)),
  sources: Object.fromEntries(['models.json', 'rules.json', 'rule-selections.csv', 'review.json'].map(name => [name, hash(fs.readFileSync(path.join(out, name)))])) };
fs.writeFileSync(path.join(out, 'report-ui-qa.json'), JSON.stringify(qa, null, 2) + '\n');
console.log(JSON.stringify(qa));
