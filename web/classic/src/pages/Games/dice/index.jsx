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
import { ArrowDown, ArrowUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import BetInput from '../common/BetInput';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatCoins, gamePost, percent } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { WagerStats, wagerResultMessage } from '../common/wager';
import './dice.css';

// 点数的个数：服务端掷出 0 到 SIDES - 1 之间的一个整数，与 pkg/dice 一致。
const SIDES = 10000;
// 点击后点数至少跳动这么久才停下(毫秒)；停下后标记滑到点数上，再过 SETTLE_MS 公布结果。
const ROLL_MIN_MS = 450;
const SETTLE_MS = 350;
const SCRAMBLE_MS = 50;
const HISTORY_SIZE = 10;
// 点数条下方的刻度。
const SCALE = [0, 2500, 5000, 7500, 9999];

// 获胜条件：押小掷出小于阈值、押大掷出不小于 SIDES − 阈值就赢，两个方向都正好有 threshold 个点数获胜。
function winningCondition(direction, threshold) {
  return direction === 'over' ? `≥ ${SIDES - threshold}` : `< ${threshold}`;
}

// 等待 ms 毫秒。定时器记在 page 上，页面卸载时被清掉，后面的步骤不再执行。
function delay(ms, page) {
  return new Promise((resolve) => {
    page.timer = setTimeout(resolve, ms);
  });
}

// 大号点数：等结果时快速跳动(只为好看，与结果无关)，停下后显示服务端掷出的点数，公布结果后按输赢着色。
const DiceReadout = ({ value, rolling, tone }) => {
  const [scramble, setScramble] = useState(0);

  useEffect(() => {
    if (!rolling) return undefined;
    const timer = setInterval(
      () => setScramble(Math.floor(Math.random() * SIDES)),
      SCRAMBLE_MS,
    );
    return () => clearInterval(timer);
  }, [rolling]);

  let className = 'dice-readout';
  if (rolling) className += ' is-rolling';
  else if (tone) className += ` is-${tone}`;
  else if (value === undefined) className += ' is-empty';
  return (
    <div className={className} aria-hidden>
      {rolling ? scramble : (value ?? '—')}
    </div>
  );
};

