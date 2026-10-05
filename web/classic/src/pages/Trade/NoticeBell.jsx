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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Modal, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { Bell } from 'lucide-react';
import { getUserIdFromLocalStorage, timestamp2string } from '../../helpers';
import {
  formatPrice,
  formatQty,
  formatSignedUsdt,
  formatUsdt,
  tradeGet,
} from './api';

const { Text } = Typography;
// 每隔多久查一次新的通知，页面不在前台时不查。
const POLL_MS = 5_000;
// 一次最多弹几条提示，多出来的看通知列表。
const MAX_TOASTS = 3;
// 通知列表里显示最近几条。
const LIST_SIZE = 30;
// 提示停留的秒数：后台发生的事要给用户时间看清。
const TOAST_SECONDS = 8;

// 每种通知的标签颜色与提示的样式(Toast 的方法名)。
const KIND_STYLES = {
  tp: { color: 'green', toast: 'success' },
  sl: { color: 'orange', toast: 'warning' },
  liquidation: { color: 'red', toast: 'error' },
  fill: { color: 'blue', toast: 'info' },
  cover: { color: 'red', toast: 'error' },
};

// noticeText 把一条通知写成一句话：止盈止损与强平带上平掉的数量、价格和盈亏，限价单成交带上方向，扣额度带上金额。
function noticeText(notice, perUnit, t) {
  const sideLabels = {
    long: t('多仓'),
    short: t('空仓'),
    buy: t('买入'),
    sell: t('卖出'),
  };
  const actionLabels = {
    'open-long': t('开多'),
    'open-short': t('开空'),
    'close-long': t('平多'),
    'close-short': t('平空'),
  };
  const values = {
    symbol: notice.symbol,
    side: sideLabels[notice.side] || notice.side,
    qty: formatQty(notice.qty),
    price: formatPrice(notice.price),
    pnl: formatSignedUsdt(notice.pnl, perUnit),
  };
  switch (notice.kind) {
    case 'tp':
      return t(
        '{{symbol}} {{side}}止盈：平掉 {{qty}}，均价 {{price}}，盈亏 {{pnl}} USDT',
        values,
      );
    case 'sl':
      return t(
        '{{symbol}} {{side}}止损：平掉 {{qty}}，均价 {{price}}，盈亏 {{pnl}} USDT',
        values,
      );
    case 'liquidation':
      return t(
        '{{symbol}} {{side}}被强平：{{qty}} @ {{price}}，盈亏 {{pnl}} USDT',
        values,
      );
    case 'cover':
      return t('合约亏空 {{amount}} USDT 已从站内额度扣除', {
        amount: formatUsdt(notice.amount, perUnit),
      });
    default: {
      const action =
        notice.market === 'futures'
          ? actionLabels[`${notice.action}-${notice.side}`]
          : values.side;
      return notice.action === 'close'
        ? t(
            '{{symbol}} 限价{{action}}成交 {{qty}} @ {{price}}，盈亏 {{pnl}} USDT',
            {
              ...values,
              action,
            },
          )
        : t('{{symbol}} 限价{{action}}成交 {{qty}} @ {{price}}', {
            ...values,
            action,
          });
    }
  }
}

