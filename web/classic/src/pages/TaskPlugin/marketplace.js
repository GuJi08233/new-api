/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 插件市场索引的解析与源码获取。索引与源码都由浏览器直接拉取，
// 网关不发起任何外部请求；上传时服务端会按索引 sha256 重新校验源码。

export const SUPPORTED_INDEX_VERSION = 1;
export const SUPPORTED_PLUGIN_KIND = 'task';
// 与后端 maxTaskPluginSourceBytes / 图标上限保持一致
export const MAX_PLUGIN_SOURCE_BYTES = 8 * 1024 * 1024;
export const MAX_PLUGIN_ICON_BYTES = 512 * 1024;

// 版本 path 相对索引地址解析，且必须与索引同源，避免索引把源码下载指向别处
export function resolvePluginSourceUrl(indexUrl, path) {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  if (!trimmed) return null;
  let base;
  let resolved;
  try {
    base = new URL(indexUrl);
    resolved = new URL(trimmed, base);
  } catch {
    return null;
  }
  if (resolved.protocol !== 'http:' && resolved.protocol !== 'https:') {
    return null;
  }
  if (resolved.origin !== base.origin) return null;
  return resolved.toString();
}

const stringArray = (value) =>
  Array.isArray(value) &&
  value.every((item) => typeof item === 'string' && item.length > 0)
    ? value
    : undefined;

const trimmedString = (value) =>
  typeof value === 'string' && value.trim() ? value.trim() : undefined;

function parseMarketplacePlugin(entry) {
  if (!entry || typeof entry !== 'object') return null;
  const key = trimmedString(entry.key);
  if (!key) return null;

  const versions = [];
  for (const candidate of Array.isArray(entry.versions) ? entry.versions : []) {
    if (!candidate || typeof candidate !== 'object') continue;
    const version = trimmedString(candidate.version);
    const path = trimmedString(candidate.path);
    if (!version || !path) continue;
    const kind = trimmedString(candidate.kind);
    if (kind && kind !== SUPPORTED_PLUGIN_KIND) continue;
    versions.push({
      version,
      path,
      sha256: trimmedString(candidate.sha256),
      minApiVersion: Number.isFinite(Number(candidate.minApiVersion))
        ? Number(candidate.minApiVersion)
        : undefined,
      allowedHosts: stringArray(candidate.allowedHosts),
      baseUrl: trimmedString(candidate.baseUrl),
    });
  }
  if (versions.length === 0) return null;

  const declaredLatest = trimmedString(entry.latest);
  const latest = versions.some((item) => item.version === declaredLatest)
    ? declaredLatest
    : versions[0].version;

  // icon 只接受 LobeHub 名称或 text 方案，丢弃 data URI 与远程地址，防止页面被用作信标
  let icon;
  const rawIcon = trimmedString(entry.icon);
  if (
    rawIcon &&
    rawIcon.length <= 128 &&
    !rawIcon.startsWith('data:') &&
    !rawIcon.includes('://')
  ) {
    icon = rawIcon;
  }

  // 图片图标以附带文件形式随索引发布，只从索引同站拉取
  let iconFile;
  if (entry.iconFile && typeof entry.iconFile === 'object') {
    const iconPath = trimmedString(entry.iconFile.path);
    if (iconPath && pluginIconMediaType(iconPath)) {
      iconFile = {
        path: iconPath,
        sha256: trimmedString(entry.iconFile.sha256),
      };
    }
  }

  let description;
  if (typeof entry.description === 'string') {
    description = entry.description;
  } else if (
    entry.description &&
    typeof entry.description === 'object' &&
    !Array.isArray(entry.description)
  ) {
    description = {};
    for (const [locale, text] of Object.entries(entry.description)) {
      if (typeof text === 'string') description[locale] = text;
    }
  }

  let website;
  const rawWebsite = trimmedString(entry.website);
  if (rawWebsite) {
    try {
      const parsed = new URL(rawWebsite);
      if (parsed.protocol === 'https:' || parsed.protocol === 'http:') {
        website = parsed.toString();
      }
    } catch {
      website = undefined;
    }
  }

  return {
    key,
    name: trimmedString(entry.name) || key,
    icon,
    iconFile,
    description,
    website,
    sortPriority: Number.isInteger(entry.sortPriority) ? entry.sortPriority : 0,
    channelTypes: Array.isArray(entry.channelTypes)
      ? entry.channelTypes.filter((item) => Number.isFinite(item))
      : [],
    models: stringArray(entry.models) || [],
    latest,
    versions,
  };
}

