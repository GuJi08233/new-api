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

import React, { useEffect, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { clearIpInfoCache } from '../../components/common/ui/IpTag';

const { Text, Title } = Typography;

const KEY_PREFIX = 'risk_control_setting.';
const KEY_ENABLED = `${KEY_PREFIX}enabled`;
const KEY_UA_BLACKLIST = `${KEY_PREFIX}ua_blacklist`;
const KEY_UA_ACTION = `${KEY_PREFIX}ua_blacklist_action`;
const KEY_IP_BLACKLIST = `${KEY_PREFIX}ip_blacklist`;
const KEY_SCAN_MINUTES = `${KEY_PREFIX}scan_minutes`;
const KEY_WHITELIST = `${KEY_PREFIX}whitelist_user_ids`;
const KEY_PUBLIC_KEY_USERS = `${KEY_PREFIX}public_key_user_ids`;
const KEY_RULES = `${KEY_PREFIX}auto_ban_rules`;
const KEY_TINY_MAX_TOKENS = `${KEY_PREFIX}tiny_request_max_tokens`;
const KEY_EVENT_RETENTION = `${KEY_PREFIX}event_retention_days`;
const KEY_IP_BAN_FIRST = `${KEY_PREFIX}ip_ban_first_minutes`;
const KEY_IP_BAN_SECOND = `${KEY_PREFIX}ip_ban_second_minutes`;
const KEY_IP_BAN_LADDER = `${KEY_PREFIX}ip_ban_escalation_minutes`;
const KEY_IP_BAN_PERMANENT = `${KEY_PREFIX}ip_ban_permanent_offense`;
const KEY_IP_BAN_IPV6_PREFIX = `${KEY_PREFIX}ip_ban_ipv6_prefix_length`;
const KEY_PG_ENABLED = `${KEY_PREFIX}probe_guard_enabled`;
const KEY_PG_DRY_RUN = `${KEY_PREFIX}probe_guard_dry_run`;
const KEY_PG_WINDOW = `${KEY_PREFIX}probe_guard_window_seconds`;
const KEY_PG_MODEL_COUNT = `${KEY_PREFIX}probe_guard_model_count`;
const KEY_EG_ENABLED = `${KEY_PREFIX}error_guard_enabled`;
const KEY_EG_DRY_RUN = `${KEY_PREFIX}error_guard_dry_run`;
const KEY_EG_WINDOW = `${KEY_PREFIX}error_guard_window_seconds`;
const KEY_EG_THRESHOLD = `${KEY_PREFIX}error_guard_threshold`;
const KEY_EG_STATUS_CODES = `${KEY_PREFIX}error_guard_status_codes`;
const KEY_PG_ACTION = `${KEY_PREFIX}probe_guard_action`;
const KEY_PG_BAN_MINUTES = `${KEY_PREFIX}probe_guard_ban_minutes`;
const KEY_EG_ACTION = `${KEY_PREFIX}error_guard_action`;
const KEY_EG_BAN_MINUTES = `${KEY_PREFIX}error_guard_ban_minutes`;

// 系统设置里的 IP 日志开关。白名单账号的 IP 豁免依据日志中的地址,关闭时豁免不生效。
const KEY_GLOBAL_IP_LOG = 'GlobalRecordIpLogEnabled';

// 封禁升级阶梯的级数上限与默认值,与后端校验保持一致。
const MAX_BAN_LADDER_STEPS = 10;
const DEFAULT_BAN_LADDER = [10, 60];

// IP 维度指标才允许封禁 IP 的处置动作,与后端校验保持一致。
const IP_DIMENSION_METRICS = ['ip_multi_user', 'ip_multi_token'];
// 包含封禁 IP 的动作,切换到用户维度指标时要回退。
const BAN_IP_ACTIONS = ['ban_ip', 'ban_both'];
// 真正产生封禁的动作。封禁时长对 IP 与账号一视同仁,仅告警的规则无时长可言。
const DISPOSITION_ACTIONS = ['disable_user', 'ban_ip', 'ban_both'];

const IP_LOCATION_PREFIX = 'ip_location_setting.';
const KEY_GITEE_API_KEY = `${IP_LOCATION_PREFIX}gitee_api_key`;
const KEY_IPV4_ORDER = `${IP_LOCATION_PREFIX}ipv4_order`;
const KEY_IPV6_ORDER = `${IP_LOCATION_PREFIX}ipv6_order`;
const KEY_AUTO_LOOKUP = `${IP_LOCATION_PREFIX}auto_lookup`;

const IP_PROVIDER_OPTIONS = [
  { value: 'gitee', label: 'Gitee AI（中文，仅 IPv4，需密钥）' },
  { value: 'ipwhois', label: 'ipwho.is（英文，IPv4/IPv6）' },
  { value: 'ip9', label: 'ip9.com.cn（中文，IPv4/IPv6）' },
];

const DEFAULT_IPV4_ORDER = ['gitee', 'ipwhois', 'ip9'];
const DEFAULT_IPV6_ORDER = ['ipwhois', 'ip9'];

const parseJsonArray = (value, fallback = []) => {
  if (!value) return fallback;
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : fallback;
  } catch (e) {
    return fallback;
  }
};

