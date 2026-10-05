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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Empty,
  Pagination,
  Radio,
  RadioGroup,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { ExternalLink, RefreshCw } from 'lucide-react';
import { timestamp2string } from '../../helpers';
import { formatPrice, formatQty, tradeGet } from './api';

const { Text } = Typography;
const PAGE_SIZE = 20;
const SECTIONS = [
  ['calendar', '财经日历'],
  ['liquidations', '强平快照'],
  ['whales', '大户持仓样本'],
  ['news', '新闻快讯'],
  ['companies', '公司资料'],
];
const COVERAGE = {
  global_high_importance: '全球重要经济事件：过去 3 天与未来 7 天。',
  binance_usdm_snapshots:
    '仅 Binance USDⓈ-M 合约强平快照；每个交易对每秒最多一条，不代表全市场或完整成交。本站仅保留启动后最近 24 小时内的最多 200 条记录。',
  leaderboard_top20_sample:
    '仅 Hyperliquid 公开榜单中账户价值至少 100 万美元的前 20 个账户样本，统计 BTC、ETH、SOL、XRP、DOGE 中价值至少 10 万美元的仓位。',
  important_news_cn: 'BlockBeats 中文重要快讯，保留来源原文。',
  static_company_identity:
    '静态公司或基金身份资料，未接入实时财务数据；资料日期未知时请以官网为准。',
};
const SOURCE_ERRORS = {
  api_key_missing: '新闻服务尚未配置，请联系管理员。',
  feature_disabled: '此栏目尚未启用。',
  upstream_unavailable: '数据源暂时不可用，请稍后刷新。',
  request_cancelled: '数据请求未完成，请刷新重试。',
  connecting: '正在连接数据源，请稍后刷新。',
  partial_upstream_failure: '部分样本获取失败，当前结果仅包含成功获取的账户。',
};

function InsightTime({ value, t }) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds <= 0) return t('时间未知');
  const date = new Date(seconds * 1000);
  if (!Number.isFinite(date.getTime())) return t('时间未知');
  return <time dateTime={date.toISOString()}>{timestamp2string(seconds)}</time>;
}

// 外部新闻和官网链接共用协议校验；正文始终由 React 按文本渲染。
function InsightLink({ href, children }) {
  let url;
  try {
    url = new URL(href);
  } catch {
    return null;
  }
  if (
    !['https:', 'http:'].includes(url.protocol) ||
    !url.hostname ||
    url.username ||
    url.password
  ) {
    return null;
  }
  return (
    <a
      href={url.href}
      target='_blank'
      rel='noopener noreferrer'
      className='inline-flex items-center gap-1 text-blue-500'
    >
      {children}
      <ExternalLink size={12} aria-hidden='true' />
    </a>
  );
}

