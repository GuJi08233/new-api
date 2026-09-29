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
import { ArrowDown, ArrowUp, Equal, SkipForward } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import BetInput from '../common/BetInput';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { percent } from '../common/api';
import { WagerStats, wagerResultMessage } from '../common/wager';
import { useGameInfo } from '../common/useGameInfo';
import { RANK_LABELS, SUIT_ICONS, isRedSuit } from '../common/cards';
import { useWagerRound } from '../common/useWagerRound';
import './hilo.css';

const GUESS_ICONS = {
  higher: ArrowUp,
  lower: ArrowDown,
  same: Equal,
  skip: SkipForward,
};

// 一张牌：左上角是点数与小花色，右下角是大花色(历史记录里的小牌不画)，红心与方块是红色。
const HiloCard = ({ card, small }) => {
  const Suit = SUIT_ICONS[card.suit];
  const classes = ['hilo-card'];
  if (small) classes.push('is-small');
  if (isRedSuit(card.suit)) classes.push('is-red');
  return (
    <div className={classes.join(' ')}>
      <span className='hilo-card-corner'>
        <span className='hilo-card-rank'>{RANK_LABELS[card.rank - 1]}</span>
        <Suit className='hilo-card-pip' />
      </span>
      {!small && <Suit className='hilo-card-suit' />}
    </div>
  );
};

// 猜法的说明：A 上的猜大、K 上的猜小是严格大于、小于，其余的猜大、猜小含相同点数。
function guessLabel(guess, rank, t) {
  const label = RANK_LABELS[rank - 1];
  switch (guess) {
    case 'higher':
      return rank === 1
        ? t('比 {{rank}} 大', { rank: label })
        : t('{{rank}} 或更大', { rank: label });
    case 'lower':
      return rank === 13
        ? t('比 {{rank}} 小', { rank: label })
        : t('{{rank}} 或更小', { rank: label });
    default:
      return t('还是 {{rank}}', { rank: label });
  }
}

// 一局结束后的提示：猜错输掉下注；兑现时说明猜对了几次、累计多少倍，到顶时是自动兑现。
function hiloResultMessage(round, lost, maxMultiplier, t, symbol) {
  if (lost) {
    return {
      kind: 'loss',
      text: t('猜错了，输掉 {{bet}} {{symbol}}', { bet: round.bet, symbol }),
    };
  }
  const message = wagerResultMessage(
    { bet: round.bet, win: round.win },
    t,
    symbol,
  );
  const note =
    round.win >= round.bet * maxMultiplier
      ? t('到达 {{max}} 倍上限，自动兑现', { max: maxMultiplier })
      : t('猜对 {{wins}} 次，累计 {{multiplier}} 倍', {
          wins: round.wins,
          multiplier: round.multiplier,
        });
  return { ...message, note };
}

