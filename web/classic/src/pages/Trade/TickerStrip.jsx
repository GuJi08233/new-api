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
import { changePercent, decimalsOf, formatPrice, trendClass } from './api';

// 每个品种滚过去用的秒数，滚完一圈的时间随品种数变长，速度不变。
const SECONDS_PER_ITEM = 7;

// 顶部行情条：当前市场(现货或合约)每个品种的最新价与 24 小时涨跌，点一格进入交易。行情来自 feed 上的 ticker 事件——页面上
// 已有的行情推送把每条 24 小时行情转过来，行情条自己保存，只重绘行情条。一排放不下时两份相同的内容首尾相接向左滚，悬停或
// 键盘聚焦时停下。
const TickerStrip = ({ items, feed, current, onOpen, t }) => {
  const [live, setLive] = useState({});
  const [scrolling, setScrolling] = useState(false);
  const viewportRef = useRef(null);
  const setRef = useRef(null);

  useEffect(() => {
    const onTicker = (event) => {
      const data = event.detail;
      setLive((previous) => ({
        ...previous,
        [data.s]: { price: data.c, open: data.o },
      }));
    };
    feed.addEventListener('ticker', onTicker);
    return () => feed.removeEventListener('ticker', onTicker);
  }, [feed]);

  // 一份内容比行情条宽时才滚动。
  useEffect(() => {
    const observer = new ResizeObserver(() =>
      setScrolling(
        setRef.current.scrollWidth > viewportRef.current.clientWidth,
      ),
    );
    observer.observe(viewportRef.current);
    observer.observe(setRef.current);
    return () => observer.disconnect();
  }, []);

  const cells = (copy) =>
    items.map((item) => {
      const quote = live[item.symbol] || item.quote || {};
      const change = changePercent(quote.price, quote.open);
      return (
        <button
          key={item.symbol}
          type='button'
          tabIndex={copy ? -1 : undefined}
          className={`trade-ticker-cell${item.symbol === current ? ' trade-ticker-current' : ''}`}
          onClick={() => onOpen(item.symbol)}
        >
          <b>{item.ticker}</b>
          <span>
            {formatPrice(quote.price, decimalsOf(item.rules?.tick_size))}
          </span>
          <span className={trendClass(change)}>
            {change === null
              ? '--'
              : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
          </span>
        </button>
      );
    });

  return (
    <div
      ref={viewportRef}
      role='region'
      aria-label={t('行情')}
      className={`trade-ticker${scrolling ? ' trade-ticker-scrolling' : ''}`}
    >
      <div
        className='trade-ticker-track'
        style={{ animationDuration: `${items.length * SECONDS_PER_ITEM}s` }}
      >
        <div ref={setRef} className='trade-ticker-set'>
          {cells(false)}
        </div>
        {scrolling && (
          <div className='trade-ticker-set' aria-hidden='true'>
            {cells(true)}
          </div>
        )}
      </div>
    </div>
  );
};

export default TickerStrip;
