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

import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  CheckboxGroup,
  Input,
  InputNumber,
  Spin,
  Switch,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showSuccess,
  showWarning,
  timestamp2string,
} from '../../helpers';

const { Text, Title } = Typography;

// 模拟盘管理页：行情连接状态、全站汇总与配置。配置通过通用的配置接口逐项保存，后端逐项校验取值范围，保存后立即生效。
const NUMBER_FIELDS = [
  ['fee_bps', '手续费(万分之一)', 'max_fee_bps', 0],
  ['max_order_usd', '单笔最多成交金额(美元)', 'max_order_usd', 1],
  ['max_position_usd', '每个交易对最多持仓(美元)', 'max_position_usd', 1],
  [
    'daily_profit_out_usd',
    '每人每天最多转出的盈利(美元，0 为不限制)',
    'max_profit_out',
    0,
  ],
  ['stale_ms', '等待最新盘口的最长时间(毫秒)', 'max_stale_ms', 'min_stale_ms'],
];
const TEXT_FIELDS = [
  ['rest_url', 'Binance REST 地址'],
  ['ws_url', 'Binance WebSocket 地址'],
  ['proxy_url', '代理地址(http 或 socks5，留空按环境变量)'],
];

function optionValue(value) {
  if (typeof value === 'boolean') return String(value);
  if (Array.isArray(value)) return JSON.stringify(value);
  return String(value ?? '');
}

