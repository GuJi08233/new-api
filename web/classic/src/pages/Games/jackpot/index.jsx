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
import { InputNumber } from '@douyinfe/semi-ui';
import { Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { timestamp2string } from '../../../helpers';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, percent, signed } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import './jackpot.css';

// 页面每隔这么久刷新一次奖池；开奖由服务端在刷新时完成。
const POLL_MS = 2000;
const RING_RADIUS = 80;
const RING_WIDTH = 22;
// 玩家的颜色依次取用，自己永远是主色。
const PLAYER_COLORS = [
  '#0ea5e9',
  '#f59e0b',
  '#10b981',
  '#ec4899',
  '#8b5cf6',
  '#ef4444',
  '#14b8a6',
  '#6366f1',
];

// 从正上方起顺时针转过 degrees 度、离圆心 radius 处的坐标。
function polar(radius, degrees) {
  const radians = (degrees * Math.PI) / 180;
  return [radius * Math.sin(radians), -radius * Math.cos(radians)];
}

// 圆环上从 from 度到 to 度的一段。
function arcPath(from, to) {
  const outer = RING_RADIUS + RING_WIDTH / 2;
  const inner = RING_RADIUS - RING_WIDTH / 2;
  const large = to - from > 180 ? 1 : 0;
  const [x1, y1] = polar(outer, from);
  const [x2, y2] = polar(outer, to);
  const [x3, y3] = polar(inner, to);
  const [x4, y4] = polar(inner, from);
  return `M${x1} ${y1}A${outer} ${outer} 0 ${large} 1 ${x2} ${y2}L${x3} ${y3}A${inner} ${inner} 0 ${large} 0 ${x4} ${y4}Z`;
}

// 奖池里每位玩家占的一段，按投入先后从正上方顺时针排开，占的角度就是中奖概率。
function playerArcs(players, pot) {
  let start = 0;
  let colorIndex = 0;
  return players.map((player) => {
    const sweep = pot > 0 ? (player.amount / pot) * 360 : 0;
    const arc = {
      ...player,
      from: start,
      to: start + sweep,
      color: player.own
        ? 'var(--semi-color-primary)'
        : PLAYER_COLORS[colorIndex++ % PLAYER_COLORS.length],
    };
    start += sweep;
    return arc;
  });
}

