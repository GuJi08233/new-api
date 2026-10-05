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

// 模拟盘管理页：现货与合约的行情连接状态、全站汇总与配置。配置通过通用的配置接口逐项保存，后端逐项校验取值范围，
// 保存后立即生效。
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
const FUTURES_NUMBER_FIELDS = [
  [
    'futures_max_leverage',
    '合约最高杠杆(倍，同时不超过各合约在 Binance 的上限)',
    'max_leverage',
    1,
  ],
  ['futures_taker_fee_bps', '合约吃单手续费(万分之一)', 'max_fee_bps', 0],
  ['futures_maker_fee_bps', '合约挂单手续费(万分之一)', 'max_fee_bps', 0],
  [
    'futures_max_position_usd',
    '每个合约最多持仓价值(美元，多空合计，含挂单)',
    'max_position_usd',
    1,
  ],
];
const FUTURES_TEXT_FIELDS = [
  ['futures_rest_url', 'Binance 合约 REST 地址'],
  ['futures_ws_url', 'Binance 合约 WebSocket 地址'],
];

function optionValue(value) {
  if (typeof value === 'boolean') return String(value);
  if (Array.isArray(value)) return JSON.stringify(value);
  return String(value ?? '');
}

// 一条行情连接(现货或合约)的状态：是否在运行、是否连上、最近一次错误，以及可以展开的每个交易对的盘口、规则和价格。
const MarketStatus = ({ title, market, futures, idleText, t }) => {
  const [expanded, setExpanded] = useState(false);
  const restricted = market.last_error.includes('http 451');
  const connected = market.connected && (!futures || market.data_connected);
  const items = market.symbols || [];
  const columns = [
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
    ...(futures
      ? [
          {
            title: t('标记价格'),
            dataIndex: 'mark',
            render: (value, item) =>
              value
                ? `${value} (${t('{{seconds}} 秒前更新', {
                    seconds: (item.mark_age_ms / 1000).toFixed(1),
                  })})`
                : '--',
          },
        ]
      : []),
  ];

  return (
    <Card title={title}>
      <div className='flex flex-col gap-3'>
        <div className='flex flex-wrap items-center gap-2'>
          {!market.running ? (
            <Tag color='grey'>{idleText}</Tag>
          ) : connected ? (
            <Tag color='green'>{t('已连接')}</Tag>
          ) : (
            <Tag color='orange'>{t('连接中')}</Tag>
          )}
          <Text type='tertiary' size='small'>
            {t('每个节点各连一条行情，这里显示当前节点的状态。')}
          </Text>
          {market.running && (
            <>
              <Text size='small'>
                {t(
                  '{{books}}/{{total}} 个交易对有盘口，{{rules}}/{{total}} 个有交易规则',
                  {
                    books: items.filter((item) => item.has_book).length,
                    rules: items.filter((item) => item.has_rules).length,
                    total: items.length,
                  },
                )}
              </Text>
              <Button
                size='small'
                theme='borderless'
                onClick={() => setExpanded(!expanded)}
              >
                {expanded ? t('收起') : t('展开明细')}
              </Button>
            </>
          )}
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
                    {futures
                      ? t(
                          'Binance 拒绝了服务器所在地区(451)，请把合约 REST 地址改成 https://www.binance.com，或者配置代理。',
                        )
                      : t(
                          'Binance 拒绝了服务器所在地区(451)，请改用行情镜像地址或配置代理。',
                        )}
                  </span>
                )}
              </div>
            }
          />
        )}
        {market.running && expanded && (
          <Table
            size='small'
            rowKey='symbol'
            columns={columns}
            dataSource={items}
            pagination={false}
          />
        )}
      </div>
    </Card>
  );
};

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
      futures_symbols: [...(res.data.data.setting.futures_symbols || [])],
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
  const futuresMarket = status.futures_market;
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

  const numberField = ([key, label, maxKey, minValue]) => {
    const min = typeof minValue === 'string' ? limits[minValue] : minValue;
    const percent = key.endsWith('fee_bps')
      ? `；${t('当前为 {{percent}}%', { percent: (form[key] / 100).toFixed(2) })}`
      : '';
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
          {percent}
        </Text>
      </div>
    );
  };
  const textField = ([key, label]) => (
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
  );
  const symbolChoices = (field, key) => (
    <CheckboxGroup value={form[key]} onChange={(value) => change(key, value)}>
      {[
        ['crypto', t('加密货币')],
        ['stock', field === 'futures' ? t('美股永续') : t('美股代币')],
        ['commodity', t('大宗商品')],
      ].map(([kind, label]) => {
        const items = symbols.filter(
          (item) => item.kind === kind && item[field],
        );
        if (!items.length) return null;
        return (
          <div
            key={kind}
            className='flex flex-wrap items-center gap-x-4 gap-y-2'
          >
            <Text type='tertiary' size='small' style={{ width: 72 }}>
              {label}
            </Text>
            {items.map((item) => (
              <Checkbox key={item[field]} value={item[field]}>
                {item.ticker}
              </Checkbox>
            ))}
          </div>
        );
      })}
    </CheckboxGroup>
  );

  return (
    <div className='mt-[60px] px-2 pb-6'>
      <div className='mx-auto flex max-w-[1200px] flex-col gap-4'>
        <Title heading={3}>{t('模拟盘管理')}</Title>
        <MarketStatus
          title={t('现货行情连接')}
          market={market}
          idleText={t('未运行(模拟盘关闭或没有开放的交易对)')}
          t={t}
        />
        <MarketStatus
          title={t('合约行情连接')}
          market={futuresMarket}
          futures
          idleText={t('未运行(合约关闭并且没有人持仓)')}
          t={t}
        />
        <Card title={t('全站汇总')}>
          <div className='grid grid-cols-2 gap-4 md:grid-cols-4'>
            {[
              [t('账户数'), stats.accounts],
              [t('资金合计'), `${usd(stats.cash + stats.frozen)} USDT`],
              [t('持仓成本'), `${usd(stats.position_cost)} USDT`],
              [t('累计净转入'), `${usd(stats.net_in)} USDT`],
              [t('挂着的委托'), stats.open_orders],
              [t('合约仓位'), stats.futures_positions],
              [t('合约保证金'), `${usd(stats.futures_margin)} USDT`],
              [t('挂着的合约委托'), stats.futures_open_orders],
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
              <Text className='whitespace-nowrap'>{t('开放模拟盘')}</Text>
              <Text type='tertiary' size='small'>
                {t('关闭后不能下单和转入；已有的委托可以撤销，资金可以转出。')}
              </Text>
            </div>
            <div className='flex items-center gap-3'>
              <Switch
                checked={form.leaderboard_enabled}
                onChange={(value) => change('leaderboard_enabled', value)}
              />
              <Text className='whitespace-nowrap'>{t('开放排行榜')}</Text>
              <Text type='tertiary' size='small'>
                {t(
                  '买入过现货或开过合约的用户按累计盈亏、收益率与总资产排名，所有用户都能看到前 100 名的显示名与金额，名单每 5 分钟最多更新一次。',
                )}
              </Text>
            </div>
            <div className='flex items-center gap-3'>
              <Switch
                checked={form.insights_enabled}
                aria-label={t('市场资讯')}
                onChange={(value) => change('insights_enabled', value)}
              />
              <Text className='whitespace-nowrap'>{t('开放市场资讯')}</Text>
              <Text type='tertiary' size='small'>
                {t(
                  '财经日历、强平快照、大户持仓样本、新闻快讯与公司资料。外部数据源中断时只影响对应栏目，不影响交易。',
                )}
              </Text>
            </div>
            <div className='flex flex-col gap-2'>
              <Text strong>{t('开放交易的交易对')}</Text>
              {symbolChoices('symbol', 'symbols')}
            </div>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              {NUMBER_FIELDS.map(numberField)}
              {TEXT_FIELDS.map(textField)}
            </div>
            <Title heading={5} className='!mb-0'>
              {t('永续合约')}
            </Title>
            <div className='flex items-center gap-3'>
              <Switch
                checked={form.futures_enabled}
                onChange={(value) => change('futures_enabled', value)}
              />
              <Text className='whitespace-nowrap'>{t('开放合约')}</Text>
              <Text type='tertiary' size='small'>
                {t(
                  '可选全仓或逐仓，模拟盘本身也要开放。关闭后不能开仓和反手；已有的仓位照样可以平仓、调杠杆、调整逐仓保证金和止盈止损，强平与资金费照常进行。价格跳空越过强平价时，超出保证金的亏损由用户承担：先扣模拟盘资金，不够的从站内额度扣，额度可以扣成负数。',
                )}
              </Text>
            </div>
            <div className='flex flex-col gap-2'>
              <Text strong>{t('开放开仓的合约')}</Text>
              {symbolChoices('futures', 'futures_symbols')}
            </div>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              {FUTURES_NUMBER_FIELDS.map(numberField)}
              {FUTURES_TEXT_FIELDS.map(textField)}
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
