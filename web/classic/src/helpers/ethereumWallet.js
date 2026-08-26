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

const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000';
const EIP6963_ANNOUNCE_EVENT = 'eip6963:announceProvider';
const EIP6963_REQUEST_EVENT = 'eip6963:requestProvider';
const WALLETCONNECT_OFFICIAL_RELAY_URL = 'wss://relay.walletconnect.com';

const ERC20_ABI = [
  'function approve(address spender, uint256 amount) returns (bool)',
  'function allowance(address owner, address spender) view returns (uint256)',
  'function balanceOf(address owner) view returns (uint256)',
];
const NATIVE_PAY_ABI = ['function payWithETH(bytes32 orderId) payable'];
const TOKEN_PAY_ABI = [
  'function payWithToken(bytes32 orderId, address token, uint256 amount)',
];

// Solidity 与 OpenZeppelin v5 的 revert 选择器，用于把链上回滚数据翻译成可读原因。
const REVERT_ERROR_STRING_SELECTOR = '0x08c379a0'; // Error(string)
const REVERT_PANIC_SELECTOR = '0x4e487b71'; // Panic(uint256)
const REVERT_ERC20_INSUFFICIENT_BALANCE = '0xe450d38c'; // ERC20InsufficientBalance(address,uint256,uint256)
const REVERT_ERC20_INSUFFICIENT_ALLOWANCE = '0xfb8f41b2'; // ERC20InsufficientAllowance(address,uint256,uint256)
const REVERT_ERROR_NAMES = {
  [REVERT_ERC20_INSUFFICIENT_BALANCE]: 'ERC20InsufficientBalance',
  [REVERT_ERC20_INSUFFICIENT_ALLOWANCE]: 'ERC20InsufficientAllowance',
  '0x96c6fd1e': 'ERC20InvalidSender',
  '0xec442f05': 'ERC20InvalidReceiver',
  '0xe602df05': 'ERC20InvalidApprover',
  '0x94280d62': 'ERC20InvalidSpender',
  '0xd93c0665': 'EnforcedPause',
  '0x118cdaa7': 'OwnableUnauthorizedAccount',
  '0x3ee5aeb5': 'ReentrancyGuardReentrantCall',
  '0x5274afe7': 'SafeERC20FailedOperation',
};

// 支付合约（NewApiPayment.sol）以 require 字符串回滚；把用户可能触发的文案映射成可读提示。
const KNOWN_REVERT_REASON_KEYS = {
  'NewApiPayment: order already paid':
    '该订单已在链上完成支付，无需再次付款；若额度未到账，请稍候回调或联系管理员',
  'NewApiPayment: unsupported token':
    '该代币暂不被支付合约接受，请刷新页面后重试或联系管理员',
};

function getWindowEthereum() {
  if (typeof window === 'undefined') return undefined;
  return window.ethereum;
}

function normalizeText(value) {
  return String(value || '')
    .trim()
    .toLowerCase();
}

function buildWalletName(provider, info) {
  if (info?.name) return info.name;
  if (provider?.isRabby) return 'Rabby Wallet';
  if (provider?.isMetaMask) return 'MetaMask';
  if (provider?.isCoinbaseWallet) return 'Coinbase Wallet';
  return 'Browser Wallet';
}

function getWalletScore(provider, info) {
  const name = normalizeText(info?.name);
  const rdns = normalizeText(info?.rdns);
  if (provider?.isRabby || name.includes('rabby') || rdns.includes('rabby')) {
    return 300;
  }
  if (
    provider?.isMetaMask ||
    name.includes('metamask') ||
    rdns.includes('metamask')
  ) {
    return 200;
  }
  if (
    provider?.isCoinbaseWallet ||
    name.includes('coinbase') ||
    rdns.includes('coinbase')
  ) {
    return 150;
  }
  return 50;
}

function makeWalletEntry(provider, info = {}) {
  return {
    id:
      info?.uuid ||
      info?.rdns ||
      `${buildWalletName(provider, info)}-${Math.random().toString(36).slice(2)}`,
    name: buildWalletName(provider, info),
    icon: info?.icon || '',
    rdns: info?.rdns || '',
    provider,
    score: getWalletScore(provider, info),
  };
}