const Jackpot = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, setInfo } = useGameInfo('jackpot');
  const [amount, setAmount] = useState(1);
  const [entering, setEntering] = useState(false);
  const [message, setMessage] = useState(null);
  // 上一次看到的一轮：它开奖之后在最近开奖里找到它，公布赢家。
  const lastRoundRef = useRef(null);
  const [drawn, setDrawn] = useState(null);
  // 本地时钟与服务器时钟之差，倒计时按服务器时间算。
  const offsetRef = useRef(0);
  const [now, setNow] = useState(() => Date.now() / 1000);

  const apply = useCallback(
    (data) => {
      offsetRef.current = data.server_time - Date.now() / 1000;
      const last = lastRoundRef.current;
      if (last && data.round?.id !== last.id) {
        const result = (data.recent || []).find((item) => item.id === last.id);
        if (result) setDrawn({ ...result, players: last.players });
      }
      lastRoundRef.current = data.round?.pot > 0 ? data.round : null;
      setInfo(data);
    },
    [setInfo],
  );

  // 拿到信息之后定时刷新，开奖、别人的投入都靠它看到。
  const loaded = !!info;
  useEffect(() => {
    if (!loaded) return undefined;
    const timer = setInterval(async () => {
      const response = await gameGet('/api/game/jackpot', t);
      if (response.data) apply(response.data);
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [loaded, apply, t]);

  useEffect(() => {
    if (info && !lastRoundRef.current && info.round?.pot > 0) {
      lastRoundRef.current = info.round;
      offsetRef.current = info.server_time - Date.now() / 1000;
    }
  }, [info]);

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now() / 1000), 250);
    return () => clearInterval(timer);
  }, []);

  const symbol = info?.symbol || '';
  const rule = info?.rule || {};
  const round = info?.round;
  const pot = round?.pot || 0;
  const players = round?.players || [];
  const mine = round?.mine || 0;
  const room = Math.max((rule.max_bet || 0) - mine, 0);
  const serverNow = now + offsetRef.current;
  const secondsLeft = round?.deadline
    ? Math.max(Math.ceil(round.deadline - serverNow), 0)
    : null;
  const closing = secondsLeft === 0;

  const enter = async () => {
    if (entering) return;
    setEntering(true);
    setMessage(null);
    const response = await gamePost('/api/game/jackpot/enter', { amount }, t);
    setEntering(false);
    if (response.error) {
      setMessage({ kind: 'error', text: response.error });
      return;
    }
    apply(response.data);
    setDrawn(null);
    setMessage({
      kind: 'win',
      text: t('已投入 {{amount}} {{symbol}}', { amount, symbol }),
    });
  };

  const arcs = playerArcs(players, pot);
  const drawnArcs = drawn ? playerArcs(drawn.players || [], drawn.pot) : [];
  // 开奖后指针停在抽中的那张奖券上。
  const pointer =
    drawn && drawn.pot > 0 ? ((drawn.ticket + 0.5) / drawn.pot) * 360 : null;
  const shownArcs = drawn ? drawnArcs : arcs;

  let status = t('等待第一位玩家投入');
  if (drawn) {
    status = drawn.own
      ? t('你赢得了 {{payout}} {{symbol}}！', { payout: drawn.payout, symbol })
      : t('{{winner}} 赢得了 {{payout}} {{symbol}}', {
          winner: drawn.winner,
          payout: drawn.payout,
          symbol,
        });
  } else if (closing) {
    status = t('开奖中…');
  } else if (secondsLeft !== null) {
    status = t('{{seconds}} 秒后开奖', { seconds: secondsLeft });
  } else if (players.length === 1) {
    status = t('等待第二位玩家投入，之后开始 {{seconds}} 秒倒计时', {
      seconds: rule.round_seconds,
    });
  }

  const side = info && (
    <>
      <SideBox
        label={t('今日输赢')}
        value={signed(info.net_today)}
        unit={symbol}
        note={t('今天参与了 {{plays}} 轮', { plays: info.plays_today || 0 })}
      />
      <SideBox label={t('本轮玩家')}>
        {players.length === 0 ? (
          <div className='game-box-note'>{t('还没有人投入')}</div>
        ) : (
          <table className='game-table'>
            <thead>
              <tr>
                <th>{t('玩家')}</th>
                <th>{t('投入')}</th>
                <th>{t('概率')}</th>
              </tr>
            </thead>
            <tbody>
              {arcs.map((player, index) => (
                <tr key={index} className={player.own ? 'is-active' : ''}>
                  <td>
                    <span
                      className='jackpot-swatch'
                      style={{ background: player.color }}
                    />
                    {player.own ? t('我') : player.name}
                  </td>
                  <td>{player.amount}</td>
                  <td>{percent(player.amount / pot)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </SideBox>
      <SideBox label={t('最近开奖')}>
        {(info.recent || []).length === 0 ? (
          <div className='game-box-note'>{t('还没有开过奖')}</div>
        ) : (
          <ul className='jackpot-recent'>
            {info.recent.map((item) => (
              <li key={item.id} className={item.own ? 'is-own' : ''}>
                <span className='jackpot-recent-winner'>
                  <Trophy size={12} />
                  {item.own ? t('我') : item.winner}
                </span>
                <span>
                  {t('奖池 {{pot}}，概率 {{chance}}', {
                    pot: item.pot,
                    chance: percent(item.winner_stake / item.pot, 1),
                  })}
                </span>
                <span className='jackpot-recent-time'>
                  {timestamp2string(item.drawn_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </SideBox>
      <SideBox label={t('规则')}>
        <RuleList
          items={[
            t(
              '每投 1 个游戏币就是一张奖券，开奖时从所有奖券里随机抽一张，投得越多中奖概率越高',
            ),
            t(
              '第二位玩家投入后开始 {{seconds}} 秒倒计时，倒计时期间还能继续投',
              {
                seconds: rule.round_seconds,
              },
            ),
            t('赢家拿走奖池，平台抽取 {{rake}}%（向下取整）', {
              rake: round?.rake_percent ?? rule.rake_percent,
            }),
            t('每人每轮最多投入 {{max}} {{symbol}}', {
              max: rule.max_bet,
              symbol,
            }),
            t('一直只有一位玩家的一轮，{{minutes}} 分钟后原样退回', {
              minutes: Math.round((rule.stale_seconds || 600) / 60),
            }),
          ]}
        />
      </SideBox>
    </>
  );

  return (
    <GameLayout
      title={t('夺宝奖池')}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
    >
      <div className={drawn?.own ? 'games-stage is-big-win' : 'games-stage'}>
        <div className='jackpot-ring'>
          <svg viewBox='-120 -120 240 240' aria-hidden>
            <circle r={RING_RADIUS} className='jackpot-ring-track' />
            {shownArcs.length === 1 ? (
              <circle
                r={RING_RADIUS}
                className='jackpot-ring-full'
                style={{ stroke: shownArcs[0].color }}
              />
            ) : (
              shownArcs.map((arc, index) => (
                <path
                  key={index}
                  d={arcPath(arc.from, arc.to)}
                  style={{ fill: arc.color }}
                  className={
                    drawn && arc.name === drawn.winner
                      ? 'jackpot-arc is-winner'
                      : 'jackpot-arc'
                  }
                />
              ))
            )}
            {pointer !== null && (
              <g
                className='jackpot-pointer'
                style={{ transform: `rotate(${pointer}deg)` }}
              >
                <path
                  d={`M0 ${-RING_RADIUS + 16}L-7 ${-RING_RADIUS + 34}L7 ${-RING_RADIUS + 34}Z`}
                />
              </g>
            )}
          </svg>
          <div className='jackpot-ring-center'>
            <span className='jackpot-pot'>{drawn ? drawn.pot : pot}</span>
            <span className='jackpot-pot-unit'>{symbol}</span>
            <span className='jackpot-pot-label'>
              {drawn ? t('上一轮奖池') : t('当前奖池')}
            </span>
          </div>
        </div>

        <StageMessage kind={drawn?.own ? 'big-win' : ''}>{status}</StageMessage>
        {message && (
          <StageMessage kind={message.kind}>{message.text}</StageMessage>
        )}

        <div className='game-controls'>
          <div className='game-chips'>
            {[1, 5, 10, 50, 100]
              .filter((value) => value <= rule.max_bet)
              .map((value) => (
                <button
                  key={value}
                  type='button'
                  className={
                    value === amount ? 'game-chip is-active' : 'game-chip'
                  }
                  onClick={() => setAmount(value)}
                >
                  {value}
                </button>
              ))}
          </div>
          <InputNumber
            value={amount}
            min={1}
            max={Math.max(room, 1)}
            precision={0}
            suffix={symbol}
            style={{ width: 150 }}
            onChange={(value) =>
              setAmount(typeof value === 'number' ? value : 1)
            }
          />
          <button
            type='button'
            className='games-play-button'
            disabled={
              entering ||
              closing ||
              amount < 1 ||
              amount > room ||
              amount > (info?.balance || 0)
            }
            onClick={enter}
          >
            {entering ? t('投入中…') : t('投入奖池')}
          </button>
          <div className='games-stage-counter'>
            {t('本轮已投入 {{mine}} / {{max}} {{symbol}}', {
              mine,
              max: rule.max_bet,
              symbol,
            })}
            {mine > 0 &&
              pot > 0 &&
              ` · ${t('中奖概率 {{chance}}', { chance: percent(mine / pot) })}`}
          </div>
        </div>
      </div>
    </GameLayout>
  );
};

export default Jackpot;
