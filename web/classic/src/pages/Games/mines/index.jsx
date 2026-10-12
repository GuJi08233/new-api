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

import React, { useEffect, useState } from 'react';
import { InputNumber } from '@douyinfe/semi-ui';
import { Bomb, Gem } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import BetInput from '../common/BetInput';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { WagerStats, wagerResultMessage } from '../common/wager';
import { useGameInfo } from '../common/useGameInfo';
import { useWagerRound } from '../common/useWagerRound';
import './mines.css';

const CELLS = 25;
const MIN_MINES = 1;
const MAX_MINES = 24;
// 雷数的几个常用选项，其余的在输入框里填。
const MINE_PRESETS = [1, 3, 5, 10, 24];

// 一局结束后的提示：踩雷输掉下注；兑现时说明是手动兑现还是挖完、到顶自动兑现。
function minesResultMessage(round, maxMultiplier, t, symbol) {
  if (round.exploded !== undefined) {
    return {
      kind: 'loss',
      text: t('踩到地雷，输掉 {{bet}} {{symbol}}', { bet: round.bet, symbol }),
    };
  }
  const message = wagerResultMessage(
    { bet: round.bet, win: round.win },
    t,
    symbol,
  );
  let note = t('挖出 {{count}} 颗宝石后兑现', {
    count: round.revealed.length,
  });
  if (round.revealed.length === CELLS - round.mines) {
    note = t('宝石全部挖出，自动兑现');
  } else if (round.win >= round.bet * maxMultiplier) {
    note = t('到达 {{max}} 倍上限，自动兑现', { max: maxMultiplier });
  }
  return { ...message, note };
}