// 索引只是展示缓存：格式不对的插件条目跳过而不让整个源失败，准入仍以服务端编译为准
export function parseMarketplaceIndex(payload) {
  if (!payload || typeof payload !== 'object') {
    throw new Error('index is not an object');
  }
  const indexVersion = Number(payload.indexVersion);
  if (!Number.isFinite(indexVersion)) {
    throw new Error('index is missing indexVersion');
  }
  if (indexVersion > SUPPORTED_INDEX_VERSION) {
    throw new Error(`unsupported indexVersion ${indexVersion}`);
  }
  const plugins = (Array.isArray(payload.plugins) ? payload.plugins : [])
    .map(parseMarketplacePlugin)
    .filter(Boolean)
    .sort(
      (left, right) =>
        right.sortPriority - left.sortPriority ||
        left.key.localeCompare(right.key),
    );
  return {
    indexVersion,
    name: typeof payload.name === 'string' ? payload.name : '',
    plugins,
  };
}

export async function fetchMarketplaceIndex(indexUrl) {
  const response = await fetch(indexUrl, { cache: 'no-cache' });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  return parseMarketplaceIndex(await response.json());
}

// 版本化源码常被标记为 immutable，原地重发的文件若读到旧缓存会被 sha256 拒绝，故总是回源校验
export async function fetchPluginSourceText(url) {
  const response = await fetch(url, { cache: 'no-cache' });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  const declaredLength = Number(response.headers.get('content-length'));
  if (
    Number.isFinite(declaredLength) &&
    declaredLength > MAX_PLUGIN_SOURCE_BYTES
  ) {
    throw new Error('plugin source exceeds 8 MiB');
  }
  const text = await response.text();
  if (new TextEncoder().encode(text).length > MAX_PLUGIN_SOURCE_BYTES) {
    throw new Error('plugin source exceeds 8 MiB');
  }
  return text;
}

// 非安全上下文（HTTP 部署）没有 WebCrypto，返回 null；权威校验在服务端
export async function sha256Hex(bytes) {
  if (!globalThis.crypto?.subtle) return null;
  const digest = await globalThis.crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('');
}

const bytesToBase64 = (bytes) => {
  let binary = '';
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary);
};

export function pluginIconMediaType(fileName) {
  const lower = String(fileName || '').toLowerCase();
  if (lower.endsWith('.svg')) return 'image/svg+xml';
  if (lower.endsWith('.png')) return 'image/png';
  return null;
}

export function encodePluginIconBytes(fileName, bytes) {
  const mediaType = pluginIconMediaType(fileName);
  if (
    !mediaType ||
    bytes.length === 0 ||
    bytes.length > MAX_PLUGIN_ICON_BYTES
  ) {
    return null;
  }
  return `data:${mediaType};base64,${bytesToBase64(bytes)}`;
}

// 图标只是装饰：拉取失败、类型不符或摘要不匹配都返回 null，绝不阻断安装
export async function fetchPluginIconDataUri(url, expectedSha256) {
  try {
    const response = await fetch(url, { cache: 'no-cache' });
    if (!response.ok) return null;
    const bytes = new Uint8Array(await response.arrayBuffer());
    const expected = expectedSha256?.trim().toLowerCase();
    if (expected) {
      const actual = await sha256Hex(bytes);
      if (actual !== null && actual !== expected) return null;
    }
    return encodePluginIconBytes(new URL(url).pathname, bytes);
  } catch {
    return null;
  }
}

// 已安装版本不在索引中（本地上传或源回滚了版本）时标记为 diverged，界面不能称之为升级
export function deriveInstallState(plugin, installed) {
  const match = installed.find((item) => item.meta?.key === plugin.key);
  if (!match) return { status: 'not_installed' };
  const installedVersion = match.meta.version;
  const entry = plugin.versions.find(
    (item) => item.version === installedVersion,
  );
  if (!entry) return { status: 'diverged', installedVersion };
  // 版本号相同但源码不同：本地改过（例如内置插件带有本仓库的安全修补），装市场版会覆盖这些修改
  const modified = Boolean(
    entry.sha256 &&
      match.source_hash &&
      entry.sha256.toLowerCase() !== match.source_hash.toLowerCase(),
  );
  if (installedVersion === plugin.latest) {
    return { status: modified ? 'modified' : 'up_to_date', installedVersion };
  }
  return { status: 'upgradable', installedVersion, modified };
}

// 上传只在临时注册表里编译校验，与内置静态路由的冲突要等发布到运行时才暴露，安装后需回查运行状态
export function findRuntimeFailure(plugins, key) {
  const item = plugins.find((plugin) => plugin.meta?.key === key);
  if (item?.runtime_status !== 'compile_failed') return '';
  return item.runtime_error || 'unknown error';
}

// 插件描述可以是字符串或 { en, zh, ... } 本地化映射
export function localizedText(value, language) {
  if (!value) return '';
  if (typeof value === 'string') return value;
  const lang = String(language || '').toLowerCase();
  const candidates = [lang, lang.split('-')[0], 'en', 'zh'];
  for (const candidate of candidates) {
    if (candidate && typeof value[candidate] === 'string') {
      return value[candidate];
    }
  }
  return Object.values(value).find((text) => typeof text === 'string') || '';
}