function appendUniqueWallet(target, seen, provider, info = {}) {
  if (!provider || seen.has(provider)) return;
  seen.add(provider);
  target.push(makeWalletEntry(provider, info));
}

function getLegacyInjectedWallets() {
  const wallets = [];
  const seen = new Set();
  const injected = getWindowEthereum();
  if (!injected) return wallets;

  if (Array.isArray(injected.providers) && injected.providers.length > 0) {
    injected.providers.forEach((provider) => {
      appendUniqueWallet(wallets, seen, provider);
    });
    return wallets;
  }

  appendUniqueWallet(wallets, seen, injected);
  return wallets;
}

export async function discoverInjectedWallets(timeoutMs = 250) {
  if (typeof window === 'undefined') return [];

  const wallets = [];
  const seen = new Set();
  const handler = (event) => {
    const detail = event?.detail || {};
    appendUniqueWallet(wallets, seen, detail.provider, detail.info || {});
  };

  window.addEventListener(EIP6963_ANNOUNCE_EVENT, handler);
  try {
    window.dispatchEvent(new Event(EIP6963_REQUEST_EVENT));
    await new Promise((resolve) => window.setTimeout(resolve, timeoutMs));
  } finally {
    window.removeEventListener(EIP6963_ANNOUNCE_EVENT, handler);
  }

  getLegacyInjectedWallets().forEach((wallet) => {
    appendUniqueWallet(wallets, seen, wallet.provider, wallet);
  });

  return wallets.sort(
    (a, b) => b.score - a.score || a.name.localeCompare(b.name),
  );
}

function buildWalletConnectMetadata(config = {}) {
  const fallbackName =
    (typeof document !== 'undefined' && document.title) ||
    (typeof window !== 'undefined' && window.location?.hostname) ||
    'new-api';
  const fallbackURL =
    (typeof window !== 'undefined' && window.location?.origin) ||
    'http://localhost';
  const icon = String(config?.icon || '').trim();

  return {
    name: String(config?.appName || '').trim() || fallbackName,
    description:
      String(config?.description || '').trim() ||
      String(config?.appName || '').trim() ||
      fallbackName,
    url: String(config?.url || '').trim() || fallbackURL,
    icons: icon ? [icon] : [],
  };
}

function hasWalletConnectProjectId(config = {}) {
  return String(config?.projectId || '').trim() !== '';
}

function getWalletConnectRelayUrls(config = {}) {
  if (config?.relayProxyEnabled) {
    return [WALLETCONNECT_OFFICIAL_RELAY_URL];
  }
  const urls = [
    String(config?.primaryRelayUrl || '').trim(),
    String(config?.backupRelayUrl || '').trim(),
  ].filter(Boolean);
  return [...new Set(urls)];
}

function getWalletConnectTransportProxyUrl(config = {}) {
  if (!config?.relayProxyEnabled) return '';
  return normalizeWalletConnectRelayUrl(
    config?.relayProxyUrl || '/api/walletconnect/relay',
  );
}

