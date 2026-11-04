import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {versionCommand} from './version.mjs';

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'model-confluence-version-'));
  t.after(() => rmSync(root, {recursive: true, force: true}));
  mkdirSync(join(root, 'web'));
  mkdirSync(join(root, 'docs/releases'), {recursive: true});
  writeFileSync(join(root, 'web/package.json'), JSON.stringify({
    name: 'model-confluence-web', version: '1.9.3', private: true,
    scripts: {build: 'tsc -b && vite build'},
  }, null, 2) + '\n');
  return root;
}

test('缺少或空白日志时，bump 保留版本文件原值', t => {
  const root = fixture(t);
  const path = join(root, 'web/package.json');
  const before = readFileSync(path, 'utf8');
  assert.throws(() => versionCommand(root, 'bump', '1.9.4'), /缺少更新日志/);
  writeFileSync(join(root, 'docs/releases/1.9.4.md'), ' \n\t');
  assert.throws(() => versionCommand(root, 'bump', '1.9.4'), /不能为空/);
  assert.equal(readFileSync(path, 'utf8'), before);
});

test('bump 更新版本，保留其他字段，check 不修改文件', t => {
  const root = fixture(t);
  const path = join(root, 'web/package.json');
  const before = JSON.parse(readFileSync(path, 'utf8'));
  const notes = '## 修复\n\n- 修复协议转换。\n';
  writeFileSync(join(root, 'docs/releases/1.9.4.md'), notes);
  assert.equal(versionCommand(root, 'bump', '1.9.4'), '1.9.4');
  const updated = readFileSync(path, 'utf8');
  assert.deepEqual(JSON.parse(updated), {...before, version: '1.9.4'});
  assert.equal(versionCommand(root, 'check', 'v1.9.4'), '1.9.4');
  assert.equal(readFileSync(path, 'utf8'), updated);
  assert.equal(readFileSync(join(root, 'docs/releases/1.9.4.md'), 'utf8'), notes);
});

test('check 拒绝不匹配的标签和缺失、空白日志', t => {
  const root = fixture(t);
  for (const tag of [undefined, '1.9.3', 'v1.9.4', 'v1.9.3-rc.1']) {
    assert.throws(() => versionCommand(root, 'check', tag), /版本不一致/);
  }
  assert.throws(() => versionCommand(root, 'check', 'v1.9.3'), /缺少更新日志/);
  writeFileSync(join(root, 'docs/releases/1.9.3.md'), '\n');
  assert.throws(() => versionCommand(root, 'check', 'v1.9.3'), /不能为空/);
});

test('拒绝非法版本和命令，不修改版本文件', t => {
  const root = fixture(t);
  const path = join(root, 'web/package.json');
  const before = readFileSync(path, 'utf8');
  for (const version of [undefined, 'v1.9.4', '01.9.4', '1.9', '1.9.4-rc.1', '../1.9.4', 194]) {
    assert.throws(() => versionCommand(root, 'bump', version), /正式三段数字版本/);
  }
  assert.throws(() => versionCommand(root, 'unknown', '1.9.4'), /用法/);
  assert.equal(readFileSync(path, 'utf8'), before);
  writeFileSync(path, JSON.stringify({version: '1.9.4-rc.1'}));
  assert.throws(() => versionCommand(root, 'check', 'v1.9.4-rc.1'), /正式三段数字版本/);
});