// 模拟盘的通知铃铛：止盈止损、强平、挂着的限价单成交、亏空从站内额度扣除都在后台发生，每 5 秒查一次比上次新的通知，
// 新的弹出提示(一次最多 3 条)并让页面刷新(onNotice)；点开看最近 30 条。弹过提示的与看过列表的 id 按用户记在浏览器里：
// 刷新页面不重复弹，离开期间新来的回来时弹出，没看过的条数显示在铃铛上；第一次来的用户从现在开始算。
const NoticeBell = ({ perUnit, onNotice, t }) => {
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [list, setList] = useState([]);
  const onNoticeRef = useRef(onNotice);
  onNoticeRef.current = onNotice;
  const userId = getUserIdFromLocalStorage();
  const toastedKey = `trade-notice-toasted-${userId}`;
  const readKey = `trade-notice-read-${userId}`;

  useEffect(() => {
    let alive = true;
    let busy = false;
    let toasted = -1;
    const show = (items, latestId) => {
      items.slice(0, MAX_TOASTS).forEach((notice) =>
        Toast[KIND_STYLES[notice.kind]?.toast || 'info']({
          content: noticeText(notice, perUnit, t),
          duration: TOAST_SECONDS,
        }),
      );
      if (items.length > MAX_TOASTS) {
        Toast.info({
          content: t('还有 {{count}} 条通知，点铃铛查看', {
            count: items.length - MAX_TOASTS,
          }),
          duration: TOAST_SECONDS,
        });
      }
      toasted = latestId;
      localStorage.setItem(toastedKey, String(latestId));
    };
    // 第一次查：没看过的条数从看过列表的位置算，没弹过的弹出来。
    const start = async () => {
      const stored = localStorage.getItem(toastedKey);
      const read = Number(localStorage.getItem(readKey)) || 0;
      const res = await tradeGet('/api/trade/notices', t, {
        after: stored === null ? 0 : read,
        limit: stored === null ? 1 : 50,
      });
      if (!alive || !res.data) return;
      const { items, latest_id: latestId } = res.data;
      if (stored === null) {
        localStorage.setItem(readKey, String(latestId));
        localStorage.setItem(toastedKey, String(latestId));
        toasted = latestId;
        return;
      }
      setUnread(items.length);
      const fresh = items.filter((notice) => notice.id > Number(stored));
      show(fresh, latestId);
      if (fresh.length) onNoticeRef.current?.();
    };
    const poll = async () => {
      if (busy || document.visibilityState !== 'visible') return;
      busy = true;
      if (toasted < 0) {
        await start();
        busy = false;
        return;
      }
      // 别的标签页弹过的不再弹。
      const after = Math.max(
        toasted,
        Number(localStorage.getItem(toastedKey)) || 0,
      );
      const res = await tradeGet('/api/trade/notices', t, { after, limit: 20 });
      busy = false;
      if (!alive || !res.data?.items.length) return;
      setUnread((value) => value + res.data.items.length);
      show(res.data.items, res.data.latest_id);
      onNoticeRef.current?.();
    };
    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [perUnit, readKey, t, toastedKey]);

  const openList = async () => {
    setOpen(true);
    const res = await tradeGet('/api/trade/notices', t, { limit: LIST_SIZE });
    if (!res.data) return;
    setList(res.data.items);
    localStorage.setItem(readKey, String(res.data.latest_id));
    setUnread(0);
  };

  const kindLabels = {
    tp: t('止盈'),
    sl: t('止损'),
    liquidation: t('强平'),
    fill: t('成交'),
    cover: t('扣额度'),
  };

  return (
    <>
      {/* 没看过的条数写在铃铛旁边：页头紧贴着顶栏，角标会被顶栏挡住。 */}
      <Button
        theme='borderless'
        type={unread > 0 ? 'primary' : 'tertiary'}
        icon={<Bell size={16} />}
        aria-label={t('模拟盘通知')}
        onClick={openList}
      >
        {unread > 0 ? (unread > 99 ? '99+' : unread) : null}
      </Button>
      <Modal
        title={t('模拟盘通知')}
        visible={open}
        footer={null}
        onCancel={() => setOpen(false)}
        width={640}
      >
        <div className='flex max-h-[60vh] flex-col gap-2 overflow-y-auto pb-4'>
          {list.length === 0 ? (
            <Text type='tertiary'>{t('还没有通知')}</Text>
          ) : (
            list.map((notice) => (
              <div key={notice.id} className='flex items-start gap-2'>
                <Tag
                  size='small'
                  color={KIND_STYLES[notice.kind]?.color || 'grey'}
                  className='shrink-0'
                >
                  {kindLabels[notice.kind] || notice.kind}
                </Tag>
                <div className='flex min-w-0 flex-col'>
                  <Text size='small' className='trade-num'>
                    {noticeText(notice, perUnit, t)}
                  </Text>
                  <Text type='tertiary' size='small'>
                    {timestamp2string(notice.created_at)}
                  </Text>
                </div>
              </div>
            ))
          )}
        </div>
      </Modal>
    </>
  );
};

export default NoticeBell;
