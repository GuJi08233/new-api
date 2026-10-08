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

import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Typography } from '@douyinfe/semi-ui';
import { ChevronDown, Moon, Sunrise, Sunset } from 'lucide-react';
import { formatDuration } from './api';
import { sessionKinds, sessionState, sessionWeek } from './marketSession';

const { Text, Title } = Typography;

const SESSION_LABELS = {
  pre: '盘前',
  open: '盘中',
  post: '盘后',
  overnight: '夜盘',
  closed: '休市',
};

// 各时段的流动性，照 Binance 时段图的说明。
const SESSION_LIQUIDITY = {
  pre: '高流动性',
  open: '高流动性',
  post: '中等流动性',
  overnight: '中等流动性',
  closed: '低流动性',
};

const WEEKDAY_LABELS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日'];

const pad = (value) => String(value).padStart(2, '0');
const clockOf = (ts) => {
  const time = new Date(ts);
  return `${pad(time.getHours())}:${pad(time.getMinutes())}`;
};
const clockOfMinute = (minute) =>
  `${pad(Math.floor(minute / 60) % 24)}:${pad(minute % 60)}`;

const SessionIcon = ({ kind, size }) => {
  const Icon = kind === 'pre' ? Sunrise : kind === 'post' ? Sunset : Moon;
  return (
    <span
      className={`trade-session-icon trade-session-${kind}`}
      style={{ width: size, height: size }}
    >
      {kind === 'open' ? (
        <span className='trade-session-dot' />
      ) : (
        <Icon size={size} />
      )}
    </span>
  );
};

// 标的市场交易时段的徽标：美股代币、美股和大宗商品合约 7×24 小时交易，但流动性跟着标的市场走。点开是本周的时段图，按本地
// 时间画。
const SessionBadge = ({ session, t }) => {
  const [visible, setVisible] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  // 时段边界都在整分钟上，按分钟重算时间表就够了，倒计时用当前时刻。
  const minute = Math.floor(now / 60000) * 60000;
  const state = useMemo(() => sessionState(session, minute), [session, minute]);
  const week = useMemo(
    () => (visible ? sessionWeek(session, minute) : null),
    [session, minute, visible],
  );
  if (!state) return null;

  const remain = formatDuration((state.targetAt - now) / 1000, t);
  const countdown =
    state.kind === 'open'
      ? t('{{time}}后收盘', { time: remain })
      : t('{{time}}后开盘', { time: remain });
  const today = new Date(now);
  const nowPercent =
    ((today.getHours() * 60 + today.getMinutes()) / 1440) * 100;
  const todayIndex = (today.getDay() + 6) % 7;
  const offset = -today.getTimezoneOffset();
  const zone = `UTC${offset < 0 ? '-' : '+'}${Math.floor(Math.abs(offset) / 60)}${
    offset % 60 ? `:${pad(Math.abs(offset) % 60)}` : ''
  }`;
  const startDay = new Date(state.start);
  const endDay = new Date(state.end);
  const spansDays = startDay.toDateString() !== endDay.toDateString();
  const range = spansDays
    ? `${t(WEEKDAY_LABELS[(startDay.getDay() + 6) % 7])} ${clockOf(state.start)} - ${t(
        WEEKDAY_LABELS[(endDay.getDay() + 6) % 7],
      )} ${clockOf(state.end)}`
    : `${clockOf(state.start)} - ${clockOf(state.end)}`;

  return (
    <>
      <button
        type='button'
        className='trade-session-badge'
        title={t('标的市场交易时段')}
        onClick={() => setVisible(true)}
      >
        <SessionIcon kind={state.kind} size={12} />
        <span>{t(SESSION_LABELS[state.kind])}</span>
        <span className='trade-session-countdown'>· {countdown}</span>
        <ChevronDown size={12} />
      </button>
      <Modal
        visible={visible}
        title={t('标的市场交易时段')}
        onCancel={() => setVisible(false)}
        footer={null}
        width={600}
      >
        <div className='flex flex-col gap-5 pb-4'>
          <div>
            <div className='flex items-center gap-2'>
              <SessionIcon kind={state.kind} size={22} />
              <Title heading={4} className='!mb-0'>
                {t(SESSION_LABELS[state.kind])}
              </Title>
            </div>
            <div className='trade-num mt-1 font-semibold'>
              {range}, {zone}
            </div>
            <Text type='tertiary' size='small'>
              {countdown}
            </Text>
          </div>
          {week && (
            <div className='trade-session-week'>
              <div className='trade-session-axis'>
                <span
                  className='trade-session-bubble'
                  style={{ left: `${nowPercent}%` }}
                >
                  {clockOf(now)}
                </span>
              </div>
              <div className='trade-session-axis'>
                {week.ticks.map((tick) => (
                  <span
                    key={tick}
                    className='trade-session-tick trade-num'
                    style={{ left: `${(tick / 1440) * 100}%` }}
                  >
                    {clockOfMinute(tick)}
                  </span>
                ))}
              </div>
              <div className='trade-session-rows'>
                <div className='trade-session-now-track'>
                  <span
                    className='trade-session-now'
                    style={{ left: `${nowPercent}%` }}
                  />
                </div>
                {week.rows.map((row, index) => (
                  <div
                    key={row.dayStart}
                    className={`trade-session-row${index === todayIndex ? ' is-today' : ''}`}
                  >
                    <span className='trade-session-day'>
                      {t(WEEKDAY_LABELS[index])}
                    </span>
                    <div className='trade-session-track'>
                      {row.bars.map((bar) => (
                        <span
                          key={`${bar.kind}-${bar.from}`}
                          className={`trade-session-bar trade-session-${bar.kind}`}
                          style={{
                            left: `${(bar.from / 1440) * 100}%`,
                            width: `${((bar.to - bar.from) / 1440) * 100}%`,
                          }}
                        />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
          <div className='grid grid-cols-3 gap-x-2 gap-y-4'>
            {sessionKinds(session).map((kind) => (
              <div key={kind}>
                <SessionIcon kind={kind} size={18} />
                <div className='mt-1 font-semibold'>
                  {t(SESSION_LABELS[kind])}
                </div>
                <Text type='tertiary' size='small'>
                  {t(SESSION_LIQUIDITY[kind])}
                </Text>
              </div>
            ))}
          </div>
          <Text type='tertiary' size='small'>
            {t(
              '这里全天候可以交易，但流动性会随标的市场的交易时段变化，休市时价差更大、价格可能跳空。时段图按本地时间画，未计节假日。',
            )}
          </Text>
        </div>
      </Modal>
    </>
  );
};

export default SessionBadge;
