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
import { Button, InputNumber, Select, Tag } from '@douyinfe/semi-ui';
import { Gift, History, Package, Store } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RuleList } from '../common/SideBox';
import { gameGet } from '../common/api';
import { playSound } from '../common/sound';
import { formatAmount, formatLeft, goodsName, useSectText } from './parts';

// 宗门里这种货物现在有多少。
function stockOf(sect, goods) {
  if (goods === 'herb' || goods === 'ore') return sect.res?.[goods] || 0;
  if (goods.startsWith('pill_')) return sect.pills?.[goods.slice(5)] || 0;
  if (goods.startsWith('rare_')) return sect.rare?.[goods.slice(5)] || 0;
  return 0;
}

// 一单：货物与数量、单价与总价、谁挂的、还剩多久；action 是右边的按钮。
const ListingRow = ({ row, view, children, note }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const unit = row.price / row.count;
  return (
    <div className='sc-listing'>
      <strong>
        {goodsName(text, row.goods)} ×{formatAmount(row.count)}
      </strong>
      <span className='sc-chip is-stone'>
        {row.price > 0
          ? t('{{price}} 灵石', { price: formatAmount(row.price) })
          : t('赠送')}
      </span>
      {row.price > 0 && row.count > 1 && (
        <span className='sc-muted'>
          {t('单价 {{n}}', {
            n: unit >= 10 ? Math.round(unit) : unit.toFixed(2),
          })}
        </span>
      )}
      {note ?? (
        <span className='sc-muted'>
          {view.names[row.seller_id] || `#${row.seller_id}`}
        </span>
      )}
      <span className='sc-spacer' />
      {children}
    </div>
  );
};

// 挂单表单：选货物、数量、总价，可以指名卖给一位道友(指名时可以 0 灵石赠送)。
const ListingForm = ({ view, sect, busy, act, friends }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const rules = view.rules;
  const [goods, setGoods] = useState(rules.goods[0].key);
  const [count, setCount] = useState(rules.goods[0].min);
  const [price, setPrice] = useState(100);
  const [target, setTarget] = useState(0);
  const good = rules.goods.find((g) => g.key === goods) || rules.goods[0];
  const have = stockOf(sect, goods);
  const tax = Math.floor((price * rules.tax_pct) / 100);
  const valid =
    count >= good.min &&
    count <= Math.min(good.max, have) &&
    price >= 0 &&
    price <= rules.max_price &&
    (price > 0 || target > 0);
  return (
    <div className='sc-recipe sc-market-form'>
      <div className='sc-building-foot'>
        <Select
          size='small'
          value={goods}
          onChange={(value) => {
            const next = rules.goods.find((g) => g.key === value);
            setGoods(value);
            setCount(next.min);
          }}
          optionList={rules.goods.map((g) => ({
            value: g.key,
            label: `${goodsName(text, g.key)}（${formatAmount(stockOf(sect, g.key))}）`,
          }))}
          style={{ width: 180 }}
        />
        <span className='sc-muted'>{t('数量')}</span>
        <InputNumber
          size='small'
          min={good.min}
          max={good.max}
          value={count}
          onChange={(value) => setCount(Math.floor(Number(value) || 0))}
          style={{ width: 110 }}
        />
        <span className='sc-muted'>{t('总价')}</span>
        <InputNumber
          size='small'
          min={0}
          max={rules.max_price}
          value={price}
          onChange={(value) => setPrice(Math.floor(Number(value) || 0))}
          style={{ width: 120 }}
        />
      </div>
      <div className='sc-building-foot'>
        <span className='sc-muted'>{t('卖给')}</span>
        <Select
          size='small'
          value={target}
          onChange={setTarget}
          optionList={[
            { value: 0, label: t('所有掌门') },
            ...friends.map((f) => ({ value: f.user_id, label: f.name })),
          ]}
          style={{ width: 180 }}
        />
        <span className='sc-muted'>
          {price > 0
            ? t('成交后得灵石 {{n}}（抽成 {{tax}}）', {
                n: formatAmount(price - tax),
                tax: formatAmount(tax),
              })
            : target > 0
              ? t('0 灵石就是赠送')
              : t('卖给所有掌门时要标价')}
        </span>
        <span className='sc-spacer' />
        <Button
          size='small'
          theme='solid'
          disabled={busy || !valid}
          onClick={() => act('list', { goods, count, price, target })}
        >
          {target > 0 && price === 0 ? t('赠送') : t('挂出')}
        </Button>
      </div>
      {count > have && (
        <span className='sc-hint'>
          {t('库存只有 {{n}}', { n: formatAmount(have) })}
        </span>
      )}
    </div>
  );
};

