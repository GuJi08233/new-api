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
const KEY_RULES = `${KEY_PREFIX}auto_ban_rules`;

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
  const [rules, setRules] = useState([]);
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
        setWhitelistText(parseJsonArray(optionMap[KEY_WHITELIST]).join(','));
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
    const whitelistUserIds = whitelistText
      .split(/[,，\s]+/)
      .map((s) => parseInt(s, 10))
      .filter((n) => Number.isFinite(n) && n > 0);
    const cleanRules = rules.map(({ _id, ...rest }) => ({
      enabled: !!rest.enabled,
      metric: rest.metric || 'ip_multi_user',
      window_hours: rest.window_hours > 0 ? rest.window_hours : 24,
      threshold: rest.threshold > 0 ? rest.threshold : 1,
      action: rest.action || 'alert',
    }));

    const updates = [
      { key: KEY_ENABLED, value: String(enabled) },
      { key: KEY_UA_BLACKLIST, value: JSON.stringify(uaBlacklist) },
      { key: KEY_UA_ACTION, value: uaAction },
      { key: KEY_IP_BLACKLIST, value: JSON.stringify(ipBlacklist) },
      { key: KEY_SCAN_MINUTES, value: String(scanMinutes) },
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
          style={{ width: 180 }}
          onChange={(val) => updateRule(record._id, { metric: val })}
        >
          <Select.Option value='ip_multi_user'>
            {t('单 IP 关联用户数')}
          </Select.Option>
          <Select.Option value='user_multi_ip'>
            {t('单用户使用 IP 数')}
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
          style={{ width: 140 }}
          onChange={(val) => updateRule(record._id, { action: val })}
        >
          <Select.Option value='alert'>{t('仅告警')}</Select.Option>
          <Select.Option value='disable_user'>
            {t('自动禁用用户')}
          </Select.Option>
        </Select>
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
          '自动封禁规则由后台周期扫描执行,只影响普通用户(管理员与白名单用户不会被自动处置)。IP/UA 黑名单在请求时实时拦截,白名单用户完全豁免拦截,但其请求仍计入风控统计。',
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
            <Text>{t('白名单用户 ID(逗号分隔,完全豁免风控,仍计入统计)')}</Text>
            <TextArea
              value={whitelistText}
              onChange={setWhitelistText}
              placeholder='1,2,3'
              rows={1}
              style={{ width: 320 }}
            />
          </Space>
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

      <Card
        className='mb-4'
        title={t('自动封禁规则')}
        headerExtraContent={
          <Button icon={<IconPlus />} onClick={addRule}>
            {t('添加规则')}
          </Button>
        }
      >
        <Table
          columns={ruleColumns}
          dataSource={rules}
          rowKey='_id'
          pagination={false}
          empty={t('暂无规则')}
        />
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
