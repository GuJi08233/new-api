const fs = require('node:fs/promises');
const path = require('node:path');
const { createHash } = require('node:crypto');

const BUILD_INFO_MAGIC = Buffer.from('ff20476f206275696c64696e663a', 'hex');

// Go 1.18+ 在所有受支持的可执行格式中使用同一内联 build info 格式。
// 只读取元数据，不执行待打包的后端，也不要求本机安装 Go。
function readGoBuildVersion(binary) {
  const executable =
    binary.subarray(0, 2).toString() === 'MZ' ||
    binary.subarray(0, 4).equals(Buffer.from('7f454c46', 'hex')) ||
    ['feedface', 'cefaedfe', 'feedfacf', 'cffaedfe'].includes(
      binary.subarray(0, 4).toString('hex'),
    );
  if (!executable) {
    throw new Error('Backend is not a supported PE, ELF, or Mach-O executable');
  }

  let header = -1;
  for (let offset = binary.indexOf(BUILD_INFO_MAGIC); offset !== -1;
    offset = binary.indexOf(BUILD_INFO_MAGIC, offset + 1)) {
    if (offset % 16 !== 0) continue;
    if (header !== -1) throw new Error('Backend has ambiguous Go build information');
    header = offset;
  }
  if (header < 0 || header + 33 > binary.length) {
    throw new Error('Backend has no readable Go build information');
  }
  if (![4, 8].includes(binary[header + 14]) ||
      ![2, 3].includes(binary[header + 15]) ||
      binary.subarray(header + 16, header + 32).some((byte) => byte !== 0)) {
    throw new Error('Backend uses unsupported or damaged Go build information');
  }

  // 正式 Go 版本远小于 128 字节，长度的 uvarint 必须只有一个字节。
  const length = binary[header + 32];
  if (length === 0 || length >= 128 || header + 33 + length > binary.length) {
    throw new Error('Backend has a truncated or invalid Go version');
  }
  const version = binary.subarray(header + 33, header + 33 + length).toString('utf8');
  if (!/^go\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`Backend must use a stable Go release; found ${JSON.stringify(version)}`);
  }
  return version;
}

async function validateBackendBinary(binaryPath) {
  const moduleFile = await fs.readFile(path.join(__dirname, '..', 'go.mod'), 'utf8');
  const minimum = moduleFile.match(/^go (\d+\.\d+\.\d+)\s*$/m)?.[1];
  if (!minimum) throw new Error('go.mod must specify a stable Go patch release');
  const version = readGoBuildVersion(await fs.readFile(binaryPath));
  const actual = version.slice(2).split('.').map(Number);
  const required = minimum.split('.').map(Number);
  const difference = actual.map((part, index) => part - required[index]).find((part) => part !== 0) || 0;
  if (difference < 0) {
    throw new Error(`Refusing to package ${binaryPath}: built with ${version}; rebuild with Go ${minimum} or newer`);
  }
  return version;
}

async function beforePack(context) {
  const [nodeMajor, nodeMinor, nodePatch] = process.versions.node.split('.').map(Number);
  if (nodeMajor !== 24 || nodeMinor < 22 || nodeMinor === 22 && nodePatch < 1) {
    throw new Error(`Packaging requires Node ${require('./package.json').engines.node}; found ${process.versions.node}`);
  }
  const binaryName = context.electronPlatformName === 'win32' ? 'new-api.exe' : 'new-api';
  await validateBackendBinary(path.resolve(context.packager.projectDir, '..', binaryName));

  const electronPackagePath = require.resolve('electron/package.json');
  const electronDirectory = path.dirname(electronPackagePath);
  const electronVersion = require(electronPackagePath).version;
  const expectedVersion = require('./package.json').devDependencies.electron;
  if (electronVersion !== expectedVersion || context.packager.config.electronVersion &&
      context.packager.config.electronVersion !== expectedVersion) {
    throw new Error(`Electron runtime must match the locked version ${expectedVersion}`);
  }
  const checksums = require(path.join(electronDirectory, 'checksums.json'));
  const arch = require('builder-util').Arch[context.arch];
  const filename = `electron-v${electronVersion}-${context.electronPlatformName}-${arch}.zip`;
  const expectedHash = checksums[filename];
  if (!/^[a-f0-9]{64}$/.test(expectedHash || '')) {
    throw new Error(`The locked Electron package has no checksum for ${filename}`);
  }
  // 使用 Electron npm 包携带的固定哈希，不从下载站获取 SHASUMS256.txt。
  // 在 beforePack 中失败会中止构建；electronDist 函数异常会被 builder 吞掉并回退下载。
  const { downloadArtifact } = require(require.resolve('@electron/get', { paths: [electronDirectory] }));
  const zipPath = await downloadArtifact({
    version: electronVersion,
    artifactName: 'electron',
    platform: context.electronPlatformName,
    arch,
    checksums: { [filename]: expectedHash },
  });
  const actualHash = createHash('sha256').update(await fs.readFile(zipPath)).digest('hex');
  if (actualHash !== expectedHash) throw new Error(`Electron archive checksum mismatch: ${zipPath}`);
  context.packager.config.electronDist = zipPath;
}

async function afterPack(context) {
  const binaryName = context.electronPlatformName === 'win32' ? 'new-api.exe' : 'new-api';
  const resourcesDirectory = context.packager.getResourcesDir(context.appOutDir);
  await validateBackendBinary(path.join(resourcesDirectory, 'bin', binaryName));
}

module.exports = { beforePack, afterPack, readGoBuildVersion, validateBackendBinary };
