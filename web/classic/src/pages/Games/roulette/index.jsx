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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Trash2, Undo2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gamePost, percent } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { WagerStats, wagerResultMessage } from '../common/wager';
import RouletteTable, { SPOTS } from './RouletteTable';
import RouletteWheel from './RouletteWheel';
import './roulette.css';

// 号码的个数：0–36。
const POCKETS = 37;
// 筹码面值，超过单次下注上限的不显示。
const CHIP_VALUES = [0.01, 0.1, 0.5, 1, 5, 10, 50, 100, 500];
const HISTORY_SIZE = 12;
// 撤销最多退回这么多步。
const UNDO_LIMIT = 50;

const Roulette = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).roulette;
  const { loading, unavailable, info, patch, reload } = useGameInfo('roulette');
  const wheelRef = useRef(null);
  const aliveRef = useRef(true);
  const historyIdRef = useRef(0);
  const [chip, setChip] = useState(1);
  // 桌面上的注：键是注型-号码，值是押的游戏币。旋转之后保留，可以直接再转一次。
  const [bets, setBets] = useState({});
  // 之前的桌面，撤销时退回上一步。
  const [undoStack, setUndoStack] = useState([]);
  const [spinning, setSpinning] = useState(false);
  // 上一次旋转：服务端的结果与当时桌面上的注。桌面改动之后不再标出每一注的输赢。
  const [lastSpin, setLastSpin] = useState(null);
  const [error, setError] = useState('');
  // 下注被挡住的原因(超过上限等)，下一次改动桌面时清掉。
  const [hint, setHint] = useState('');
  // 本次打开页面以来开出的号码，最新的在前。
  const [history, setHistory] = useState([]);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  // 每个号码的颜色，轮盘、桌面与最近开出共用。
  const redNumbers = info?.red_numbers;
  const colors = useMemo(
    () =>
      Array.from({ length: POCKETS }, (_, number) => {
        if (number === 0) return 'green';
        return redNumbers?.includes(number) ? 'red' : 'black';
      }),
    [redNumbers],
  );

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
  const maxBet = Math.max(info.max_bet || 1, 1);
  const maxBets = info.max_bets || SPOTS.length;
  const payouts = info.payouts || {};
  const chips = CHIP_VALUES.filter((value) => value <= maxBet);
  const chipValue = chips.includes(chip) ? chip : chips[0];
  const count = Object.keys(bets).length;
  const totalCents = Object.values(bets).reduce(
    (sum, amount) => sum + Math.round(amount * 100),
    0,
  );
  const total = totalCents / 100;
  const canSpin =
    !spinning &&
    totalCents >= 1 &&
    total <= maxBet &&
    count <= maxBets &&
    total <= info.balance;
  const overLimit = t('一局合计最多下注 {{max}} {{symbol}}', {
    max: maxBet,
    symbol,
  });

  const changeBets = (next) => {
    setUndoStack((stack) => [...stack, bets].slice(-UNDO_LIMIT));
    setBets(next);
    setHint('');
  };

  const place = (spot) => {
    if (totalCents + Math.round(chipValue * 100) > Math.round(maxBet * 100)) {
      setHint(overLimit);
      return;
    }
    if (!bets[spot.key] && count >= maxBets) {
      setHint(t('一局最多下 {{count}} 注', { count: maxBets }));
      return;
    }
    changeBets({
      ...bets,
      [spot.key]:
        (Math.round((bets[spot.key] || 0) * 100) +
          Math.round(chipValue * 100)) /
        100,
    });
  };

  const double = () => {
    if (total * 2 > maxBet) {
      setHint(overLimit);
      return;
    }
    changeBets(
      Object.fromEntries(
        Object.entries(bets).map(([key, amount]) => [
          key,
          (Math.round(amount * 100) * 2) / 100,
        ]),
      ),
    );
  };

  const undo = () => {
    if (undoStack.length === 0) return;
    setBets(undoStack[undoStack.length - 1]);
    setUndoStack(undoStack.slice(0, -1));
    setHint('');
  };

  const spin = async () => {
    if (!canSpin) return;
    const placed = bets;
    const slip = SPOTS.filter((spot) => placed[spot.key]).map((spot) => ({
      type: spot.type,
      value: spot.value,
      amount: placed[spot.key],
    }));
    setSpinning(true);
    setError('');
    setHint('');
    setLastSpin(null);
    wheelRef.current.start();
    const response = await gamePost(
      '/api/game/roulette/spin',
      { bets: slip },
      t,
    );
    if (!aliveRef.current) return;
    if (response.error) {
      // 没有结果时小球不落进任何号码格，免得看起来开出了号码。
      wheelRef.current.stop();
      setSpinning(false);
      setError(response.error);
      // 余额或规则可能已经变了(没有得到答复时这一局也可能已经结算)，重新读取。
      reload(true);
      return;
    }
    const data = response.data;
    await wheelRef.current.land(data.number);
    if (!aliveRef.current) return;
    // 小球停稳后才公布结果、更新余额，免得还在转就剧透。
    setSpinning(false);
    setLastSpin({ data, bets: placed });
    historyIdRef.current += 1;
    setHistory((items) =>
      [{ id: historyIdRef.current, number: data.number }, ...items].slice(
        0,
        HISTORY_SIZE,
      ),
    );
    patch({
      balance: data.balance,
      net_today: data.net_today,
      plays_today: data.plays_today,
      best_win: Math.max(info.best_win || 0, data.win),
    });
  };

  const hit = lastSpin ? lastSpin.data.number : null;
  const wins =
    lastSpin && lastSpin.bets === bets
      ? Object.fromEntries(
          lastSpin.data.bets.map((bet) => [
            `${bet.type}-${bet.value}`,
            bet.win,
          ]),
        )
      : null;
  const colorNames = { red: t('红色'), black: t('黑色'), green: t('绿色') };

  let message = {
    kind: '',
    text: t('选好筹码点桌面下注，可以同时押好几处，然后旋转'),
  };
  if (spinning) {
    message = { kind: '', text: t('转动中…') };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (lastSpin) {
    message = {
      ...wagerResultMessage(lastSpin.data, t, symbol),
      note: t('开出 {{number}}（{{color}}）', {
        number: lastSpin.data.number,
        color: colorNames[lastSpin.data.color],
      }),
    };
  }
  let shownHint = hint;
  if (!shownHint && total > info.balance) shownHint = t('游戏币余额不足');

  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('赔率')}>
        <table className='game-table'>
          <thead>
            <tr>
              <th>{t('押法')}</th>
              <th>{t('赔率')}</th>
            </tr>
          </thead>
          <tbody>
            {[
              [t('单号 0–36'), payouts.straight],
              [t('红色 / 黑色'), payouts.red],
              [t('单数 / 双数'), payouts.odd],
              ['1–18 / 19–36', payouts.low],
              [t('打：1–12、13–24、25–36'), payouts.dozen],
              [t('列：与 2:1 格连成一线的 12 个号码'), payouts.column],
            ].map(([name, odds]) => (
              <tr key={name}>
                <td>{name}</td>
                <td>{`${odds}:1`}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <RuleList
          items={[
            t('赔率不含本金：押中派彩 = 下注 × (赔率 + 1)，没押中的注输掉'),
            t('开出 0 时所有外围注都输，只有押 0 的单号中'),
            t('不管怎么押，理论返还率都是 {{rate}}，玩家长期是输的', {
              rate: percent(info.payback),
            }),
            t('一次可以下多注，合计最多 {{max}} {{symbol}}，不限次数', {
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
      className='roulette-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <RouletteWheel
          ref={wheelRef}
          colors={colors}
          number={hit}
          label={meta.name}
        />

        <div className='roulette-recent' aria-label={t('最近开出')}>
          {history.map((item) => (
            <span
              key={item.id}
              className={`roulette-recent-item is-${colors[item.number]}`}
            >
              {item.number}
            </span>
          ))}
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='game-hud roulette-hud'>
          <span>
            {t('合计下注')}
            <strong>{total}</strong>
            {` / ${maxBet} ${symbol}`}
          </span>
          <span>
            {t('注数')}
            <strong>{count}</strong>
          </span>
        </div>

        <RouletteTable
          colors={colors}
          bets={bets}
          wins={wins}
          hit={hit}
          disabled={spinning}
          onPlace={place}
        />

        <div className='roulette-chips' role='group' aria-label={t('筹码')}>
          {chips.map((value) => (
            <button
              key={value}
              type='button'
              className={`roulette-chip is-chip-${value}${value === chipValue ? ' is-active' : ''}`}
              aria-pressed={value === chipValue}
              aria-label={t('筹码 {{value}}', { value })}
              onClick={() => setChip(value)}
            >
              {value}
            </button>
          ))}
        </div>

        <div className='game-controls'>
          <div className='roulette-actions'>
            <button
              type='button'
              className='roulette-action'
              disabled={spinning || undoStack.length === 0}
              onClick={undo}
            >
              <Undo2 size={14} />
              {t('撤销')}
            </button>
            <button
              type='button'
              className='roulette-action'
              disabled={spinning || count === 0}
              onClick={() => changeBets({})}
            >
              <Trash2 size={14} />
              {t('清空')}
            </button>
            <button
              type='button'
              className='roulette-action'
              disabled={spinning || count === 0}
              onClick={double}
            >
              ×2
            </button>
          </div>
          <button
            type='button'
            className='games-play-button'
            disabled={!canSpin}
            onClick={spin}
          >
            {spinning ? t('转动中…') : t('旋转')}
          </button>
          {shownHint && <div className='games-stage-hint'>{shownHint}</div>}
        </div>
      </div>
    </GameLayout>
  );
};

export default Roulette;
