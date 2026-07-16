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
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Text, Title } = Typography;

const KEY_PREFIX = 'risk_control_setting.';
const KEY_ENABLED = `${KEY_PREFIX}enabled`;
const KEY_UA_BLACKLIST = `${KEY_PREFIX}ua_blacklist`;
const KEY_UA_ACTION = `${KEY_PREFIX}ua_blacklist_action`;
const KEY_IP_BLACKLIST = `${KEY_PREFIX}ip_blacklist`;
const KEY_SCAN_MINUTES = `${KEY_PREFIX}scan_minutes`;
const KEY_WHITELIST = `${KEY_PREFIX}whitelist_user_ids`;
const KEY_RULES = `${KEY_PREFIX}auto_ban_rules`;

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
        setUaBlacklistText(parseJsonArray(optionMap[KEY_UA_BLACKLIST]).join('\n'));
        setUaAction(optionMap[KEY_UA_ACTION] || 'block');
        setIpBlacklistText(parseJsonArray(optionMap[KEY_IP_BLACKLIST]).join('\n'));
        const scan = parseInt(optionMap[KEY_SCAN_MINUTES], 10);
        setScanMinutes(Number.isFinite(scan) && scan > 0 ? scan : 10);
        setWhitelistText(parseJsonArray(optionMap[KEY_WHITELIST]).join(','));
        setRules(
          parseJsonArray(optionMap[KEY_RULES]).map((r, idx) => ({
            ...r,
            _id: idx,
          })),
        );
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
    ];

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
    setRules([
      ...rules,
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
    setRules(rules.map((r) => (r._id === id ? { ...r, ...patch } : r)));
  };

  const removeRule = (id) => {
    setRules(rules.filter((r) => r._id !== id));
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
          <Select.Option value='disable_user'>{t('自动禁用用户')}</Select.Option>
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
          '自动封禁规则由后台周期扫描执行,只影响普通用户(管理员与白名单用户不会被自动处置)。UA 黑名单在请求时实时拦截。',
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
            <Text>{t('白名单用户 ID(逗号分隔,永不自动处置)')}</Text>
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
            {t('每行一条。普通文本按子串匹配(不区分大小写),含正则元字符时按正则匹配。')}
          </Text>
          <TextArea
            value={uaBlacklistText}
            onChange={setUaBlacklistText}
            placeholder={'curl\npython-requests\n^Go-http-client'}
            rows={6}
          />
          <Space>
            <Text>{t('命中后的动作')}</Text>
            <Select value={uaAction} onChange={setUaAction} style={{ width: 220 }}>
              <Select.Option value='block'>{t('仅拒绝请求(403)')}</Select.Option>
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
            {t('每行一条,支持精确 IP 或 CIDR 网段(如 1.2.3.4、10.0.0.0/8)。命中后直接拒绝调用。')}
          </Text>
          <TextArea
            value={ipBlacklistText}
            onChange={setIpBlacklistText}
            placeholder={'1.2.3.4\n10.0.0.0/8\n2001:db8::/32'}
            rows={6}
          />
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
        <Table
          columns={ruleColumns}
          dataSource={rules}
          rowKey='_id'
          pagination={false}
          empty={t('暂无规则')}
        />
      </Card>

      <Button theme='solid' type='primary' loading={saving} onClick={saveSettings}>
        {t('保存设置')}
      </Button>
    </Spin>
  );
};

export default RiskSettings;