// 万宝阁页。post 是宗门页的请求(共用忙碌状态与错误提示)，onSect 收下操作之后的最新宗门。
const Market = ({ sect, busy, post, onSect }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const [view, setView] = useState(null);
  const [goods, setGoods] = useState('');
  const [page, setPage] = useState(0);
  const [friends, setFriends] = useState([]);
  const hall = sect.buildings.find((b) => b.key === 'hall')?.level || 0;

  const load = useCallback(async () => {
    const query = new URLSearchParams({ page: String(page) });
    if (goods) query.set('goods', goods);
    const response = await gameGet(`/api/game/wendao/sect/market?${query}`, t);
    if (response.data) setView(response.data);
  }, [goods, page, t]);
  useEffect(() => {
    load();
  }, [load]);
  useEffect(() => {
    gameGet('/api/game/wendao/sect/garden/neighbors', t).then((response) =>
      setFriends(response.data?.neighbors || []),
    );
  }, [t]);

  const act = async (action, body) => {
    const data = await post(`/api/game/wendao/sect/market/${action}`, body);
    if (!data) {
      load();
      return;
    }
    playSound(action === 'buy' ? 'coin' : 'click');
    setView({ ...data, page: 0 });
    setPage(0);
    if (data.sect) onSect(data.sect);
  };

  if (!view) return null;
  const rules = view.rules;
  const now = view.now;
  const canTrade = hall >= rules.min_hall;
  const stone = sect.res?.stone || 0;
  return (
    <div className='sc-market'>
      <RuleList
        items={[
          t(
            '掌门之间用灵石买卖灵草、玄铁、丹药与灵药；挂单时货物押在阁里，挂 {{days}} 天没卖出就过期，可以取回',
            { days: Math.round(rules.ttl / 86400) },
          ),
          t(
            '成交时万宝阁抽走 {{pct}}% 灵石；可以指名卖给一位道友，指名时标 0 灵石就是赠送',
            { pct: rules.tax_pct },
          ),
          t('主殿 {{n}} 级起才能买卖；万宝阁只流通宗门的东西，不涉及游戏币', {
            n: rules.min_hall,
          }),
        ]}
      />
      {view.offers.length > 0 && (
        <>
          <h4 className='sc-heading'>
            <Gift size={16} />
            {t('道友指名给你')}
          </h4>
          <div className='sc-listings'>
            {view.offers.map((row) => (
              <ListingRow key={row.id} row={row} view={view}>
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy || !canTrade || stone < row.price}
                  onClick={() => act('buy', { id: row.id })}
                >
                  {row.price > 0 ? t('买下') : t('收下')}
                </Button>
              </ListingRow>
            ))}
          </div>
        </>
      )}
      <h4 className='sc-heading'>
        <Store size={16} />
        {t('在售')}
        <span className='sc-spacer' />
        <Select
          size='small'
          value={goods}
          onChange={(value) => {
            setGoods(value);
            setPage(0);
          }}
          optionList={[
            { value: '', label: t('全部货物') },
            ...rules.goods.map((g) => ({
              value: g.key,
              label: goodsName(text, g.key),
            })),
          ]}
          style={{ width: 150 }}
        />
      </h4>
      {view.listings.length === 0 ? (
        <p className='sc-muted'>{t('现在没有人在卖')}</p>
      ) : (
        <div className='sc-listings'>
          {view.listings.map((row) => (
            <ListingRow key={row.id} row={row} view={view}>
              <span className='sc-muted'>
                {formatLeft(row.expires_at - now, t)}
              </span>
              <Button
                size='small'
                theme='solid'
                disabled={busy || !canTrade || stone < row.price}
                onClick={() => act('buy', { id: row.id })}
              >
                {t('买下')}
              </Button>
            </ListingRow>
          ))}
        </div>
      )}
      {(view.page > 0 || view.more) && (
        <div className='sc-building-foot sc-market-pager'>
          <Button
            size='small'
            disabled={view.page === 0}
            onClick={() => setPage(view.page - 1)}
          >
            {t('上一页')}
          </Button>
          <span className='sc-muted'>
            {t('第 {{n}} 页', { n: view.page + 1 })}
          </span>
          <Button
            size='small'
            disabled={!view.more}
            onClick={() => setPage(view.page + 1)}
          >
            {t('下一页')}
          </Button>
        </div>
      )}
      <h4 className='sc-heading'>
        <Package size={16} />
        {t('我的挂单 {{n}} / {{max}}', {
          n: view.mine.length,
          max: rules.max_listings,
        })}
      </h4>
      {canTrade ? (
        <ListingForm
          view={view}
          sect={sect}
          busy={busy}
          act={act}
          friends={friends}
        />
      ) : (
        <p className='sc-hint'>
          {t('主殿升到 {{n}} 级才能在万宝阁买卖', { n: rules.min_hall })}
        </p>
      )}
      {view.mine.length > 0 && (
        <div className='sc-listings'>
          {view.mine.map((row) => {
            const expired = row.expires_at <= now;
            return (
              <ListingRow
                key={row.id}
                row={row}
                view={view}
                note={
                  <>
                    {row.target_id > 0 && (
                      <Tag size='small' color='green'>
                        {t('指名：{{name}}', {
                          name: view.names[row.target_id] || row.target_id,
                        })}
                      </Tag>
                    )}
                    <span className={expired ? 'sc-hint' : 'sc-muted'}>
                      {expired
                        ? t('已过期')
                        : t('{{time}}后过期', {
                            time: formatLeft(row.expires_at - now, t),
                          })}
                    </span>
                  </>
                }
              >
                <Button
                  size='small'
                  disabled={busy}
                  onClick={() => act('cancel', { id: row.id })}
                >
                  {expired ? t('取回') : t('撤下')}
                </Button>
              </ListingRow>
            );
          })}
        </div>
      )}
      {view.trades.length > 0 && (
        <>
          <h4 className='sc-heading'>
            <History size={16} />
            {t('成交记录')}
          </h4>
          <ul className='sc-log'>
            {view.trades.map((row) => {
              const sold = row.seller_id === view.self;
              const name = goodsName(text, row.goods);
              const gift = row.price === 0;
              return (
                <li key={row.id}>
                  {gift
                    ? sold
                      ? t('赠给{{who}}：{{goods}} ×{{n}}', {
                          who: view.names[row.buyer_id] || row.buyer_id,
                          goods: name,
                          n: formatAmount(row.count),
                        })
                      : t('收下{{who}}赠送的{{goods}} ×{{n}}', {
                          who: view.names[row.seller_id] || row.seller_id,
                          goods: name,
                          n: formatAmount(row.count),
                        })
                    : sold
                      ? t('卖给{{who}}：{{goods}} ×{{n}}，得灵石 {{stone}}', {
                          who: view.names[row.buyer_id] || row.buyer_id,
                          goods: name,
                          n: formatAmount(row.count),
                          stone: formatAmount(row.price - row.tax),
                        })
                      : t('从{{who}}买到：{{goods}} ×{{n}}，花灵石 {{stone}}', {
                          who: view.names[row.seller_id] || row.seller_id,
                          goods: name,
                          n: formatAmount(row.count),
                          stone: formatAmount(row.price),
                        })}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
};

export default Market;