const TradeAdmin = () => {
  const { t } = useTranslation();
  const [status, setStatus] = useState(null);
  const [form, setForm] = useState(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    const res = await API.get('/api/trade/admin/status');
    if (!res.data.success) {
      showError(res.data.message);
      return;
    }
    setStatus(res.data.data);
    setForm({
      ...res.data.data.setting,
      symbols: [...(res.data.data.setting.symbols || [])],
    });
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    const timer = setInterval(async () => {
      const res = await API.get('/api/trade/admin/status');
      if (res.data.success) setStatus(res.data.data);
    }, 5000);
    return () => clearInterval(timer);
  }, []);

  if (!status || !form) {
    return (
      <div className='mt-[60px] flex justify-center px-2 py-10'>
        <Spin size='large' />
      </div>
    );
  }

  const { market, stats, defaults, limits, symbols } = status;
  const perUnit = status.quota_per_unit || 1;
  const usd = (quota) => (Number(quota || 0) / perUnit).toFixed(2);
  const change = (key, value) =>
    setForm((previous) => ({ ...previous, [key]: value }));

  const save = async () => {
    const changed = Object.keys(form).filter(
      (key) => optionValue(form[key]) !== optionValue(status.setting[key]),
    );
    if (!changed.length) {
      showWarning(t('你似乎并没有修改什么'));
      return;
    }
    setSaving(true);
    for (const key of changed) {
      const res = await API.put('/api/option/', {
        key: `trade_setting.${key}`,
        value: optionValue(form[key]),
      });
      if (!res.data.success) {
        setSaving(false);
        showError(res.data.message);
        await load();
        return;
      }
    }
    setSaving(false);
    showSuccess(t('保存成功'));
    load();
  };

  const restricted = market.last_error.includes('http 451');
  const symbolColumns = [
    { title: t('交易对'), dataIndex: 'symbol' },
    {
      title: t('盘口'),
      dataIndex: 'has_book',
      render: (value, item) =>
        value ? (
          <Tag color='green'>
            {t('{{seconds}} 秒前更新', {
              seconds: (item.book_age_ms / 1000).toFixed(1),
            })}
          </Tag>
        ) : (
          <Tag color='orange'>{t('暂无')}</Tag>
        ),
    },
    {
      title: t('交易规则'),
      dataIndex: 'has_rules',
      render: (value) =>
        value ? (
          <Tag color='green'>{t('已获取')}</Tag>
        ) : (
          <Tag color='orange'>{t('暂无')}</Tag>
        ),
    },
    {
      title: t('最新价'),
      dataIndex: 'price',
      render: (value) => value || '--',
    },
  ];

  return (
    <div className='mt-[60px] px-2 pb-6'>
      <div className='mx-auto flex max-w-[1200px] flex-col gap-4'>
        <Title heading={3}>{t('模拟盘管理')}</Title>
        <Card title={t('行情连接')}>
          <div className='flex flex-col gap-3'>
            <div className='flex flex-wrap items-center gap-2'>
              {!market.running ? (
                <Tag color='grey'>
                  {t('未运行(模拟盘关闭或没有开放的交易对)')}
                </Tag>
              ) : market.connected ? (
                <Tag color='green'>{t('已连接')}</Tag>
              ) : (
                <Tag color='orange'>{t('连接中')}</Tag>
              )}
              <Text type='tertiary' size='small'>
                {t('每个节点各连一条行情，这里显示当前节点的状态。')}
              </Text>
            </div>
            {market.last_error && (
              <Banner
                type={restricted ? 'danger' : 'warning'}
                closeIcon={null}
                description={
                  <div className='flex flex-col gap-1'>
                    <span>
                      {t('最近一次错误')}({timestamp2string(market.error_at)})：
                      {market.last_error}
                    </span>
                    {restricted && (
                      <span>
                        {t(
                          'Binance 拒绝了服务器所在地区(451)，请改用行情镜像地址或配置代理。',
                        )}
                      </span>
                    )}
                  </div>
                }
              />
            )}
            {market.running && (
              <Table
                size='small'
                rowKey='symbol'
                columns={symbolColumns}
                dataSource={market.symbols || []}
                pagination={false}
              />
            )}
          </div>
        </Card>
        <Card title={t('全站汇总')}>
          <div className='grid grid-cols-2 gap-4 md:grid-cols-5'>
            {[
              [t('账户数'), stats.accounts],
              [t('资金合计'), `${usd(stats.cash + stats.frozen)} USDT`],
              [t('持仓成本'), `${usd(stats.position_cost)} USDT`],
              [t('累计净转入'), `${usd(stats.net_in)} USDT`],
              [t('挂着的委托'), stats.open_orders],
            ].map(([label, value]) => (
              <div key={label}>
                <Text type='tertiary' size='small'>
                  {label}
                </Text>
                <div className='text-lg font-semibold'>{value}</div>
              </div>
            ))}
          </div>
        </Card>
        <Card title={t('模拟盘配置')}>
          <div className='flex flex-col gap-5'>
            <Banner
              type='info'
              closeIcon={null}
              description={t(
                '账户里的钱来自用户转入的额度，1 USDT = 1 美元额度，不送初始资金。只有可用资金能转出，超过转入额度的部分算盈利，受每天的上限约束。',
              )}
            />
            <div className='flex items-center gap-3'>
              <Switch
                checked={form.enabled}
                onChange={(value) => change('enabled', value)}
              />
              <Text>{t('开放模拟盘')}</Text>
              <Text type='tertiary' size='small'>
                {t('关闭后不能下单和转入；已有的委托可以撤销，资金可以转出。')}
              </Text>
            </div>
            <div className='flex flex-col gap-2'>
              <Text strong>{t('开放交易的交易对')}</Text>
              <CheckboxGroup
                value={form.symbols}
                onChange={(value) => change('symbols', value)}
              >
                {['crypto', 'stock'].map((kind) => (
                  <div
                    key={kind}
                    className='flex flex-wrap items-center gap-x-4 gap-y-2'
                  >
                    <Text type='tertiary' size='small' style={{ width: 72 }}>
                      {kind === 'crypto' ? t('加密货币') : t('美股代币')}
                    </Text>
                    {symbols
                      .filter((item) => item.kind === kind)
                      .map((item) => (
                        <Checkbox key={item.symbol} value={item.symbol}>
                          {item.ticker}
                        </Checkbox>
                      ))}
                  </div>
                ))}
              </CheckboxGroup>
            </div>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              {NUMBER_FIELDS.map(([key, label, maxKey, minValue]) => {
                const min =
                  typeof minValue === 'string' ? limits[minValue] : minValue;
                return (
                  <div key={key} className='flex flex-col gap-1'>
                    <Text strong>{t(label)}</Text>
                    <InputNumber
                      value={form[key]}
                      min={min}
                      max={limits[maxKey]}
                      precision={0}
                      onChange={(value) => change(key, value ?? 0)}
                    />
                    <Text type='tertiary' size='small'>
                      {t('默认值 {{value}}，可填 {{min}} 到 {{max}}', {
                        value: defaults[key],
                        min,
                        max: limits[maxKey],
                      })}
                      {key === 'fee_bps'
                        ? `；${t('当前为 {{percent}}%', { percent: (form.fee_bps / 100).toFixed(2) })}`
                        : ''}
                    </Text>
                  </div>
                );
              })}
              {TEXT_FIELDS.map(([key, label]) => (
                <div key={key} className='flex flex-col gap-1'>
                  <Text strong>{t(label)}</Text>
                  <Input
                    value={form[key]}
                    onChange={(value) => change(key, value.trim())}
                  />
                  <Text type='tertiary' size='small'>
                    {t('默认值 {{value}}', { value: defaults[key] || t('空') })}
                  </Text>
                </div>
              ))}
            </div>
            <div>
              <Button theme='solid' loading={saving} onClick={save}>
                {t('保存')}
              </Button>
            </div>
          </div>
        </Card>
      </div>
    </div>
  );
};

export default TradeAdmin;