const RiskSettings = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  const [enabled, setEnabled] = useState(false);
  const [uaBlacklistText, setUaBlacklistText] = useState('');
  const [uaAction, setUaAction] = useState('block');
  const [ipBlacklistText, setIpBlacklistText] = useState('');
  const [scanMinutes, setScanMinutes] = useState(10);
  const [whitelistText, setWhitelistText] = useState('');
  const [publicKeyUsersText, setPublicKeyUsersText] = useState('');
  const [ipLogEnabled, setIpLogEnabled] = useState(true);
  const [rules, setRules] = useState([]);
  const [tinyMaxTokens, setTinyMaxTokens] = useState(16);
  const [retentionDays, setRetentionDays] = useState(30);
  const [banLadder, setBanLadder] = useState(DEFAULT_BAN_LADDER);
  const [ipBanPermanent, setIpBanPermanent] = useState(3);
  const [ipv6PrefixLength, setIpv6PrefixLength] = useState(64);
  const [pgEnabled, setPgEnabled] = useState(false);
  const [pgDryRun, setPgDryRun] = useState(true);
  const [pgWindow, setPgWindow] = useState(60);
  const [pgModelCount, setPgModelCount] = useState(5);
  const [egEnabled, setEgEnabled] = useState(false);
  const [egDryRun, setEgDryRun] = useState(true);
  const [egWindow, setEgWindow] = useState(60);
  const [egThreshold, setEgThreshold] = useState(5);
  const [egStatusCodesText, setEgStatusCodesText] = useState('');
  const [pgAction, setPgAction] = useState('ban_ip');
  const [pgBanMinutes, setPgBanMinutes] = useState(0);
  const [egAction, setEgAction] = useState('ban_ip');
  const [egBanMinutes, setEgBanMinutes] = useState(0);
  const [giteeApiKey, setGiteeApiKey] = useState('');
  const [ipv4Order, setIpv4Order] = useState(DEFAULT_IPV4_ORDER);
  const [ipv6Order, setIpv6Order] = useState(DEFAULT_IPV6_ORDER);
  const [autoLookup, setAutoLookup] = useState(true);
  const [clearingCache, setClearingCache] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);

  const loadSettings = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/option/');
      const { success, message, data } = res.data;
      if (success) {
        const optionMap = {};
        data.forEach((item) => {
          optionMap[item.key] = item.value;
        });
        setEnabled(optionMap[KEY_ENABLED] === 'true');
        setUaBlacklistText(
          parseJsonArray(optionMap[KEY_UA_BLACKLIST]).join('\n'),
        );
        setUaAction(optionMap[KEY_UA_ACTION] || 'block');
        setIpBlacklistText(
          parseJsonArray(optionMap[KEY_IP_BLACKLIST]).join('\n'),
        );
        const scan = parseInt(optionMap[KEY_SCAN_MINUTES], 10);
        setScanMinutes(Number.isFinite(scan) && scan > 0 ? scan : 10);
        const tiny = parseInt(optionMap[KEY_TINY_MAX_TOKENS], 10);
        setTinyMaxTokens(Number.isFinite(tiny) && tiny > 0 ? tiny : 16);
        const retention = parseInt(optionMap[KEY_EVENT_RETENTION], 10);
        setRetentionDays(
          Number.isFinite(retention) && retention > 0 ? retention : 30,
        );
        // 阶梯未配置时用旧的首次/再犯两档初始化,保存后即迁移为阶梯
        const ladder = parseJsonArray(optionMap[KEY_IP_BAN_LADDER]).filter(
          (n) => Number.isFinite(n) && n > 0,
        );
        if (ladder.length > 0) {
          setBanLadder(ladder);
        } else {
          const banFirst = parseInt(optionMap[KEY_IP_BAN_FIRST], 10);
          const banSecond = parseInt(optionMap[KEY_IP_BAN_SECOND], 10);
          setBanLadder([
            Number.isFinite(banFirst) && banFirst > 0 ? banFirst : 10,
            Number.isFinite(banSecond) && banSecond > 0 ? banSecond : 60,
          ]);
        }
        const ipv6Prefix = parseInt(optionMap[KEY_IP_BAN_IPV6_PREFIX], 10);
        setIpv6PrefixLength(
          Number.isFinite(ipv6Prefix) && ipv6Prefix >= 32 && ipv6Prefix <= 128
            ? ipv6Prefix
            : 64,
        );
        const banPermanent = parseInt(optionMap[KEY_IP_BAN_PERMANENT], 10);
        setIpBanPermanent(
          Number.isFinite(banPermanent) && banPermanent >= 0 ? banPermanent : 3,
        );
        setPgEnabled(optionMap[KEY_PG_ENABLED] === 'true');
        // 未配置时跟随后端默认值(true),仅显式 false 时关闭
        setPgDryRun(optionMap[KEY_PG_DRY_RUN] !== 'false');
        const pgWin = parseInt(optionMap[KEY_PG_WINDOW], 10);
        setPgWindow(Number.isFinite(pgWin) && pgWin > 0 ? pgWin : 60);
        const pgCount = parseInt(optionMap[KEY_PG_MODEL_COUNT], 10);
        setPgModelCount(Number.isFinite(pgCount) && pgCount > 0 ? pgCount : 5);
        setEgEnabled(optionMap[KEY_EG_ENABLED] === 'true');
        setEgDryRun(optionMap[KEY_EG_DRY_RUN] !== 'false');
        const egWin = parseInt(optionMap[KEY_EG_WINDOW], 10);
        setEgWindow(Number.isFinite(egWin) && egWin > 0 ? egWin : 60);
        const egCount = parseInt(optionMap[KEY_EG_THRESHOLD], 10);
        setEgThreshold(Number.isFinite(egCount) && egCount > 0 ? egCount : 5);
        setEgStatusCodesText(
          parseJsonArray(optionMap[KEY_EG_STATUS_CODES]).join(','),
        );
        setPgAction(optionMap[KEY_PG_ACTION] || 'ban_ip');
        setEgAction(optionMap[KEY_EG_ACTION] || 'ban_ip');
        const pgBan = parseInt(optionMap[KEY_PG_BAN_MINUTES], 10);
        setPgBanMinutes(Number.isFinite(pgBan) && pgBan > 0 ? pgBan : 0);
        const egBan = parseInt(optionMap[KEY_EG_BAN_MINUTES], 10);
        setEgBanMinutes(Number.isFinite(egBan) && egBan > 0 ? egBan : 0);
        setPublicKeyUsersText(
          parseJsonArray(optionMap[KEY_PUBLIC_KEY_USERS]).join(','),
        );
        setWhitelistText(parseJsonArray(optionMap[KEY_WHITELIST]).join(','));
        // 只有明确为 false 才提示;取不到时按开启处理,避免误报
        setIpLogEnabled(optionMap[KEY_GLOBAL_IP_LOG] !== 'false');
        setRules(
          parseJsonArray(optionMap[KEY_RULES]).map((r, idx) => ({
            ...r,
            _id: idx,
          })),
        );
        setGiteeApiKey(optionMap[KEY_GITEE_API_KEY] || '');
        setIpv4Order(
          parseJsonArray(optionMap[KEY_IPV4_ORDER], DEFAULT_IPV4_ORDER),
        );
        setIpv6Order(
          parseJsonArray(optionMap[KEY_IPV6_ORDER], DEFAULT_IPV6_ORDER),
        );
        // 未配置时跟随后端默认值(true)，因此只在显式为 false 时关闭。
        setAutoLookup(optionMap[KEY_AUTO_LOOKUP] !== 'false');
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setLoading(false);
  };

  useEffect(() => {
    loadSettings();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const saveSettings = async () => {
    const uaBlacklist = uaBlacklistText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    const ipBlacklist = ipBlacklistText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    const parseUserIds = (text) =>
      text
        .split(/[,，\s]+/)
        .map((s) => parseInt(s, 10))
        .filter((n) => Number.isFinite(n) && n > 0);
    const whitelistUserIds = parseUserIds(whitelistText);
    const publicKeyUserIds = parseUserIds(publicKeyUsersText);
    const cleanRules = rules.map(({ _id, ...rest }) => {
      const metric = rest.metric || 'ip_multi_user';
      let action = rest.action || 'alert';
      // 封禁 IP 只对 IP 维度指标有效,指标被改为用户维度后回退为仅告警
      if (
        BAN_IP_ACTIONS.includes(action) &&
        !IP_DIMENSION_METRICS.includes(metric)
      ) {
        action = 'alert';
      }
      return {
        enabled: !!rest.enabled,
        metric,
        window_hours: rest.window_hours > 0 ? rest.window_hours : 24,
        threshold: rest.threshold > 0 ? rest.threshold : 1,
        action,
        ban_minutes: rest.ban_minutes > 0 ? rest.ban_minutes : 0,
      };
    });

    const ladder = banLadder.filter((n) => Number.isFinite(n) && n > 0);
    const statusCodes = egStatusCodesText
      .split(/[,，\s]+/)
      .map((s) => parseInt(s, 10))
      .filter((n) => Number.isFinite(n) && n >= 400 && n <= 599);

    const updates = [
      { key: KEY_ENABLED, value: String(enabled) },
      { key: KEY_UA_BLACKLIST, value: JSON.stringify(uaBlacklist) },
      { key: KEY_UA_ACTION, value: uaAction },
      { key: KEY_IP_BLACKLIST, value: JSON.stringify(ipBlacklist) },
      { key: KEY_SCAN_MINUTES, value: String(scanMinutes) },
      { key: KEY_TINY_MAX_TOKENS, value: String(tinyMaxTokens) },
      { key: KEY_EVENT_RETENTION, value: String(retentionDays) },
      {
        key: KEY_IP_BAN_LADDER,
        value: JSON.stringify(ladder.length > 0 ? ladder : DEFAULT_BAN_LADDER),
      },
      { key: KEY_IP_BAN_PERMANENT, value: String(ipBanPermanent) },
      { key: KEY_IP_BAN_IPV6_PREFIX, value: String(ipv6PrefixLength) },
      { key: KEY_PG_ENABLED, value: String(pgEnabled) },
      { key: KEY_PG_DRY_RUN, value: String(pgDryRun) },
      { key: KEY_PG_WINDOW, value: String(pgWindow) },
      { key: KEY_PG_MODEL_COUNT, value: String(pgModelCount) },
      { key: KEY_EG_ENABLED, value: String(egEnabled) },
      { key: KEY_EG_DRY_RUN, value: String(egDryRun) },
      { key: KEY_EG_WINDOW, value: String(egWindow) },
      { key: KEY_EG_THRESHOLD, value: String(egThreshold) },
      { key: KEY_EG_STATUS_CODES, value: JSON.stringify(statusCodes) },
      { key: KEY_PG_ACTION, value: pgAction },
      { key: KEY_PG_BAN_MINUTES, value: String(pgBanMinutes) },
      { key: KEY_EG_ACTION, value: egAction },
      { key: KEY_EG_BAN_MINUTES, value: String(egBanMinutes) },
      { key: KEY_PUBLIC_KEY_USERS, value: JSON.stringify(publicKeyUserIds) },
      { key: KEY_WHITELIST, value: JSON.stringify(whitelistUserIds) },
      { key: KEY_RULES, value: JSON.stringify(cleanRules) },
      { key: KEY_IPV4_ORDER, value: JSON.stringify(ipv4Order) },
      { key: KEY_IPV6_ORDER, value: JSON.stringify(ipv6Order) },
      { key: KEY_AUTO_LOOKUP, value: String(autoLookup) },
    ];
    // 密钥字段被通用配置接口脱敏，不会回显；留空表示不修改已保存的密钥。
    if (giteeApiKey.trim() !== '') {
      updates.push({ key: KEY_GITEE_API_KEY, value: giteeApiKey.trim() });
    }

    setSaving(true);
    try {
      const results = await Promise.all(
        updates.map((item) => API.put('/api/option/', item)),
      );
      const failed = results.filter((res) => !res?.data?.success);
      if (failed.length > 0) {
        showError(t('部分保存失败，请重试'));
      } else {
        showSuccess(t('保存成功'));
      }
    } catch (e) {
      showError(e.message);
    }
    setSaving(false);
  };

  const addRule = () => {
    setRules((currentRules) => [
      ...currentRules,
      {
        _id: Date.now(),
        enabled: false,
        metric: 'ip_multi_user',
        window_hours: 24,
        threshold: 3,
        action: 'alert',
      },
    ]);
  };

  const updateRule = (id, patch) => {
    setRules((currentRules) =>
      currentRules.map((r) => (r._id === id ? { ...r, ...patch } : r)),
    );
  };

  const removeRule = (id) => {
    setRules((currentRules) => currentRules.filter((r) => r._id !== id));
  };

  const confirmClearCache = async () => {
    setConfirmClear(false);
    setClearingCache(true);
    try {
      const res = await API.post('/api/ip_info/reset');
      const { success, message, data } = res.data;
      if (success) {
        const count = typeof data?.deleted === 'number' ? data.deleted : 0;
        clearIpInfoCache();
        showSuccess(t('已清空 {{count}} 条缓存', { count }));
      } else {
        showError(message || t('清空失败'));
      }
    } catch (e) {
      showError(e.message);
    }
    setClearingCache(false);
  };

  const ruleColumns = [
    {
      title: t('启用'),
      dataIndex: 'enabled',
      width: 80,
      render: (v, record) => (
        <Switch
          checked={!!v}
          onChange={(checked) => updateRule(record._id, { enabled: checked })}
        />
      ),
    },
    {
      title: t('指标'),
      dataIndex: 'metric',
      render: (v, record) => (
        <Select
          value={v}
          style={{ width: 200 }}
          onChange={(val) => {
            const patch = { metric: val };
            // 封禁 IP 只对 IP 维度指标有效,切到用户维度时回退为仅告警
            if (
              BAN_IP_ACTIONS.includes(record.action) &&
              !IP_DIMENSION_METRICS.includes(val)
            ) {
              patch.action = 'alert';
            }
            updateRule(record._id, patch);
          }}
        >
          <Select.Option value='ip_multi_user'>
            {t('单 IP 关联用户数')}
          </Select.Option>
          <Select.Option value='user_multi_ip'>
            {t('单用户使用 IP 数')}
          </Select.Option>
          <Select.Option value='ip_multi_token'>
            {t('单 IP 使用令牌数(批量测活)')}
          </Select.Option>
          <Select.Option value='user_tiny_request'>
            {t('用户微量请求数(自动测活)')}
          </Select.Option>
          <Select.Option value='user_error_burst'>
            {t('用户错误请求数')}
          </Select.Option>
        </Select>
      ),
    },
    {
      title: t('统计窗口(小时)'),
      dataIndex: 'window_hours',
      render: (v, record) => (
        <InputNumber
          value={v}
          min={1}
          max={168}
          style={{ width: 100 }}
          onChange={(val) => updateRule(record._id, { window_hours: val })}
        />
      ),
    },
    {
      title: t('阈值(超过则触发)'),
      dataIndex: 'threshold',
      render: (v, record) => (
        <InputNumber
          value={v}
          min={1}
          style={{ width: 100 }}
          onChange={(val) => updateRule(record._id, { threshold: val })}
        />
      ),
    },
    {
      title: t('动作'),
      dataIndex: 'action',
      render: (v, record) => (
        <Select
          value={v}
          style={{ width: 180 }}
          onChange={(val) => updateRule(record._id, { action: val })}
        >
          <Select.Option value='alert'>{t('仅告警')}</Select.Option>
          <Select.Option value='disable_user'>
            {t('自动禁用用户')}
          </Select.Option>
          {IP_DIMENSION_METRICS.includes(record.metric) && (
            <Select.Option value='ban_ip'>{t('封禁 IP')}</Select.Option>
          )}
          {IP_DIMENSION_METRICS.includes(record.metric) && (
            <Select.Option value='ban_both'>
              {t('封禁 IP + 禁用用户')}
            </Select.Option>
          )}
        </Select>
      ),
    },
    {
      title: t('封禁时长(分钟)'),
      dataIndex: 'ban_minutes',
      render: (v, record) =>
        DISPOSITION_ACTIONS.includes(record.action) ? (
          <InputNumber
            value={v || 0}
            min={0}
            max={43200}
            style={{ width: 110 }}
            placeholder='0'
            onChange={(val) =>
              updateRule(record._id, { ban_minutes: val > 0 ? val : 0 })
            }
          />
        ) : (
          <Text type='tertiary' size='small'>
            -
          </Text>
        ),
    },
    {
      title: '',
      width: 60,
      render: (_, record) => (
        <Button
          theme='borderless'
          type='danger'
          icon={<IconDelete />}
          onClick={() => removeRule(record._id)}
        />
      ),
    },
  ];

  return (
    <Spin spinning={loading}>
      <Banner
        type='info'
        className='mb-4'
        description={t(
          '自动封禁规则由后台周期扫描执行,只影响普通用户(管理员与全局白名单账号不会被自动处置)。IP/UA 黑名单在请求时实时拦截。全局白名单账号的流量既不被拦截,也不进入自动封禁的排行统计,因此不会连带封掉它使用的 IP;这些请求仍照常出现在滥用排行榜(可用排行榜上的开关过滤)。每条规则可单独选择处置对象(IP / 账号 / 两者)与封禁时长。',
        )}
      />

      <Card className='mb-4' title={t('总开关')}>
        <Space vertical align='start'>
          <Space>
            <Switch checked={enabled} onChange={setEnabled} />
            <Text>{t('启用风控(UA 黑名单拦截与自动封禁扫描的总开关)')}</Text>
          </Space>
          <Space>
            <Text>{t('自动封禁扫描周期(分钟)')}</Text>
            <InputNumber
              value={scanMinutes}
              min={1}
              max={1440}
              style={{ width: 120 }}
              onChange={(v) => setScanMinutes(v > 0 ? v : 10)}
            />
          </Space>
          <Space>
            <Text>{t('拦截/告警事件保留天数(封禁与解禁记录永久保留)')}</Text>
            <InputNumber
              value={retentionDays}
              min={1}
              max={365}
              style={{ width: 120 }}
              onChange={(v) => setRetentionDays(v > 0 ? v : 30)}
            />
          </Space>
          <Space>
            <Text>{t('全局白名单账号 ID(逗号分隔)')}</Text>
            <TextArea
              value={whitelistText}
              onChange={setWhitelistText}
              placeholder='1,2,3'
              rows={1}
              style={{ width: 320 }}
            />
          </Space>
          <Text type='tertiary' size='small'>
            {t(
              '全局白名单账号完全不受风控:请求不被拦截、账号不被自动禁用,且它近 24 小时内使用过的 IP 不会被自动封禁(这些账号的流量也不进入自动封禁扫描的排行统计)。把运营者自己和内部账号填在这里,避免自己的出口地址被自己的规则封掉后连带拦下同出口的其他用户。手动添加的 IP 封禁不受此豁免影响。',
            )}
            {t(
              'IP 豁免依据日志中记录的地址,需要开启 IP 日志(全局或白名单账号自己的设置)才能生效。',
            )}
          </Text>
          {whitelistText.trim() !== '' && !ipLogEnabled && (
            <Banner
              type='warning'
              description={t(
                '全局 IP 日志已关闭:白名单账号的 IP 豁免依赖日志中的 IP 字段,除非这些账号单独开启了 IP 记录,否则实时防护仍会封禁它们使用的出口地址。',
              )}
            />
          )}
          <Space>
            <Text>{t('用户级白名单账号 ID(逗号分隔,仅保护账号)')}</Text>
            <TextArea
              value={publicKeyUsersText}
              onChange={setPublicKeyUsersText}
              placeholder='1,2,3'
              rows={1}
              style={{ width: 320 }}
            />
          </Space>
          <Text type='tertiary' size='small'>
            {t(
              '用户级白名单只保证账号永不被自动禁用,来源 IP 的封禁与请求拦截照常生效。适用于对外分享的公开/共享密钥:滥用者不是账号主人,禁用账号会打死所有正常使用者。想封住滥用者又不影响密钥主人时,把账号填在这里,并把处置动作设为「封禁 IP」或「封禁 IP + 禁用账号」。',
            )}
          </Text>
        </Space>
      </Card>

      <Card className='mb-4' title={t('UA 黑名单')}>
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              '每行一条。普通文本按子串匹配(不区分大小写),含正则元字符时按正则匹配。',
            )}
          </Text>
          <TextArea
            value={uaBlacklistText}
            onChange={setUaBlacklistText}
            placeholder={'curl\npython-requests\n^Go-http-client'}
            rows={6}
          />
          <Space>
            <Text>{t('命中后的动作')}</Text>
            <Select
              value={uaAction}
              onChange={setUaAction}
              style={{ width: 220 }}
            >
              <Select.Option value='block'>
                {t('仅拒绝请求(403)')}
              </Select.Option>
              <Select.Option value='disable_user'>
                {t('拒绝请求并自动禁用用户')}
              </Select.Option>
            </Select>
          </Space>
        </Space>
      </Card>

      <Card className='mb-4' title={t('IP 黑名单')}>
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              '每行一条,支持精确 IP 或 CIDR 网段(如 1.2.3.4、10.0.0.0/8)。命中后直接拒绝调用。',
            )}
          </Text>
          <TextArea
            value={ipBlacklistText}
            onChange={setIpBlacklistText}
            placeholder={'1.2.3.4\n10.0.0.0/8\n2001:db8::/32'}
            rows={6}
          />
        </Space>
      </Card>

      <Card className='mb-4' title={t('IP 归属地查询')}>
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              '用于使用日志与风控页面的 IP 归属地展示。按顺序依次尝试提供方，失败自动切换下一个；结果会缓存到数据库，同一 IP 只查询一次。多选框的选择顺序即查询顺序。',
            )}
          </Text>
          <Space>
            <Text>{t('Gitee AI 密钥')}</Text>
            <Input
              value={giteeApiKey}
              onChange={setGiteeApiKey}
              mode='password'
              placeholder={t('已保存的密钥不会回显；留空表示不修改')}
              style={{ width: 360 }}
            />
          </Space>
          <Space>
            <Text>{t('IPv4 查询顺序')}</Text>
            <Select
              multiple
              value={ipv4Order}
              onChange={setIpv4Order}
              optionList={IP_PROVIDER_OPTIONS}
              style={{ minWidth: 420 }}
            />
          </Space>
          <Space>
            <Text>{t('IPv6 查询顺序')}</Text>
            <Select
              multiple
              value={ipv6Order}
              onChange={setIpv6Order}
              optionList={IP_PROVIDER_OPTIONS.filter(
                (o) => o.value !== 'gitee',
              )}
              style={{ minWidth: 420 }}
            />
          </Space>
          <Space>
            <Text>{t('自动预取新 IP 归属地')}</Text>
            <Switch checked={autoLookup} onChange={setAutoLookup} />
          </Space>
          <Text type='tertiary' size='small'>
            {t(
              'relay 请求的新客户端 IP 会在后台自动查询并缓存到数据库，日志展示无需等待。',
            )}
          </Text>
          <Space>
            <Button
              type='danger'
              theme='borderless'
              icon={<IconRefresh />}
              loading={clearingCache}
              onClick={() => setConfirmClear(true)}
            >
              {t('清空归属地缓存')}
            </Button>
            <Text type='tertiary' size='small'>
              {t(
                '清空后下次查询会重新拉取外部接口，可切换数据源或升级后刷新。',
              )}
            </Text>
          </Space>
        </Space>
      </Card>

      <Modal
        title={t('确认清空归属地缓存')}
        visible={confirmClear}
        onOk={confirmClearCache}
        onCancel={() => setConfirmClear(false)}
        okType='danger'
        okText={t('确认清空')}
        cancelText={t('取消')}
      >
        <Text>
          {t('确认清空所有 IP 归属地缓存？下次查询将重新拉取外部接口。')}
        </Text>
      </Modal>

      <Card className='mb-4' title={t('Probe Guard(实时测活防护)')}>
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              '在请求处理链路中实时检测「单 IP 短时间遍历多个不同模型」的批量测活行为,命中即拒绝请求并执行下方的处置动作(默认只封来源 IP,避免误伤被盗密钥的主人)。私网 IP、全局白名单账号与管理员豁免。建议先以演练模式观察告警,再关闭演练正式启用。',
            )}
          </Text>
          <Space>
            <Switch checked={pgEnabled} onChange={setPgEnabled} />
            <Text>{t('启用 Probe Guard')}</Text>
          </Space>
          <Space>
            <Switch checked={pgDryRun} onChange={setPgDryRun} />
            <Text>{t('演练模式(只记告警事件,不实际封禁)')}</Text>
          </Space>
          <Space wrap>
            <Text>{t('滑动窗口(秒)')}</Text>
            <InputNumber
              value={pgWindow}
              min={10}
              max={3600}
              style={{ width: 120 }}
              onChange={(v) => setPgWindow(v > 0 ? v : 60)}
            />
            <Text>{t('不同模型数阈值')}</Text>
            <InputNumber
              value={pgModelCount}
              min={2}
              max={1000}
              style={{ width: 120 }}
              onChange={(v) => setPgModelCount(v > 0 ? v : 5)}
            />
          </Space>
          <Space wrap>
            <Text>{t('处置动作')}</Text>
            <Select
              value={pgAction}
              style={{ width: 200 }}
              onChange={setPgAction}
            >
              <Select.Option value='alert'>{t('仅告警')}</Select.Option>
              <Select.Option value='ban_ip'>{t('封禁 IP')}</Select.Option>
              <Select.Option value='disable_user'>
                {t('禁用账号')}
              </Select.Option>
              <Select.Option value='ban_both'>
                {t('封禁 IP + 禁用账号')}
              </Select.Option>
            </Select>
            <Text>{t('封禁时长(分钟,0 表示走升级阶梯)')}</Text>
            <InputNumber
              value={pgBanMinutes}
              min={0}
              max={43200}
              style={{ width: 120 }}
              onChange={(v) => setPgBanMinutes(v > 0 ? v : 0)}
            />
          </Space>
        </Space>
      </Card>

      <Card className='mb-4' title={t('Error Guard(实时错误率防护)')}>
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              '在响应完成后统计「单 IP 短时间内被拒绝多少次」,只统计指定的状态码,达到阈值即执行下方的处置动作。能抓到反复拿无效密钥试探(401)、乱传参数(400)这类只在响应里才现形的行为。私网 IP、全局白名单账号与管理员豁免,风控自身的拦截不计入,已封禁 IP 的错误也不再累加。建议先以演练模式观察告警,再关闭演练正式启用。',
            )}
          </Text>
          <Space>
            <Switch checked={egEnabled} onChange={setEgEnabled} />
            <Text>{t('启用 Error Guard')}</Text>
          </Space>
          <Space>
            <Switch checked={egDryRun} onChange={setEgDryRun} />
            <Text>{t('演练模式(只记告警事件,不实际封禁)')}</Text>
          </Space>
          <Space wrap>
            <Text>{t('滑动窗口(秒)')}</Text>
            <InputNumber
              value={egWindow}
              min={10}
              max={3600}
              style={{ width: 120 }}
              onChange={(v) => setEgWindow(v > 0 ? v : 60)}
            />
            <Text>{t('错误次数阈值')}</Text>
            <InputNumber
              value={egThreshold}
              min={2}
              max={100000}
              style={{ width: 120 }}
              onChange={(v) => setEgThreshold(v > 0 ? v : 5)}
            />
          </Space>
          <Space wrap>
            <Text>{t('处置动作')}</Text>
            <Select
              value={egAction}
              style={{ width: 200 }}
              onChange={setEgAction}
            >
              <Select.Option value='alert'>{t('仅告警')}</Select.Option>
              <Select.Option value='ban_ip'>{t('封禁 IP')}</Select.Option>
              <Select.Option value='disable_user'>
                {t('禁用账号')}
              </Select.Option>
              <Select.Option value='ban_both'>
                {t('封禁 IP + 禁用账号')}
              </Select.Option>
            </Select>
            <Text>{t('封禁时长(分钟,0 表示走升级阶梯)')}</Text>
            <InputNumber
              value={egBanMinutes}
              min={0}
              max={43200}
              style={{ width: 120 }}
              onChange={(v) => setEgBanMinutes(v > 0 ? v : 0)}
            />
          </Space>
          <Text>{t('统计的状态码(逗号分隔)')}</Text>
          <Input
            value={egStatusCodesText}
            onChange={setEgStatusCodesText}
            placeholder='400,401,403,404'
            style={{ width: 320 }}
          />
          <Text type='tertiary' size='small'>
            {t(
              '留空使用默认集合 400 / 401 / 403 / 404。有意不含 5xx:上游故障期间的服务端错误不该让正常用户被封,确有需要可自行加入。',
            )}
          </Text>
        </Space>
      </Card>

      <Card
        className='mb-4'
        title={t('自动封禁规则')}
        headerExtraContent={
          <Button icon={<IconPlus />} onClick={addRule}>
            {t('添加规则')}
          </Button>
        }
      >
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text type='tertiary'>
            {t(
              'IP 维度规则(单 IP 关联用户数 / 单 IP 使用令牌数)触发「自动禁用用户」时,会处置该 IP 在窗口内关联的全部用户;「封禁 IP」按升级阶梯只封来源 IP,误伤更小。用户维度规则只处置命中用户。每条规则的封禁时长对 IP 与账号同样生效,留 0 表示走下方的升级阶梯。建议新规则先用「仅告警」观察一段时间再切换为自动处置。',
            )}
          </Text>
          <Space>
            <Text>{t('微量请求判定阈值(输入与输出 tokens 均不超过该值)')}</Text>
            <InputNumber
              value={tinyMaxTokens}
              min={1}
              max={1024}
              style={{ width: 120 }}
              onChange={(v) => setTinyMaxTokens(v > 0 ? v : 16)}
            />
          </Space>
          <Text>{t('封禁升级阶梯(分钟)')}</Text>
          <Space wrap align='center'>
            {banLadder.map((minutes, index) => (
              <Space key={index} spacing={4} align='center'>
                <Text type='tertiary' size='small'>
                  {index === 0
                    ? t('首次')
                    : t('第 {{count}} 次', { count: index + 1 })}
                </Text>
                <InputNumber
                  value={minutes}
                  min={1}
                  max={43200}
                  style={{ width: 100 }}
                  onChange={(v) =>
                    setBanLadder((steps) =>
                      steps.map((step, i) => (i === index ? v || 1 : step)),
                    )
                  }
                />
                {banLadder.length > 1 && (
                  <Button
                    icon={<IconDelete />}
                    type='danger'
                    theme='borderless'
                    size='small'
                    aria-label={t('删除该级')}
                    onClick={() =>
                      setBanLadder((steps) =>
                        steps.filter((_, i) => i !== index),
                      )
                    }
                  />
                )}
              </Space>
            ))}
            {banLadder.length < MAX_BAN_LADDER_STEPS && (
              <Button
                icon={<IconPlus />}
                size='small'
                onClick={() =>
                  setBanLadder((steps) => [
                    ...steps,
                    steps[steps.length - 1] || 10,
                  ])
                }
              >
                {t('添加一级')}
              </Button>
            )}
          </Space>
          <Space wrap>
            <Text>{t('第 N 次起永久封禁(0 表示永不永久)')}</Text>
            <InputNumber
              value={ipBanPermanent}
              min={0}
              max={100}
              style={{ width: 90 }}
              onChange={(v) => setIpBanPermanent(v >= 0 ? v : 3)}
            />
            <Text>{t('IPv6 封禁前缀长度')}</Text>
            <InputNumber
              value={ipv6PrefixLength}
              min={32}
              max={128}
              style={{ width: 90 }}
              onChange={(v) => setIpv6PrefixLength(v > 0 ? v : 64)}
            />
          </Space>
          <Text type='tertiary' size='small'>
            {t(
              '升级阶梯对 IP 封禁与账号禁用共同生效,自动封禁规则、Probe Guard 与 Error Guard 都用它:第 N 次违规用第 N 级时长,超出阶梯级数则停在最后一级,规则里填了固定时长则用固定值。违规次数分别按该 IP、该账号近 90 天内的封禁事件累计。账号的临时禁用到期后由后台每分钟检查并自动恢复,期间管理员手动封禁的账号不受影响。',
            )}
          </Text>
          <Text type='tertiary' size='small'>
            {t(
              '自动封禁 IPv6 地址时归并到该长度的前缀(默认 64)。运营商给客户端的是一整段前缀,客户端可在段内随意换地址,只封单个地址等于没封。128 表示按单地址封禁。IPv4 与手动添加的封禁不受影响。',
            )}
          </Text>
          <Table
            columns={ruleColumns}
            dataSource={rules}
            rowKey='_id'
            pagination={false}
            empty={t('暂无规则')}
          />
        </Space>
      </Card>

      <Button
        theme='solid'
        type='primary'
        loading={saving}
        onClick={saveSettings}
      >
        {t('保存设置')}
      </Button>
    </Spin>
  );
};

export default RiskSettings;