const Dice = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).dice;
  const { loading, unavailable, info, patch, reload } = useGameInfo('dice');
  const [bet, setBet] = useState(1);
  const [multiplier, setMultiplier] = useState(2);
  const [direction, setDirection] = useState('under');
  // 一次掷骰的进度：rolling 在等服务端的点数(点数跳动)，landing 点数已停下、标记正滑过去，idle 已公布结果或还没掷。
  const [phase, setPhase] = useState('idle');
  // 最近一次掷骰的结果(服务端返回)，点数与标记停在它上面。
  const [outcome, setOutcome] = useState(null);
  const [error, setError] = useState('');
  // 本次打开页面以来掷出的点数，最新的在前。
  const [history, setHistory] = useState([]);
  const historyIdRef = useRef(0);
  // 页面开着时是 { timer }，timer 是正在等待的定时器；卸载时清掉定时器并置空，进行中的掷骰不再更新页面。
  const pageRef = useRef(null);

  useEffect(() => {
    const page = { timer: 0 };
    pageRef.current = page;
    return () => {
      clearTimeout(page.timer);
      pageRef.current = null;
    };
  }, []);

  if (!info) {
    return (
      <GameLayout
        title={meta.name}
        loading={loading}
        unavailable={unavailable}
      />
    );
  }

  const symbol = info.symbol || '';
  const multipliers = info.multipliers || [];
  const choice =
    multipliers.find((item) => item.multiplier === multiplier) ||
    multipliers[0];
  const threshold = choice?.threshold || 0;
  const maxBet = Math.max(info.max_bet || 1, 1);
  // 管理员调低上限后，之前选的下注按新的上限算。
  const stake = Math.min(bet, maxBet);
  const busy = phase !== 'idle';
  const canRoll = !!choice && !busy && stake <= info.balance;

  const roll = async () => {
    const page = pageRef.current;
    if (!canRoll || !page) return;
    const startedAt = Date.now();
    const reducedMotion = window.matchMedia?.(
      '(prefers-reduced-motion: reduce)',
    ).matches;
    setPhase('rolling');
    setError('');
    const response = await gamePost(
      '/api/game/dice/roll',
      { bet: stake, multiplier: choice.multiplier, direction },
      t,
    );
    if (pageRef.current !== page) return;
    if (response.error) {
      setPhase('idle');
      setError(response.error);
      // 余额或规则可能已经变了(没有得到答复时这一注也可能已经结算)，重新读取。
      reload(true);
      return;
    }
    const data = response.data;
    if (!reducedMotion) {
      await delay(Math.max(ROLL_MIN_MS - (Date.now() - startedAt), 0), page);
    }
    setOutcome(data);
    setPhase('landing');
    if (!reducedMotion) await delay(SETTLE_MS, page);
    // 标记停稳后才公布结果、更新余额，免得还在动就剧透。
    setPhase('idle');
    historyIdRef.current += 1;
    setHistory((items) =>
      [
        { id: historyIdRef.current, roll: data.roll, won: data.won },
        ...items,
      ].slice(0, HISTORY_SIZE),
    );
    patch({
      balance: data.balance,
      net_today: data.net_today,
      plays_today: data.plays_today,
      best_win: Math.max(info.best_win || 0, data.win),
    });
  };

  const settled = phase === 'idle' && outcome;
  let tone = '';
  if (settled) tone = outcome.won ? 'win' : 'loss';

  let message = {
    kind: '',
    text: t('选好倍数和方向，掷出的点数落在绿色区间里就赢'),
  };
  if (busy) {
    message = { kind: '', text: t('掷骰中…') };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (outcome) {
    message = {
      ...wagerResultMessage(outcome, t, symbol),
      note: t('掷出 {{roll}}，需要 {{condition}}', {
        roll: outcome.roll,
        condition: winningCondition(outcome.direction, outcome.threshold),
      }),
    };
  }
  const hint = stake > info.balance ? t('游戏币余额不足') : '';

  const zoneFrom = direction === 'over' ? SIDES - threshold : 0;
  const directions = [
    { key: 'under', label: t('押小'), icon: <ArrowDown size={16} /> },
    { key: 'over', label: t('押大'), icon: <ArrowUp size={16} /> },
  ];

  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('赔付表')}>
        <table className='game-table'>
          <thead>
            <tr>
              <th>{t('倍数')}</th>
              <th>{t('押小')}</th>
              <th>{t('押大')}</th>
              <th>{t('概率')}</th>
            </tr>
          </thead>
          <tbody>
            {multipliers.map((item) => (
              <tr
                key={item.multiplier}
                className={item === choice ? 'is-active' : undefined}
              >
                <td>{`×${item.multiplier}`}</td>
                <td>{winningCondition('under', item.threshold)}</td>
                <td>{winningCondition('over', item.threshold)}</td>
                <td>{percent(item.chance)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <RuleList
          items={[
            t(
              '服务端随机掷出 0–9999 之间的一个整数，满足所选方向的条件就赢，两个方向赢的概率相同',
            ),
            t('赢了派彩 = 下注 × 倍数，已包含退回的下注；输了下注不退'),
            t(
              '胜率 = 理论返还率 ÷ 倍数，理论返还率 {{rate}}%，玩家长期是输的',
              { rate: info.rtp_percent },
            ),
            t('单次最多下注 {{max}} {{symbol}}，不限次数', {
              max: maxBet,
              symbol,
            }),
          ]}
        />
      </SideBox>
    </>
  );

  return (
    <GameLayout
      title={meta.name}
      balance={info.balance}
      symbol={symbol}
      side={side}
      className='dice-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='game-hud'>
          <span>
            {t('胜率')}
            <strong>{choice ? percent(choice.chance) : '-'}</strong>
          </span>
          <span>
            {t('赢了派彩')}
            <strong>
              {choice ? formatCoins(stake * choice.multiplier) : '-'}
            </strong>{' '}
            {symbol}
          </span>
        </div>

        <DiceReadout
          value={outcome?.roll}
          rolling={phase === 'rolling'}
          tone={tone}
        />

        <div className='dice-track'>
          <div className='dice-track-bar'>
            <div
              className='dice-track-zone'
              style={{
                left: `${(zoneFrom / SIDES) * 100}%`,
                width: `${(threshold / SIDES) * 100}%`,
              }}
            />
          </div>
          {outcome && (
            <div
              className={tone ? `dice-marker is-${tone}` : 'dice-marker'}
              style={{ left: `${((outcome.roll + 0.5) / SIDES) * 100}%` }}
              aria-hidden
            />
          )}
          <div className='dice-scale' aria-hidden>
            {SCALE.map((value) => (
              <span key={value} style={{ left: `${(value / 9999) * 100}%` }}>
                {value}
              </span>
            ))}
          </div>
        </div>

        <div className='dice-history' aria-label={t('最近掷出')}>
          {history.map((item) => (
            <span
              key={item.id}
              className={
                item.won ? 'dice-history-item is-win' : 'dice-history-item'
              }
            >
              {item.roll}
            </span>
          ))}
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='dice-options'>
          <div className='dice-option'>
            <span className='dice-option-label'>{t('倍数')}</span>
            <div className='game-chips dice-multipliers'>
              {multipliers.map((item) => (
                <button
                  key={item.multiplier}
                  type='button'
                  className={
                    item === choice ? 'game-chip is-active' : 'game-chip'
                  }
                  aria-pressed={item === choice}
                  disabled={busy}
                  onClick={() => setMultiplier(item.multiplier)}
                >
                  {`×${item.multiplier}`}
                </button>
              ))}
            </div>
          </div>
          <div className='dice-option'>
            <span className='dice-option-label'>{t('方向')}</span>
            <div className='dice-directions'>
              {directions.map((option) => (
                <button
                  key={option.key}
                  type='button'
                  className={
                    option.key === direction
                      ? 'dice-direction is-active'
                      : 'dice-direction'
                  }
                  aria-pressed={option.key === direction}
                  disabled={busy}
                  onClick={() => setDirection(option.key)}
                >
                  {option.icon}
                  <span>{option.label}</span>
                  <small>{winningCondition(option.key, threshold)}</small>
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className='game-controls'>
          <BetInput
            value={stake}
            min={info.min_bet}
            max={maxBet}
            symbol={symbol}
            disabled={busy}
            onChange={setBet}
          />
          <button
            type='button'
            className='games-play-button'
            disabled={!canRoll}
            onClick={roll}
          >
            {busy ? t('掷骰中…') : t('掷骰子')}
          </button>
          {hint && <div className='games-stage-hint'>{hint}</div>}
        </div>
      </div>
    </GameLayout>
  );
};

export default Dice;