const Hilo = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, setInfo, reload } = useGameInfo('hilo');
  const { round, busy, error, refresh, send } = useWagerRound(
    'hilo',
    setInfo,
    reload,
  );
  const [bet, setBet] = useState(1);

  // 拿到游戏信息后读出没结束的一局，刷新页面、游戏关闭之后都能接着猜。
  const loaded = !!info;
  useEffect(() => {
    if (loaded) refresh();
  }, [loaded, refresh]);

  const meta = getGameMetaMap(t).hilo;
  // 游戏关闭后只有还没结束一局的玩家能进来，玩完这一局就不能再开新局。
  const closed = info?.enabled === false;
  const symbol = info?.symbol || '';
  const balance = info?.balance || 0;
  const maxBet = Math.max(info?.max_bet || 1, 1);
  const maxMultiplier = info?.max_multiplier || 1000;
  const stake = Math.min(bet, maxBet);
  const playing = round?.status === 'playing';
  const finished = round?.status === 'finished';
  const history = round?.history || [];
  const lastMove = history[history.length - 1];
  const lost =
    finished && !!lastMove && lastMove.guess !== 'skip' && !lastMove.correct;
  const rank = round?.card?.rank;

  let message = {
    kind: '',
    text: t('下注后翻开第一张牌，猜下一张更大还是更小'),
  };
  if (playing) {
    message = {
      kind: '',
      text:
        round.wins > 0
          ? t('已猜对 {{wins}} 次，继续猜或兑现', { wins: round.wins })
          : t('猜下一张牌，猜对一次就能兑现'),
    };
  } else if (finished) {
    message = hiloResultMessage(round, lost, maxMultiplier, t, symbol);
  }

  const side = info && (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('规则')}>
        <RuleList
          items={[
            t('无限副牌，每张牌的点数 A 到 K 等概率，花色只用于显示'),
            t(
              '当前牌是 2 到 Q 时猜更大或更小(都含相同点数)；A 只能猜更大或相同，K 只能猜更小或相同',
            ),
            t(
              '猜对这一步的倍数 = 返还率 × 13 ÷ 猜对的点数种数，累计相乘；猜错输掉下注',
            ),
            t('每局最多换 {{count}} 次牌，换牌不改变倍数', {
              count: info.max_skips,
            }),
            t(
              '至少猜对一次才能兑现，派彩向下取整；到下注的 {{max}} 倍时自动兑现',
              { max: maxMultiplier },
            ),
            t('每猜一次的理论返还率都是 {{rate}}%', { rate: info.rtp_percent }),
            t('单次最多下注 {{max}} {{symbol}}', { max: maxBet, symbol }),
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
      className='hilo-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='game-hud'>
          <span>
            {t('累计倍数')}
            <strong>×{round?.multiplier || '1.00'}</strong>
          </span>
          <span>
            {t('猜对')}
            <strong>{round?.wins || 0}</strong>
          </span>
          <span>
            {t('换牌剩余')}
            <strong>{round ? round.skips_left : info?.max_skips || 0}</strong>
          </span>
        </div>

        <div className='hilo-table'>
          <div className='hilo-deck' aria-hidden />
          {round ? (
            <div
              key={`${round.session_id}-${history.length}`}
              className={lost ? 'hilo-current is-lost' : 'hilo-current'}
            >
              <HiloCard card={round.card} />
            </div>
          ) : (
            <div className='hilo-current'>
              <div className='hilo-card is-back' />
            </div>
          )}
        </div>

        {playing && (
          <div className='hilo-options'>
            {round.options.map((option) => {
              const Icon = GUESS_ICONS[option.guess];
              return (
                <button
                  key={option.guess}
                  type='button'
                  className={`hilo-option is-${option.guess}`}
                  disabled={busy}
                  onClick={() => send('guess', { guess: option.guess })}
                >
                  <span className='hilo-option-label'>
                    <Icon size={18} />
                    {guessLabel(option.guess, rank, t)}
                  </span>
                  <span className='hilo-option-chance'>
                    {t('概率 {{chance}}', {
                      chance: percent(option.chance, 1),
                    })}
                  </span>
                  <span className='hilo-option-payout'>
                    ×{option.multiplier} · {option.payout} {symbol}
                  </span>
                </button>
              );
            })}
          </div>
        )}

        {history.length > 0 && (
          <ol className='hilo-history' aria-label={t('之前的牌')}>
            {history.map((item, index) => {
              const Icon = GUESS_ICONS[item.guess];
              let state = item.correct ? 'is-correct' : 'is-wrong';
              if (item.guess === 'skip') state = 'is-skip';
              return (
                <li key={index} className={`hilo-history-item ${state}`}>
                  <HiloCard card={item.card} small />
                  <span className='hilo-history-guess'>
                    <Icon size={12} />
                  </span>
                </li>
              );
            })}
          </ol>
        )}

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing ? (
          <div className='game-controls'>
            <button
              type='button'
              className='hilo-skip'
              disabled={busy || round.skips_left <= 0}
              onClick={() => send('guess', { guess: 'skip' })}
            >
              <SkipForward size={16} />
              {t('换一张（剩 {{count}} 次）', { count: round.skips_left })}
            </button>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || round.wins === 0}
              onClick={() => send('cashout')}
            >
              {round.wins === 0
                ? t('猜对一次才能兑现')
                : t('兑现 {{amount}} {{symbol}}', {
                    amount: round.cashout,
                    symbol,
                  })}
            </button>
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
            <button
              type='button'
              className='games-play-button'
              disabled={busy || closed || stake > balance}
              onClick={() => send('start', { bet: stake }, false)}
            >
              {finished ? t('再来一局') : t('翻开第一张')}
            </button>
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

export default Hilo;
