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

export const riskReviewColor = (status) => {
  if (status === 'confirmed') return 'green';
  if (status === 'false_positive') return 'grey';
  return 'orange';
};

// 事件类型的中文文案与色彩。类型是稳定标识,新增类型时在这里和事件列表的筛选项一起补。
export const riskEventTypeMeta = (eventType, t) => {
  const meta = {
    block_ip: { label: 'IP 拦截', color: 'red', category: 'block' },
    block_ua: { label: 'UA 拦截', color: 'orange', category: 'block' },
    block_client_ua: {
      label: '客户端规则拦截',
      color: 'yellow',
      category: 'block',
    },
    ban_ip: { label: 'IP 封禁', color: 'red', category: 'ban' },
    ban_auto: { label: '自动禁用', color: 'red', category: 'ban' },
    ban_manual: { label: '手动禁用', color: 'purple', category: 'ban' },
    unban_ip: { label: 'IP 解封', color: 'green', category: 'unban' },
    unban: { label: '解除禁用', color: 'green', category: 'unban' },
    unban_auto: { label: '到期恢复', color: 'green', category: 'unban' },
    alert: { label: '规则告警', color: 'amber', category: 'alert' },
  }[eventType];
  return {
    label: meta ? t(meta.label) : eventType || '-',
    color: meta?.color,
    category: meta?.category,
  };
};

// 事件大类,用于事件列表的快捷筛选与概览标题。
export const riskEventCategories = [
  { value: 'block', label: '拦截' },
  { value: 'ban', label: '封禁与禁用' },
  { value: 'unban', label: '解封与恢复' },
  { value: 'alert', label: '告警' },
];

// 相对时间,用于最近活跃、最近处置这类只需要大致时点的位置。
export const riskRelativeTime = (timestamp) => {
  if (!timestamp) return '-';
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - timestamp);
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
};

// 剩余时间,用于封禁与临时禁用的到期提示。
export const riskRemainingTime = (expiresAt) => {
  if (!expiresAt) return '';
  const seconds = Math.floor(expiresAt - Date.now() / 1000);
  if (seconds <= 0) return '';
  if (seconds < 3600) return `${Math.ceil(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
};

// 规则或来源的可读名称。扫描规则按 rule_id 找到当前配置里的名称;防护、黑名单、客户端规则
// 按事件类型与记录的规则文本拼出来源;找不到时退回原始标识,不丢失历史信息。
export const riskRuleLabel = (item, rules, t) => {
  const ruleId = item?.rule_id || item?.evidence?.rule_id || '';
  const rule = item?.rule || '';
  if (ruleId === 'probe_guard' || ruleId === 'error_guard')
    return riskSourceLabel(ruleId, t);
  const configured = (rules || []).find(
    (entry) => entry.id && entry.id === ruleId,
  );
  if (configured)
    return configured.name || riskMetricLabel(configured.metric, t);
  if (item?.evidence?.name && item.evidence.name !== item.evidence.metric)
    return item.evidence.name;
  switch (item?.event_type) {
    case 'block_ip':
      if (rule === 'ban:*') return t('动态 IP 封禁');
      if (rule.startsWith('ban:'))
        return `${t('动态 IP 封禁')} ${rule.slice(4)}`;
      return rule ? `${t('IP 黑名单')} ${rule}` : t('IP 黑名单');
    case 'block_ua':
      return rule ? `${t('UA 黑名单')} ${rule}` : t('UA 黑名单');
    case 'block_client_ua':
      return rule ? `${t('客户端规则')} ${rule}` : t('客户端规则');
    default:
      break;
  }
  if (rule === 'exempt:disable_user') return t('豁免账号跳过处置');
  if (rule.startsWith('game:')) return `${t('游戏作弊')} ${rule.slice(5)}`;
  if (item?.evidence?.metric) return riskMetricLabel(item.evidence.metric, t);
  if (rule) return riskMetricLabel(rule, t);
  return ruleId || '-';
};

// 跳转到某条规则的命中记录时用的事件筛选条件。
export const riskRuleEventFilters = (item) => {
  if (item?.rule === 'ban:*') return { event_type: 'block_ip' };
  if (item?.rule_id) return { rule_id: item.rule_id };
  if (item?.rule) return { event_type: item.event_type, rule_id: item.rule };
  return { event_type: item?.event_type };
};

// 当前登录管理员的用户 ID,读取失败时为 0。
export const riskCurrentUserId = () => {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}').id || 0;
  } catch {
    return 0;
  }
};

// 封禁时长预设(分钟),0 表示永久。上限与后端 RiskMaxIpBanMinutes 一致(30 天)。
export const riskBanDurations = [
  { minutes: 10, label: '10 分钟' },
  { minutes: 60, label: '1 小时' },
  { minutes: 1440, label: '1 天' },
  { minutes: 10080, label: '7 天' },
  { minutes: 43200, label: '30 天' },
  { minutes: 0, label: '永久' },
];

export const RISK_MAX_BAN_MINUTES = 43200;