const InsightsPanel = ({ kind, t }) => {
  const [view, setView] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const requestId = useRef(0);

  const load = useCallback(async () => {
    const id = ++requestId.current;
    setLoading(true);
    const result = await tradeGet(`/api/trade/insights/${kind}`, t);
    if (requestId.current !== id) return;
    setLoading(false);
    if (result.error || !result.data) {
      setError(result.error || t('数据请求未完成，请刷新重试。'));
      return;
    }
    setError('');
    setView(result.data);
  }, [kind, t]);

  useEffect(() => {
    load();
    return () => {
      requestId.current += 1;
    };
  }, [load]);

  const available = view?.status === 'ready' || view?.status === 'stale';
  const items = available && Array.isArray(view.items) ? view.items : [];
  const currentPage = Math.min(
    page,
    Math.max(1, Math.ceil(items.length / PAGE_SIZE)),
  );
  const rows = items.slice(
    (currentPage - 1) * PAGE_SIZE,
    currentPage * PAGE_SIZE,
  );
  const stale = view?.status === 'stale' || (!!error && available);
  const coverage = COVERAGE[view?.coverage];
  const sourceError = view?.error
    ? t(SOURCE_ERRORS[view.error] || '数据源暂时不可用，请稍后刷新。')
    : '';
  const partialSample =
    kind === 'whales' && available && view.sample_size < view.sample_target;

  let columns = [];
  let minWidth = 760;
  if (kind === 'calendar') {
    columns = [
      {
        title: t('时间'),
        dataIndex: 'time',
        width: 180,
        render: (value) => <InsightTime value={value} t={t} />,
      },
      { title: t('国家/地区'), dataIndex: 'country', width: 100 },
      { title: t('货币'), dataIndex: 'currency', width: 80 },
      {
        title: t('经济事件'),
        dataIndex: 'title',
        width: 260,
        render: (value) => <span className='break-words'>{value}</span>,
      },
      ...[
        ['actual', '公布值'],
        ['forecast', '预期值'],
        ['previous', '前值'],
      ].map(([field, label]) => ({
        title: t(label),
        dataIndex: field,
        width: 110,
        align: 'right',
        render: (value) => <span className='trade-num'>{value || '--'}</span>,
      })),
    ];
    minWidth = 950;
  } else if (kind === 'liquidations') {
    columns = [
      { title: t('交易对'), dataIndex: 'symbol', width: 140 },
      {
        title: t('被强平方向'),
        dataIndex: 'side',
        width: 110,
        render: (value) => (
          <Tag color={value === 'long' ? 'green' : 'red'}>
            {value === 'long' ? t('多仓强平') : t('空仓强平')}
          </Tag>
        ),
      },
      {
        title: t('价格（报价币）'),
        dataIndex: 'price',
        align: 'right',
        render: (value) => (
          <span className='trade-num'>{formatPrice(value)}</span>
        ),
      },
      {
        title: t('数量'),
        dataIndex: 'quantity',
        align: 'right',
        render: (value) => (
          <span className='trade-num'>{formatQty(value)}</span>
        ),
      },
      {
        title: t('名义价值（报价币）'),
        dataIndex: 'notional',
        align: 'right',
        render: (value) => (
          <span className='trade-num'>{formatPrice(value, 2)}</span>
        ),
      },
      {
        title: t('时间'),
        dataIndex: 'time',
        width: 180,
        render: (value) => <InsightTime value={value} t={t} />,
      },
    ];
    minWidth = 900;
  } else if (kind === 'whales') {
    columns = [
      { title: t('币种'), dataIndex: 'coin', width: 90 },
      ...[
        ['long_notional', '样本多仓价值'],
        ['short_notional', '样本空仓价值'],
      ].map(([field, label]) => ({
        title: `${t(label)} (USD)`,
        dataIndex: field,
        align: 'right',
        render: (value) => (
          <span className='trade-num'>{formatPrice(value, 2)}</span>
        ),
      })),
      { title: t('多仓账户数'), dataIndex: 'long_count', align: 'right' },
      { title: t('空仓账户数'), dataIndex: 'short_count', align: 'right' },
    ];
  } else if (kind === 'companies') {
    columns = [
      { title: t('代码'), dataIndex: 'ticker', width: 90 },
      { title: t('公司/基金名称'), dataIndex: 'name', width: 260 },
      { title: t('行业'), dataIndex: 'industry', width: 200 },
      {
        title: t('资料日期'),
        dataIndex: 'as_of',
        width: 130,
        render: (value) => value || t('日期未知'),
      },
      {
        title: t('官方网站'),
        dataIndex: 'homepage',
        width: 120,
        render: (value) => (
          <InsightLink href={value}>{t('访问官网')}</InsightLink>
        ),
      },
    ];
    minWidth = 800;
  }

  return (
    <div className='trade-card flex min-w-0 flex-col gap-4 overflow-hidden'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          {view && (
            <>
              <Text size='small'>
                {t('数据来源')}：{view.source || t('来源未确认')}
              </Text>
              <Tag color={stale ? 'orange' : available ? 'green' : 'grey'}>
                {stale
                  ? t('数据已过期')
                  : available
                    ? t('数据可用')
                    : view.status === 'disabled'
                      ? t('未启用')
                      : t('暂不可用')}
              </Tag>
            </>
          )}
        </div>
        <Button
          size='small'
          theme='borderless'
          type='tertiary'
          icon={<RefreshCw size={14} />}
          loading={loading}
          onClick={load}
        >
          {t('刷新')}
        </Button>
      </div>
      {view && (
        <div className='flex flex-col gap-1'>
          <Text type='tertiary' size='small'>
            {coverage ? t(coverage) : t('数据覆盖范围未确认。')}
          </Text>
          <Text type='tertiary' size='small'>
            {t('更新时间')}：<InsightTime value={view.updated_at} t={t} />
          </Text>
          {kind === 'liquidations' && view.collection_started_at > 0 && (
            <Text type='tertiary' size='small'>
              {t('本次收集开始于')}：
              <InsightTime value={view.collection_started_at} t={t} />
            </Text>
          )}
          {kind === 'whales' && available && (
            <Text type='tertiary' size='small'>
              {t('已获取 {{count}} 个账户样本，目标最多 {{target}} 个。', {
                count: view.sample_size || 0,
                target: view.sample_target || 20,
              })}
            </Text>
          )}
          <Text type='tertiary' size='small'>
            {t('时间按本地时区显示；点击刷新获取最新可用数据。')}
          </Text>
        </div>
      )}
      {stale && (
        <Banner
          type='warning'
          closeIcon={null}
          description={t('当前显示上次成功获取的数据，最新数据暂时无法获取。')}
        />
      )}
      {available && (error || sourceError) && (
        <Banner
          type='warning'
          closeIcon={null}
          description={error || sourceError}
        />
      )}
      {partialSample && view.error !== 'partial_upstream_failure' && (
        <Banner
          type='info'
          closeIcon={null}
          description={t('可用样本少于目标数量，结果仅代表这些账户。')}
        />
      )}
      {!view && loading ? (
        <div className='flex min-h-40 items-center justify-center'>
          <Spin size='large' aria-label={t('加载中')} />
        </div>
      ) : !available ? (
        <Empty
          title={view?.status === 'disabled' ? t('未启用') : t('暂不可用')}
          description={
            error || sourceError || t('数据源暂时不可用，请稍后刷新。')
          }
        />
      ) : items.length === 0 ? (
        <Empty
          title={t('暂无数据')}
          description={
            kind === 'liquidations'
              ? t('当前尚未收到强平快照，数据源连接正常不代表期间没有强平。')
              : t('数据源本次没有返回记录。')
          }
        />
      ) : kind === 'news' ? (
        <div className='flex min-w-0 flex-col gap-5' aria-busy={loading}>
          {rows.map((item) => (
            <article key={item.id} className='min-w-0 break-words'>
              <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
                <Text type='tertiary' size='small'>
                  <InsightTime value={item.time} t={t} />
                </Text>
                <InsightLink href={item.url}>{t('阅读原文')}</InsightLink>
              </div>
              <h3 className='mb-2 text-base font-semibold'>{item.title}</h3>
              <p className='whitespace-pre-wrap break-words text-sm leading-relaxed'>
                {item.content}
              </p>
            </article>
          ))}
        </div>
      ) : (
        <Table
          size='small'
          rowKey={(item) =>
            item.id ??
            item.coin ??
            item.ticker ??
            `${item.symbol}-${item.time_ms || item.time}-${item.side}-${item.price}-${item.quantity}`
          }
          columns={columns}
          dataSource={rows}
          loading={loading}
          pagination={false}
          scroll={{ x: minWidth }}
        />
      )}
      {items.length > PAGE_SIZE && (
        <div className='flex justify-end'>
          <Pagination
            simple
            currentPage={currentPage}
            pageSize={PAGE_SIZE}
            total={items.length}
            onPageChange={setPage}
          />
        </div>
      )}
    </div>
  );
};

const InsightsView = ({ t }) => {
  const [kind, setKind] = useState('calendar');

  return (
    <div className='flex min-w-0 flex-col gap-3'>
      <div className='max-w-full overflow-x-auto pb-1'>
        <RadioGroup
          type='button'
          value={kind}
          onChange={(event) => setKind(event.target.value)}
          aria-label={t('市场资讯')}
          style={{ flexWrap: 'nowrap', whiteSpace: 'nowrap' }}
        >
          {SECTIONS.map(([value, label]) => (
            <Radio key={value} value={value}>
              {t(label)}
            </Radio>
          ))}
        </RadioGroup>
      </div>
      <InsightsPanel key={kind} kind={kind} t={t} />
    </div>
  );
};

export default InsightsView;
