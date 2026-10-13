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

export const riskSourceLabel = (source, t) => {
  switch (source) {
    case 'manual':
      return t('手动添加');
    case 'auto_rule':
      return t('自动规则');
    case 'probe_guard':
      return t('测活防护');
    case 'error_guard':
      return t('错误防护');
    case 'ua_blacklist':
      return t('UA 黑名单');
    default:
      return source || '-';
  }
};

export const riskMetricLabel = (metric, t) => {
  const labels = {
    ip_multi_user: t('单 IP 关联用户数'),
    user_multi_ip: t('单用户使用 IP 数'),
    ip_multi_token: t('单 IP 使用令牌数(批量测活)'),
    user_tiny_request: t('用户微量请求数(自动测活)'),
    user_error_burst: t('用户错误请求数'),
  };
  return labels[metric] || riskSourceLabel(metric, t);
};

export const riskActionLabel = (action, t) => {
  const labels = {
    alert: t('仅告警'),
    disable_user: t('禁用账号'),
    enable_user: t('启用用户'),
    ban_ip: t('封禁 IP'),
    ban_both: t('封禁 IP + 禁用账号'),
    unban_ip: t('解除封禁'),
    whitelist_global: t('加入全局豁免'),
    whitelist_account: t('加入账号豁免'),
  };
  return labels[action] || action;
};

export const riskReviewLabel = (status, t) => {
  if (status === 'confirmed') return t('已确认');
  if (status === 'false_positive') return t('误报');
  return t('待处理');
};
