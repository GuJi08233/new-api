const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const { readGoBuildVersion, validateBackendBinary, afterPack } = require('./build-security.cjs');

test('missing or truncated build information fails closed', () => {
  assert.throws(() => readGoBuildVersion(Buffer.from('not an executable')), /not a supported/);
  assert.throws(() => readGoBuildVersion(Buffer.from('MZ')), /no readable/);
  const truncated = Buffer.concat([
    Buffer.from('MZ'), Buffer.alloc(14), Buffer.from('ff20476f206275696c64696e663a', 'hex'),
  ]);
  assert.throws(() => readGoBuildVersion(truncated), /no readable/);
});

// 测试夹具由真实 Go 编译器交叉编译，测试仅解析文件，绝不执行它们。
// NEW_API_GO_FIXTURES 中应有 {windows,linux,darwin}-{old,safe} 六个文件。
const fixtures = process.env.NEW_API_GO_FIXTURES;
test('real PE, ELF and Mach-O backends enforce the Go security baseline', { skip: !fixtures }, async (t) => {
  for (const platform of ['windows', 'linux', 'darwin']) {
    await t.test(platform, async () => {
      const safe = path.join(fixtures, `${platform}-safe`);
      const old = path.join(fixtures, `${platform}-old`);
      assert.match(await validateBackendBinary(safe), /^go\d+\.\d+\.\d+$/);
      await assert.rejects(validateBackendBinary(old), /Refusing to package.*go1\.25\.1/);

      const output = await fs.mkdtemp(path.join(os.tmpdir(), 'new-api-afterpack-test-'));
      try {
        await fs.mkdir(path.join(output, 'bin'));
        const binaryName = platform === 'windows' ? 'new-api.exe' : 'new-api';
        const context = {
          appOutDir: output,
          electronPlatformName: platform === 'windows' ? 'win32' : platform,
          packager: { getResourcesDir: () => output },
        };
        await fs.copyFile(old, path.join(output, 'bin', binaryName));
        await assert.rejects(afterPack(context), /Refusing to package/);
        await fs.copyFile(safe, path.join(output, 'bin', binaryName));
        await afterPack(context);
      } finally {
        await fs.rm(output, { recursive: true, force: true });
      }
    });
  }
});
