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

export const SUBSCRIPTION_BALANCE_PAY_METHOD = 'balance';

/**
 * 支付方式的白名单键，与后端 model.NormalizeSubscriptionPayMethod 保持一致。
 * 加密货币的键里带钱包地址，客户端大小写不固定，所以整体转小写比较。
 */
export function normalizeSubscriptionPayMethod(method) {
  return String(method ?? '')
    .trim()
    .toLowerCase();
}

/** 解析套餐的 allowed_payment_methods，空数组表示不限制。 */
export function parseAllowedPaymentMethods(raw) {
  return String(raw ?? '')
    .split(',')
    .map(normalizeSubscriptionPayMethod)
    .filter(Boolean);
}

/** 易支付通道：pay_methods 里除去自成一类的 stripe/creem/加密货币。 */
export function getSubscriptionEpayMethods(payMethods = []) {
  return (payMethods || []).filter(
    (m) =>
      m?.type &&
      m.type !== 'stripe' &&
      m.type !== 'creem' &&
      m.type !== 'ethereum',
  );
}

/**
 * 站点当前开放的订阅支付方式，顺序与购买弹窗一致。管理端的勾选列表和买家看到
 * 的下拉必须出自同一处，否则会出现勾了的方式买家选不到、或反过来的情况。
 * 这里只反映站点级配置，套餐级的限制（商品 ID、白名单）由调用方叠加。
 */
export function buildSubscriptionPayOptions({
  payMethods = [],
  enableOnlineTopUp = false,
  enableStripeTopUp = false,
  enableCreemTopUp = false,
  enableEthereumTopUp = false,
  ethereumInfo = null,
  balanceLabel = '余额支付',
} = {}) {
  const epayMethods = getSubscriptionEpayMethods(payMethods);

  // 手动配置的代币优先，自动发现的同地址代币去重，避免下拉里出现两条同币种
  const manualTokens = (payMethods || []).filter((m) => m?.type === 'ethereum');
  const manualAddresses = manualTokens.map((m) =>
    normalizeSubscriptionPayMethod(m.address),
  );
  const autoTokens = (ethereumInfo?.tokens || []).filter(
    (token) =>
      !manualAddresses.includes(normalizeSubscriptionPayMethod(token.address)),
  );
  const ethereumTokens = [
    ...manualTokens.map((m) => ({
      symbol: m.name || 'ETH',
      address: m.address || '0x0000000000000000000000000000000000000000',
    })),
    ...autoTokens.map((token) => ({
      symbol: token.symbol,
      address: token.address,
    })),
  ];
  const hasEthereum =
    (enableEthereumTopUp && ethereumTokens.length > 0) ||
    manualTokens.length > 0;

  return [
    ...(enableOnlineTopUp && epayMethods.length > 0
      ? epayMethods.map((m) => ({ value: m.type, label: m.name || m.type }))
      : []),
    ...(enableStripeTopUp ? [{ value: 'stripe', label: 'Stripe' }] : []),
    ...(enableCreemTopUp ? [{ value: 'creem', label: 'Creem' }] : []),
    ...(hasEthereum
      ? ethereumTokens.map((token) => ({
          // 键必须与后端存储的白名单形式一致：代币地址是 EIP-55 混合大小写，
          // 而后端按小写存，不归一化的话管理端回显和提交都匹配不上。
          value: normalizeSubscriptionPayMethod(`ethereum:${token.address}`),
          label: token.symbol,
        }))
      : []),
    { value: SUBSCRIPTION_BALANCE_PAY_METHOD, label: balanceLabel },
  ];
}