const Mines = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, setInfo, reload } = useGameInfo('mines');
  const { round, busy, error, refresh, send } = useWagerRound(
    'mines',
    setInfo,
    reload,
  );
  const [bet, setBet] = useState(1);
  const [mineCount, setMineCount] = useState(3);

  // 拿到游戏信息后读出没结束的一局，刷新页面、游戏关闭之后都能接着挖。
  const loaded = !!info;
  useEffect(() => {
    if (loaded) refresh();
  }, [loaded, refresh]);

  const meta = getGameMetaMap(t).mines;
  // 游戏关闭后只有还没结束一局的玩家能进来，玩完这一局就不能再开新局。
  const closed = info?.enabled === false;
  const symbol = info?.symbol || '';
  const balance = info?.balance || 0;
  const maxBet = Math.max(info?.max_bet || 1, 1);
  const maxMultiplier = info?.max_multiplier || 1000;
  const stake = Math.min(bet, maxBet);
  const playing = round?.status === 'playing';
  const finished = round?.status === 'finished';
  const revealed = new Set(round?.revealed || []);
  const mineCells = new Set(round?.mine_cells || []);
  const mines = round?.mines ?? mineCount;
  // 翻开第一格的倍数只用于开局前的公示，派彩以服务端为准。
  const firstMultiplier =
    ((info?.rtp_percent || 0) / 100) * (CELLS / (CELLS - mineCount));

  let message = {
    kind: '',
    text: t('选好下注和雷数，点击开始挖宝'),
  };
  if (playing) {
    message = {
      kind: '',
      text:
        revealed.size === 0
          ? t('点开一格试试运气，挖到宝石就能兑现')
          : t('已挖出 {{count}} 颗宝石，继续挖或随时兑现', {
              count: revealed.size,
            }),
    };
  } else if (finished) {
    message = minesResultMessage(round, maxMultiplier, t, symbol);
  }

  const start = () => send('start', { bet: stake, mines: mineCount }, false);

  const side = info && (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('规则')}>
        <RuleList
          items={[
            t('5×5 共 25 格，开局前选 1 到 24 个雷，雷越多每格的倍数越高'),
            t(
              '挖出 k 颗宝石后兑现的倍数 = 返还率 × C(25, k) ÷ C(25 − 雷数, k)，派彩可以有小数',
            ),
            t(
              '踩到地雷输掉下注；挖出全部宝石或派彩到下注的 {{max}} 倍时自动兑现',
              { max: maxMultiplier },
            ),
            t('理论返还率 {{rate}}%', { rate: info.rtp_percent }),
            t('单次最多下注 {{max}} {{symbol}}', { max: maxBet, symbol }),
            t('押着游戏币的一局不会丢，离开页面后回来还能接着玩'),
          ]}
        />
      </SideBox>
    </>
  );

  return (
    <GameLayout
      title={meta.name}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
      className='mines-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='game-hud'>
          <span>
            {t('雷数')}
            <strong>{mines}</strong>
          </span>
          <span>
            {t('宝石')}
            <strong>
              {revealed.size} / {CELLS - mines}
            </strong>
          </span>
        </div>

        <div className={playing ? 'mines-board is-playing' : 'mines-board'}>
          {Array.from({ length: CELLS }, (_, index) => {
            let state = 'hidden';
            if (revealed.has(index)) state = 'gem';
            else if (finished && index === round.exploded) state = 'exploded';
            else if (finished && mineCells.has(index)) state = 'mine';
            else if (finished) state = 'spare';
            return (
              <button
                key={index}
                type='button'
                className={`mines-cell is-${state}`}
                disabled={!playing || state !== 'hidden' || busy}
                aria-label={t('第 {{index}} 格', { index: index + 1 })}
                onClick={() => send('pick', { index })}
              >
                {(state === 'gem' || state === 'spare') && <Gem />}
                {(state === 'mine' || state === 'exploded') && <Bomb />}
              </button>
            );
          })}
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing ? (
          <div className='game-controls'>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || revealed.size === 0}
              onClick={() => send('cashout')}
            >
              {revealed.size === 0
                ? t('至少挖开一格才能兑现')
                : t('兑现 {{amount}} {{symbol}}', {
                    amount: round.cashout,
                    symbol,
                  })}
            </button>
            <div className='games-stage-counter'>
              {t('再挖一颗宝石可兑现 {{amount}} {{symbol}}', {
                amount: round.next_cashout,
                symbol,
              })}
            </div>
          </div>
        ) : (
          <div className='game-controls'>
            <BetInput
              value={stake}
              max={maxBet}
              symbol={symbol}
              disabled={busy}
              onChange={setBet}
            />
            <div className='mines-count'>
              <span className='mines-count-label'>{t('雷数')}</span>
              <div className='game-chips'>
                {MINE_PRESETS.map((value) => (
                  <button
                    key={value}
                    type='button'
                    className={
                      value === mineCount ? 'game-chip is-active' : 'game-chip'
                    }
                    disabled={busy}
                    onClick={() => setMineCount(value)}
                  >
                    {value}
                  </button>
                ))}
              </div>
              <InputNumber
                value={mineCount}
                min={MIN_MINES}
                max={MAX_MINES}
                precision={0}
                disabled={busy}
                style={{ width: 96 }}
                onChange={(value) => {
                  if (typeof value !== 'number') return;
                  setMineCount(
                    Math.min(Math.max(Math.round(value), MIN_MINES), MAX_MINES),
                  );
                }}
              />
            </div>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || closed || stake > balance}
              onClick={start}
            >
              {finished ? t('再来一局') : t('开始挖宝')}
            </button>
            <div className='games-stage-counter'>
              {t('{{mines}} 个雷：挖出第一颗宝石 {{multiplier}} 倍', {
                mines: mineCount,
                multiplier: firstMultiplier.toFixed(2),
              })}
            </div>
            {closed && (
              <div className='games-stage-hint'>
                {t('这个游戏已经关闭，不能再开新的一局')}
              </div>
            )}
            {!closed && stake > balance && (
              <div className='games-stage-hint'>{t('游戏币余额不足')}</div>
            )}
          </div>
        )}
      </div>
    </GameLayout>
  );
};

export default Mines;