function normalizeWalletConnectRelayUrl(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  if (/^wss?:\/\//i.test(raw)) return raw;
  if (typeof window === 'undefined' || !window.location?.origin) return raw;
  const url = new URL(raw, window.location.origin);
  url.protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

function attachWalletConnectLifecycle(provider, lifecycle = {}) {
  if (typeof provider?.on !== 'function') return;
  provider.on('display_uri', (uri) => {
    lifecycle?.onWalletConnectUri?.(uri);
  });
  provider.on('connect', () => {
    lifecycle?.onWalletConnectConnected?.();
  });
  provider.on('disconnect', () => {
    lifecycle?.onWalletConnectDisconnected?.();
  });
}

function installWalletConnectWebSocketProxy(proxyUrl) {
  if (
    typeof window === 'undefined' ||
    !proxyUrl ||
    typeof window.WebSocket !== 'function'
  ) {
    return () => {};
  }

  const NativeWebSocket = window.WebSocket;
  const shouldProxy = (target) => {
    try {
      const url = new URL(String(target));
      return (
        url.protocol === 'wss:' && url.hostname === 'relay.walletconnect.com'
      );
    } catch {
      return false;
    }
  };

  const WalletConnectProxyWebSocket = function (url, protocols) {
    if (!shouldProxy(url)) {
      return protocols === undefined
        ? new NativeWebSocket(url)
        : new NativeWebSocket(url, protocols);
    }
    const target = new URL(String(url));
    const proxy = new URL(proxyUrl);
    proxy.search = target.search;
    return protocols === undefined
      ? new NativeWebSocket(proxy.toString())
      : new NativeWebSocket(proxy.toString(), protocols);
  };
  window.WebSocket = WalletConnectProxyWebSocket;
  window.WebSocket.prototype = NativeWebSocket.prototype;
  Object.setPrototypeOf(window.WebSocket, NativeWebSocket);

  return () => {
    if (window.WebSocket === WalletConnectProxyWebSocket) {
      window.WebSocket = NativeWebSocket;
    }
  };
}

async function connectWalletConnectProvider(
  chainId,
  walletConnectConfig,
  relayUrl = '',
  relayIndex = 0,
) {
  const { EthereumProvider } = await import('@walletconnect/ethereum-provider');
  const initOptions = {
    projectId: String(walletConnectConfig.projectId).trim(),
    showQrModal: false,
    chains: [Number(chainId)],
    optionalChains: [Number(chainId)],
    metadata: buildWalletConnectMetadata(walletConnectConfig),
  };
  if (relayUrl) {
    initOptions.relayUrl = relayUrl;
  }
  const provider = await EthereumProvider.init(initOptions);

  return {
    mode: 'walletconnect',
    walletName: 'WalletConnect',
    relayUrl,
    relayIndex,
    provider,
  };
}

async function connectWalletConnectProviderWithFallback(
  chainId,
  walletConnectConfig,
  lifecycle = {},
  startIndex = 0,
) {
  const relayUrls = getWalletConnectRelayUrls(walletConnectConfig);
  const candidates = relayUrls.length > 0 ? relayUrls : [''];
  let lastError;
  for (let i = startIndex; i < candidates.length; i += 1) {
    try {
      const connection = await connectWalletConnectProvider(
        chainId,
        walletConnectConfig,
        candidates[i],
        i,
      );
      connection.relayUrls = candidates;
      attachWalletConnectLifecycle(connection.provider, lifecycle);
      return connection;
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError || new Error('WalletConnect provider init failed');
}

export async function connectEthereumWallet(
  chainId,
  walletConnectConfig = {},
  lifecycle = {},
) {
  const injectedWallets = await discoverInjectedWallets();
  if (injectedWallets.length > 0) {
    const preferred = injectedWallets[0];
    return {
      mode: 'injected',
      walletName: preferred.name,
      provider: preferred.provider,
    };
  }

  if (hasWalletConnectProjectId(walletConnectConfig)) {
    lifecycle?.onWalletConnectPending?.();
    const connection = await connectWalletConnectProviderWithFallback(
      chainId,
      walletConnectConfig,
      lifecycle,
    );

    return connection;
  }

  throw new Error(
    '请安装 Rabby、MetaMask 等 EVM 钱包，或联系管理员配置 WalletConnect 二维码连接',
  );
}

export function isEthereumUserRejected(error) {
  const code =
    error && typeof error === 'object' && 'code' in error
      ? error.code
      : undefined;
  const nestedCode =
    error &&
    typeof error === 'object' &&
    'info' in error &&
    error.info?.error?.code;

  if (code === 4001 || code === 'ACTION_REJECTED' || nestedCode === 4001) {
    return true;
  }
  // 部分钱包用通用的 -32000 表示用户拒绝，但节点也用它包装 execution reverted 等
  // 真实失败，必须看文案区分，否则会把链上回滚误报成“用户取消”。
  if (code === -32000) {
    return /reject|denied|declin|cancel/i.test(String(error?.message || ''));
  }
  return false;
}

function formatTokenAmount(value, decimals, symbol) {
  const scale = Number(decimals);
  const digits = Number.isFinite(scale) ? Math.max(0, Math.trunc(scale)) : 0;
  let text;
  if (digits === 0) {
    text = value.toString();
  } else {
    const base = 10n ** BigInt(digits);
    const fraction = (value % base)
      .toString()
      .padStart(digits, '0')
      .replace(/0+$/, '');
    const whole = (value / base).toString();
    text = fraction ? `${whole}.${fraction}` : whole;
  }
  return symbol ? `${text} ${symbol}` : text;
}

function makeFriendlyEthereumError(fallbackMessage, key, params = {}) {
  const error = new Error(fallbackMessage);
  error.friendlyEthereumError = { key, params };
  return error;
}

// 支付前的余额预检失败：直接给出可读原因，避免让用户签下注定回滚的交易。
function makeInsufficientBalanceError(balance, needed, order) {
  const symbol = String(order?.symbol || '').trim();
  const decimals = order?.decimals;
  return makeFriendlyEthereumError(
    '钱包余额不足',
    '钱包余额不足：当前持有 {{balance}}，本次支付需要 {{needed}}',
    {
      balance: formatTokenAmount(balance, decimals, symbol),
      needed: formatTokenAmount(needed, decimals, symbol),
    },
  );
}

function extractRevertData(error) {
  const candidates = [
    error?.data,
    error?.data?.data,
    error?.data?.originalError?.data,
    error?.info?.error?.data,
    error?.error?.data,
    error?.cause?.data,
    error?.revert?.data,
  ];
  for (const candidate of candidates) {
    const hex = typeof candidate === 'string' ? candidate.trim() : '';
    if (/^0x[0-9a-fA-F]{8,}$/.test(hex)) {
      return hex.toLowerCase();
    }
  }
  return '';
}

// revert 数据由选择器加若干 32 字节字组成，静态类型参数可按字直接取值。
function decodeRevertWords(data) {
  const body = data.slice(10);
  const words = [];
  for (let i = 0; i + 64 <= body.length; i += 64) {
    words.push(body.slice(i, i + 64));
  }
  return words;
}

// Error(string) 的 ABI 布局：选择器 + 偏移字 + 长度字 + UTF-8 字节（右侧补零）。
// WalletConnect 原始 RPC 错误不经 ethers 归一化、没有 reason，必须自行解码。
function decodeRevertErrorString(data) {
  const body = data.slice(10);
  if (body.length < 128) return '';
  const offset = Number.parseInt(body.slice(0, 64), 16);
  if (!Number.isFinite(offset) || offset * 2 + 64 > body.length) return '';
  const lengthStart = offset * 2;
  const length = Number.parseInt(body.slice(lengthStart, lengthStart + 64), 16);
  if (!Number.isFinite(length) || length <= 0 || length > 512) return '';
  const hexBytes = body.slice(lengthStart + 64, lengthStart + 64 + length * 2);
  if (hexBytes.length < length * 2) return '';
  const bytes = new Uint8Array(length);
  for (let i = 0; i < length; i += 1) {
    bytes[i] = Number.parseInt(hexBytes.slice(i * 2, i * 2 + 2), 16);
  }
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes).trim();
  } catch {
    return '';
  }
}

// 部分节点不返回回滚数据，只把原因塞进错误文案（geth 与 hardhat 两种格式）。
function extractRevertReasonFromMessage(message) {
  const text = String(message || '');
  const match =
    text.match(/reverted with reason string ['"]([^'"]+)['"]/i) ||
    text.match(/execution reverted:\s*([^"()\n]+)/i);
  return match ? match[1].trim() : '';
}

function describeRevertReason(reason) {
  const friendlyKey = KNOWN_REVERT_REASON_KEYS[reason];
  if (friendlyKey) {
    return { key: friendlyKey, params: {} };
  }
  return { key: '合约拒绝了这笔交易：{{reason}}', params: { reason } };
}

function shortenEthereumErrorMessage(message) {
  const text = String(message || '').trim();
  if (!text) return '';
  if (text.length <= 160 && !text.includes('(action=')) return text;
  const cut = text.indexOf(' (');
  const head = cut > 0 ? text.slice(0, cut) : text;
  return head.length > 160 ? `${head.slice(0, 157)}...` : head;
}

/**
 * 把钱包与合约抛出的原始错误翻译成可读原因。
 * 返回 i18n key 与插值参数，由调用方通过 t(key, params) 渲染。
 */
export function describeEthereumError(error) {
  if (error?.friendlyEthereumError) {
    return error.friendlyEthereumError;
  }
  // 回滚数据是链上给出的确定性证据，必须优先于错误码：钱包与节点常把回滚包装成
  // 通用的 -32000，先看错误码会把余额不足误报成“用户取消”。
  const tokenMeta = error?.ethereumTokenMeta || {};
  const data = extractRevertData(error);
  const selector = data.slice(0, 10);
  const words = data ? decodeRevertWords(data) : [];

  if (selector === REVERT_ERC20_INSUFFICIENT_BALANCE && words.length >= 3) {
    return {
      key: '钱包余额不足：当前持有 {{balance}}，本次支付需要 {{needed}}',
      params: {
        balance: formatTokenAmount(
          BigInt(`0x${words[1]}`),
          tokenMeta.decimals,
          tokenMeta.symbol,
        ),
        needed: formatTokenAmount(
          BigInt(`0x${words[2]}`),
          tokenMeta.decimals,
          tokenMeta.symbol,
        ),
      },
    };
  }

  if (selector === REVERT_ERC20_INSUFFICIENT_ALLOWANCE && words.length >= 3) {
    return {
      key: '代币授权额度不足：已授权 {{allowance}}，本次支付需要 {{needed}}，请重新授权后重试',
      params: {
        allowance: formatTokenAmount(
          BigInt(`0x${words[1]}`),
          tokenMeta.decimals,
          tokenMeta.symbol,
        ),
        needed: formatTokenAmount(
          BigInt(`0x${words[2]}`),
          tokenMeta.decimals,
          tokenMeta.symbol,
        ),
      },
    };
  }

  if (selector === REVERT_ERROR_STRING_SELECTOR) {
    const reason =
      decodeRevertErrorString(data) || String(error?.reason || '').trim();
    if (reason) {
      return describeRevertReason(reason);
    }
  }

  if (selector === REVERT_PANIC_SELECTOR && words.length >= 1) {
    return {
      key: '合约内部执行出错（Panic {{code}}），请联系管理员处理',
      params: { code: `0x${BigInt(`0x${words[0]}`).toString(16)}` },
    };
  }

  if (selector.length === 10 && selector !== REVERT_ERROR_STRING_SELECTOR) {
    const name = REVERT_ERROR_NAMES[selector];
    if (name) {
      return {
        key: '合约拒绝了这笔交易（{{name}}），请联系管理员处理',
        params: { name },
      };
    }
    return {
      key: '合约拒绝了这笔交易（错误码 {{selector}}），请联系管理员处理',
      params: { selector },
    };
  }

  // 没有可解码的回滚数据，只能依据传输层错误码判断。
  const code = error?.code;
  if (isEthereumUserRejected(error)) {
    return { key: '你在钱包中取消了本次交易', params: {} };
  }
  if (code === 'INSUFFICIENT_FUNDS') {
    return {
      key: '钱包原生币不足，无法支付本次交易与网络手续费（gas）',
      params: {},
    };
  }
  if (
    code === 'NETWORK_ERROR' ||
    code === 'TIMEOUT' ||
    code === 'SERVER_ERROR'
  ) {
    return { key: '网络连接异常，请检查网络后重试', params: {} };
  }

  // ethers 的 reason 仅在 CALL_EXCEPTION 下可信（用户拒绝错误也带 reason=rejected）；
  // 从“execution reverted: …”文案里提取的原因则自证是链上回滚。
  const parsedReason =
    code === 'CALL_EXCEPTION' ? String(error?.reason || '').trim() : '';
  const messageReason = extractRevertReasonFromMessage(error?.message);
  const reason = parsedReason || messageReason;
  if (reason) {
    return describeRevertReason(reason);
  }

  if (code === 'CALL_EXCEPTION') {
    return {
      key: '合约拒绝了这笔交易，请确认代币余额与订单状态后重试',
      params: {},
    };
  }

  const message = shortenEthereumErrorMessage(error?.message);
  return message
    ? { key: '支付失败：{{message}}', params: { message } }
    : { key: '支付失败，请稍后重试', params: {} };
}

async function requestEthereumAccounts(browserProvider, rawProvider) {
  try {
    await browserProvider.send('eth_requestAccounts', []);
  } catch (error) {
    if (typeof rawProvider?.enable === 'function') {
      await rawProvider.enable();
      return;
    }
    throw error;
  }
}

async function connectWalletSession(rawProvider) {
  if (typeof rawProvider?.connect === 'function') {
    await rawProvider.connect();
    return;
  }
  if (typeof rawProvider?.enable === 'function') {
    await rawProvider.enable();
    return;
  }
  throw new Error('WalletConnect provider does not support connect/enable');
}

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function waitForWalletConnectAccounts(
  rawProvider,
  browserProvider,
  chainId,
  attempts = 10,
  intervalMs = 300,
) {
  let lastError;
  for (let i = 0; i < attempts; i += 1) {
    try {
      return await getWalletConnectAccount(
        rawProvider,
        browserProvider,
        chainId,
      );
    } catch (error) {
      lastError = error;
      if (i < attempts - 1) {
        await sleep(intervalMs);
      }
    }
  }
  throw lastError || new Error('WalletConnect 未返回可用账户信息');
}

async function waitForExpectedNetwork(
  rawProvider,
  expectedChainId,
  attempts = 10,
  intervalMs = 300,
) {
  let lastChainId = null;
  for (let i = 0; i < attempts; i += 1) {
    try {
      const chainIdHex = await rawProvider.request({
        method: 'eth_chainId',
      });
      lastChainId = BigInt(chainIdHex);
    } catch {
      lastChainId = null;
    }
    if (lastChainId === expectedChainId) {
      return;
    }
    if (i < attempts - 1) {
      await sleep(intervalMs);
    }
  }
  throw new Error(
    `请在钱包中切换到正确的网络，Chain ID: ${String(lastChainId)}`,
  );
}

async function getProviderChainId(rawProvider) {
  const chainIdHex = await rawProvider.request({ method: 'eth_chainId' });
  return BigInt(chainIdHex);
}

function normalizeWalletConnectAddress(value) {
  const parts = String(value || '').split(':');
  const address = (parts[parts.length - 1] || '').trim();
  if (!/^0x[a-fA-F0-9]{40}$/.test(address)) {
    return '';
  }
  return address;
}

async function getWalletConnectAccount(rawProvider, browserProvider, chainId) {
  let accounts = Array.isArray(rawProvider?.accounts)
    ? rawProvider.accounts
    : [];
  if (accounts.length === 0 && typeof rawProvider?.request === 'function') {
    try {
      const requestedAccounts = await rawProvider.request({
        method: 'eth_accounts',
      });
      if (Array.isArray(requestedAccounts)) {
        accounts = requestedAccounts;
      }
    } catch {
      // 某些 WalletConnect provider 不支持直接从 request 读取账户，后续继续尝试 signer
    }
  }

  const expectedPrefix = `eip155:${Number(chainId)}:`;
  const matched =
    accounts.find((item) => String(item || '').startsWith(expectedPrefix)) ||
    accounts.find((item) =>
      /^0x[a-fA-F0-9]{40}$/.test(String(item || '').trim()),
    );
  const address = normalizeWalletConnectAddress(matched || accounts[0]);
  if (address) {
    return address;
  }

  try {
    const signer = await browserProvider.getSigner();
    const signerAddress = await signer.getAddress();
    if (/^0x[a-fA-F0-9]{40}$/.test(String(signerAddress || '').trim())) {
      return String(signerAddress).trim();
    }
  } catch {
    // ignore
  }

  throw new Error(
    'WalletConnect 未返回可用账户信息，请在钱包中确认授权当前账户',
  );
}

export async function executeEthereumOrderWithAutoWallet(
  order,
  walletConnectConfig = {},
  lifecycle = {},
) {
  const restoreWalletConnectProxy = installWalletConnectWebSocketProxy(
    getWalletConnectTransportProxyUrl(walletConnectConfig),
  );
  const orderChainId = Number(order?.chain_id || 0);
  try {
    let connection = await connectEthereumWallet(
      orderChainId,
      walletConnectConfig,
      lifecycle,
    );
    let rawProvider = connection.provider;
    const { ethers } = await import('ethers');
    let browserProvider;
    if (connection.mode === 'walletconnect') {
      try {
        await connectWalletSession(rawProvider);
      } catch (error) {
        const nextRelayIndex = Number(connection.relayIndex || 0) + 1;
        if (
          Array.isArray(connection.relayUrls) &&
          nextRelayIndex < connection.relayUrls.length
        ) {
          lifecycle?.onWalletConnectPending?.();
          connection = await connectWalletConnectProviderWithFallback(
            orderChainId,
            walletConnectConfig,
            lifecycle,
            nextRelayIndex,
          );
          rawProvider = connection.provider;
          await connectWalletSession(rawProvider);
        } else {
          throw error;
        }
      }
      browserProvider = new ethers.BrowserProvider(rawProvider);
      lifecycle?.onWalletConnectSessionEstablished?.();
    } else {
      browserProvider = new ethers.BrowserProvider(rawProvider);
      await requestEthereumAccounts(browserProvider, rawProvider);
    }
    lifecycle?.onWalletConnectConnected?.();

    const expectedChainId = BigInt(order.chain_id);
    const currentChainId =
      connection.mode === 'walletconnect'
        ? await getProviderChainId(rawProvider)
        : (await browserProvider.getNetwork()).chainId;
    if (currentChainId !== expectedChainId) {
      lifecycle?.onWalletConnectSwitchNetworkPending?.();
      try {
        await rawProvider.request({
          method: 'wallet_switchEthereumChain',
          params: [{ chainId: `0x${Number(order.chain_id).toString(16)}` }],
        });
      } catch {
        throw new Error(
          `请在钱包中切换到正确的网络，Chain ID: ${order.chain_id}`,
        );
      }
      await waitForExpectedNetwork(rawProvider, expectedChainId);
      browserProvider = new ethers.BrowserProvider(rawProvider);
    }

    let signer;
    let walletConnectAccount = '';
    if (connection.mode === 'walletconnect') {
      walletConnectAccount = await waitForWalletConnectAccounts(
        rawProvider,
        browserProvider,
        order.chain_id,
      );
      lifecycle?.onWalletConnectReadyToSign?.();
      return await executeWalletConnectTransaction({
        order,
        rawProvider,
        browserProvider,
        account: walletConnectAccount,
        walletName: connection.walletName,
        lifecycle,
        ethers,
      });
    } else {
      signer = await browserProvider.getSigner();
    }
    const isNativeToken =
      String(order.token_address || '').toLowerCase() ===
      ZERO_ADDRESS.toLowerCase();

    const contract = new ethers.Contract(
      order.contract_address,
      isNativeToken ? NATIVE_PAY_ABI : TOKEN_PAY_ABI,
      signer,
    );
    const payAmount = BigInt(order.pay_amount);
    const signerAddress = await signer.getAddress();

    let tx;
    if (isNativeToken) {
      const nativeBalance = await browserProvider.getBalance(signerAddress);
      if (nativeBalance < payAmount) {
        throw makeInsufficientBalanceError(nativeBalance, payAmount, order);
      }
      lifecycle?.onWalletConnectTransactionPending?.();
      tx = await contract.payWithETH(order.order_id, { value: payAmount });
    } else {
      const tokenContract = new ethers.Contract(
        order.token_address,
        ERC20_ABI,
        signer,
      );
      // 先校验余额再请求授权，否则用户会为一笔注定回滚的支付白付一次授权手续费。
      const tokenBalance = await tokenContract.balanceOf(signerAddress);
      if (tokenBalance < payAmount) {
        throw makeInsufficientBalanceError(tokenBalance, payAmount, order);
      }

      const currentAllowance = await tokenContract.allowance(
        signerAddress,
        order.contract_address,
      );
      if (currentAllowance < payAmount) {
        lifecycle?.onWalletConnectApprovePending?.();
        const approveTx = await tokenContract.approve(
          order.contract_address,
          payAmount,
        );
        await approveTx.wait();
      }

      lifecycle?.onWalletConnectTransactionPending?.();
      tx = await contract.payWithToken(
        order.order_id,
        order.token_address,
        payAmount,
      );
    }

    const receipt = await tx.wait();
    if (receipt?.status !== 1) {
      throw new Error('Ethereum 交易失败');
    }

    return {
      hash: tx.hash,
      walletName: connection.walletName,
      mode: connection.mode,
    };
  } catch (error) {
    // 附带代币符号与精度，供 describeEthereumError 格式化链上回滚数据里的数量。
    if (error && typeof error === 'object' && !error.ethereumTokenMeta) {
      error.ethereumTokenMeta = {
        symbol: order?.symbol,
        decimals: order?.decimals,
      };
    }
    lifecycle?.onWalletConnectError?.(error);
    throw error;
  } finally {
    restoreWalletConnectProxy();
  }
}

async function executeWalletConnectTransaction({
  order,
  rawProvider,
  browserProvider,
  account,
  walletName,
  lifecycle,
  ethers,
}) {
  const isNativeToken =
    String(order.token_address || '').toLowerCase() ===
    ZERO_ADDRESS.toLowerCase();
  const contractInterface = new ethers.Interface(
    isNativeToken ? NATIVE_PAY_ABI : TOKEN_PAY_ABI,
  );
  const payAmount = BigInt(order.pay_amount);

  let txHash;
  if (isNativeToken) {
    const nativeBalance = await browserProvider.getBalance(account);
    if (nativeBalance < payAmount) {
      throw makeInsufficientBalanceError(nativeBalance, payAmount, order);
    }
    lifecycle?.onWalletConnectTransactionPending?.();
    txHash = await rawProvider.request({
      method: 'eth_sendTransaction',
      params: [
        {
          from: account,
          to: order.contract_address,
          value: ethers.toBeHex(payAmount),
          data: contractInterface.encodeFunctionData('payWithETH', [
            order.order_id,
          ]),
        },
      ],
    });
  } else {
    const erc20Interface = new ethers.Interface(ERC20_ABI);
    // 先校验余额再请求授权，否则用户会为一笔注定回滚的支付白付一次授权手续费。
    const balanceResult = await rawProvider.request({
      method: 'eth_call',
      params: [
        {
          from: account,
          to: order.token_address,
          data: erc20Interface.encodeFunctionData('balanceOf', [account]),
        },
        'latest',
      ],
    });
    const tokenBalance = hexToBigInt(balanceResult);
    if (tokenBalance < payAmount) {
      throw makeInsufficientBalanceError(tokenBalance, payAmount, order);
    }

    const allowanceResult = await rawProvider.request({
      method: 'eth_call',
      params: [
        {
          from: account,
          to: order.token_address,
          data: erc20Interface.encodeFunctionData('allowance', [
            account,
            order.contract_address,
          ]),
        },
        'latest',
      ],
    });
    if (hexToBigInt(allowanceResult) < payAmount) {
      lifecycle?.onWalletConnectApprovePending?.();
      const approveHash = await rawProvider.request({
        method: 'eth_sendTransaction',
        params: [
          {
            from: account,
            to: order.token_address,
            data: erc20Interface.encodeFunctionData('approve', [
              order.contract_address,
              payAmount,
            ]),
          },
        ],
      });
      const approveReceipt =
        await browserProvider.waitForTransaction(approveHash);
      if (approveReceipt?.status !== 1) {
        throw new Error('代币授权交易失败');
      }
    }

    lifecycle?.onWalletConnectTransactionPending?.();
    txHash = await rawProvider.request({
      method: 'eth_sendTransaction',
      params: [
        {
          from: account,
          to: order.contract_address,
          data: contractInterface.encodeFunctionData('payWithToken', [
            order.order_id,
            order.token_address,
            payAmount,
          ]),
        },
      ],
    });
  }

  const receipt = await browserProvider.waitForTransaction(txHash);
  if (receipt?.status !== 1) {
    throw new Error('Ethereum 交易失败');
  }
  return {
    hash: txHash,
    walletName,
    mode: 'walletconnect',
  };
}

function hexToBigInt(value) {
  const hex = String(value || '').trim();
  if (!hex || hex === '0x') return 0n;
  return BigInt(hex);
}
