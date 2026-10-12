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

import { API, showError } from '../../../helpers';

// 游戏页共用的请求与格式化工具。

// 把请求结果换成 { data } 或 { error }：服务端的拒绝原因原样给玩家看；登录过期交给全局处理(跳转登录页)；
// 限流与网络错误给出通用提示。network 为真表示请求没有得到答复或服务端暂时出错，结果可能已经结算或者可以原样重交，
// 页面要保留这一局、重新读取或让玩家重新提交；limited 为真表示被限流、请求没有执行，稍后可以原样重试。
function gameResult(promise, t) {
  return promise.then(
    (res) =>
      res.data.success ? { data: res.data.data } : { error: res.data.message },
    (error) => {
      const status = error?.response?.status;
      if (status === 401) {
        showError(error);
        return { error: t('登录已过期，请重新登录') };
      }
      if (status === 429) {
        return { error: t('操作太快了，请稍后再试'), limited: true };
      }
      const message = error?.response?.data?.message;
      if (status && status < 500 && message) {
        // 服务端给出了拒绝原因，例如 IP 已被风控封禁。
        return { error: message };
      }
      // 5xx 是服务端暂时出错(例如结算时数据库出错，这一局原样重交就能结算)，带着原因时照样给玩家看。
      return {
        error:
          status >= 500 && message
            ? message
            : t('网络异常，本次结果以游戏币明细为准'),
        network: true,
      };
    },
  );
}

export function gamePost(url, body, t) {
  return gameResult(API.post(url, body, { skipErrorHandler: true }), t);
}

export function gameGet(url, t, config = {}) {
  return gameResult(API.get(url, { ...config, skipErrorHandler: true }), t);
}

// 领免费奖励的请求带上一次性的 Turnstile 令牌(查询参数 turnstile)。
export function withTurnstile(url, token) {
  return token ? `${url}?turnstile=${encodeURIComponent(token)}` : url;
}

// 带正负号的整数，用于今日输赢。
export function signed(value) {
  const number = Number(value || 0);
  return number > 0 ? `+${number}` : `${number}`;
}

// 百分比，去掉多余的零：0.9644 → 96.44%。
export function percent(ratio, digits = 2) {
  return `${Number((ratio * 100).toFixed(digits))}%`;
}

export const formatNumber = (value) => Number(value || 0).toLocaleString();
