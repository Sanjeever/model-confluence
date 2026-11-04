import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const formalVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

export function versionCommand(directory, command, value) {
  if (!['bump', 'check'].includes(command)) {
    throw new Error('用法：version.mjs bump <version> 或 check <tag>');
  }
  const packagePath = resolve(directory, 'web/package.json');
  const pkg = JSON.parse(readFileSync(packagePath, 'utf8'));
  const version = command === 'bump' ? value : pkg.version;
  if (typeof version !== 'string' || !formalVersion.test(version)) {
    throw new Error('请输入正式三段数字版本，例如 1.9.4');
  }
  if (command === 'check' && value !== `v${version}`) {
    throw new Error('tag 与 web/package.json 版本不一致');
  }
  const notesPath = `docs/releases/${version}.md`;
  let notes;
  try {
    notes = readFileSync(resolve(directory, notesPath), 'utf8');
  } catch (error) {
    if (error.code === 'ENOENT') throw new Error(`缺少更新日志 ${notesPath}`);
    throw error;
  }
  if (!notes.trim()) throw new Error(`更新日志不能为空：${notesPath}`);
  if (command === 'bump' && pkg.version !== version) {
    pkg.version = version;
    writeFileSync(packagePath, JSON.stringify(pkg, null, 2) + '\n');
  }
  return version;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const [command, value] = process.argv.slice(2);
    const version = versionCommand(root, command, value);
    console.log(command === 'bump'
      ? `版本已更新为 ${version}，请与 docs/releases/${version}.md 一并提交。`
      : '版本与更新日志检查通过。');
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
