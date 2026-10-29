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
import { Switch } from '@douyinfe/semi-ui';
import { ChevronsUp, Hand, Lightbulb, Plus, Split } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import BetInput from '../common/BetInput';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { percent } from '../common/api';
import {
  WagerStats,
  playWagerFeedback,
  wagerResultMessage,
} from '../common/wager';
import { readGameStorage, writeGameStorage } from '../common/gameStorage';
import { playSound } from '../common/sound';
import { useGameInfo } from '../common/useGameInfo';
import { RANK_LABELS, SUIT_ICONS, isRedSuit } from '../common/cards';
import { useWagerRound } from '../common/useWagerRound';
import { basicStrategy } from './strategy';
import './blackjack.css';

// 一张牌：左上角是点数与小花色(牌叠在一起时也看得见)，右下角是大花色，红心与方块是红色。faceDown 时画成牌背
// (庄家还没翻开的暗牌)。
const BlackjackCard = ({ card, faceDown, index }) => {
  const { t } = useTranslation();
  const style = { '--card-index': index };
  if (faceDown) {
    return (
      <div
        className='blackjack-card is-back'
        style={style}
        role='img'
        aria-label={t('暗牌')}
      />
    );
  }
  const Suit = SUIT_ICONS[card.suit];
  const red = isRedSuit(card.suit);
  return (
    <div
      className={red ? 'blackjack-card is-red' : 'blackjack-card'}
      style={style}
    >
      <span className='blackjack-card-corner'>
        <span className='blackjack-card-rank'>
          {RANK_LABELS[card.rank - 1]}
        </span>
        <Suit className='blackjack-card-pip' />
      </span>
      <Suit className='blackjack-card-suit' />
    </div>
  );
};

// 一手牌的点数：有 A 按 11 算时是软点数。
const HandTotal = ({ total, soft }) => {
  const { t } = useTranslation();
  if (total > 21) {
    return <span className='blackjack-total is-bust'>{t('爆牌')}</span>;
  }
  return (
    <span className='blackjack-total'>
      {soft ? t('软 {{total}}', { total }) : total}
    </span>
  );
};

// 玩家的一手牌：牌、点数、下注，结束后还有这一手的结果与派彩。分牌后两手并排，轮到的那一手高亮。
const BlackjackHand = ({ hand, title, symbol }) => {
  const { t } = useTranslation();
  const outcomes = {
    win: t('赢'),
    lose: t('输'),
    push: t('平局'),
    blackjack: t('黑杰克'),
  };
  return (
    <div className={`blackjack-hand is-${hand.status}`}>
      <div className='blackjack-hand-head'>
        {title && <span className='blackjack-hand-title'>{title}</span>}
        {hand.status === 'blackjack' ? (
          <span className='blackjack-total is-blackjack'>{t('黑杰克')}</span>
        ) : (
          <HandTotal total={hand.total} soft={hand.soft} />
        )}
      </div>
      <div className='blackjack-cards'>
        {hand.cards.map((card, index) => (
          <BlackjackCard key={index} card={card} index={index} />
        ))}
      </div>
      <div className='blackjack-hand-foot'>
        <span className='blackjack-bet'>
          {t('下注 {{bet}} {{symbol}}', { bet: hand.bet, symbol })}
          {hand.doubled && ` · ${t('已加倍')}`}
        </span>
        {hand.result && (
          <span className={`blackjack-result is-${hand.result}`}>
            {outcomes[hand.result]}
          </span>
        )}
        {hand.payout > 0 && (
          <span className='blackjack-payout'>
            {t('派彩 {{payout}}', { payout: hand.payout })}
          </span>
        )}
      </div>
    </div>
  );
};

// 策略提示的开关保存在浏览器里。
const ADVICE_KEY = 'game-blackjack-advice';
// 键盘操作：H 要牌、S 停牌、D 加倍、P 分牌。
const ACTION_KEYS = {
  KeyH: 'hit',
  KeyS: 'stand',
  KeyD: 'double',
  KeyP: 'split',
};

// 一局结束后的提示。只有一手时说明输在哪里；分牌后按两手合计的派彩提示。
function blackjackResultMessage(round, t, symbol) {
  const dealer = round.dealer;
  const note =
    dealer.total > 21
      ? t('庄家爆牌')
      : t('庄家 {{total}} 点', { total: dealer.total });
  const [hand] = round.hands;
  if (round.hands.length === 1 && hand.result === 'blackjack') {
    return {
      kind: 'win',
      text: t('黑杰克！赢得 {{win}} {{symbol}}', { win: round.win, symbol }),
      note,
    };
  }
  if (round.win > 0) {
    return {
      ...wagerResultMessage({ bet: round.bet, win: round.win }, t, symbol),
      note,
    };
  }
  let text = t('输掉 {{loss}} {{symbol}}', { loss: round.bet, symbol });
  if (round.hands.length === 1 && hand.status === 'bust') {
    text = t('爆牌了，输掉 {{loss}} {{symbol}}', { loss: round.bet, symbol });
  } else if (dealer.cards.length === 2 && dealer.total === 21) {
    text = t('庄家是黑杰克，输掉 {{loss}} {{symbol}}', {
      loss: round.bet,
      symbol,
    });
  }
  return { kind: 'loss', text, note };
}

const Blackjack = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, setInfo, reload } =
    useGameInfo('blackjack');
  const { round, busy, error, refresh, send } = useWagerRound(
    'blackjack',
    setInfo,
    reload,
  );
  const [bet, setBet] = useState(2);
  const [advice, setAdvice] = useState(
    () => readGameStorage(ADVICE_KEY).value === true,
  );
  const toggleAdvice = (checked) => {
    setAdvice(checked);
    writeGameStorage(ADVICE_KEY, checked);
  };
  // 发出的牌变多时响一声发牌；同一局从进行中变成结束时揭晓输赢。
  const seenRef = useRef({ session: 0, cards: 0, status: '' });
  useEffect(() => {
    if (!round) return;
    const cards =
      round.dealer.cards.length +
      round.hands.reduce((sum, item) => sum + item.cards.length, 0);
    const previous = seenRef.current;
    const same = previous.session === round.session_id;
    if (!same || cards > previous.cards) playSound('deal');
    if (same && previous.status === 'playing' && round.status === 'finished') {
      playWagerFeedback({ bet: round.bet, win: round.win });
    } else if (!same && round.status === 'finished' && previous.session) {
      // 发牌就结束(黑杰克)的一局：同样揭晓。
      playWagerFeedback({ bet: round.bet, win: round.win });
    }
    seenRef.current = {
      session: round.session_id,
      cards,
      status: round.status,
    };
  }, [round]);

  // 拿到游戏信息后读出没结束的一局，刷新页面、游戏关闭之后都能接着打。
  const loaded = !!info;
  useEffect(() => {
    if (loaded) refresh();
  }, [loaded, refresh]);

  const meta = getGameMetaMap(t).blackjack;
  // 游戏关闭后只有还没结束一局的玩家能进来，玩完这一局就不能再开新局。
  const closed = info?.enabled === false;
  const symbol = info?.symbol || '';
  const balance = info?.balance || 0;
  const minBet = info?.min_bet || 0.01;
  const maxBet = Math.max(info?.max_bet || minBet, minBet);
  const stake = Math.min(Math.max(bet, minBet), maxBet);
  const playing = round?.status === 'playing';
  const finished = round?.status === 'finished';
  const actions = round?.actions || [];
  const hand = playing ? round.hands[round.current] : null;
  // 加倍、分牌要追加这一手的下注，余额不够时按钮不可用，服务端也会拒绝。
  const canRaise = hand ? balance >= hand.bet : false;

  let message = {
    kind: '',
    text: t('下注后发牌，点数比庄家更接近 21 点就赢'),
  };
  if (playing && hand) {
    message = {
      kind: '',
      text:
        round.hands.length > 1
          ? t('第 {{hand}} 手 {{total}} 点，要牌还是停牌？', {
              hand: round.current + 1,
              total: hand.total,
            })
          : t('你的点数 {{total}}，要牌还是停牌？', { total: hand.total }),
      note: round.peeked ? t('庄家已查看底牌，不是黑杰克') : undefined,
    };
  } else if (finished) {
    message = blackjackResultMessage(round, t, symbol);
  }

  const suggested =
    advice && playing && hand && round.dealer.cards[0]
      ? basicStrategy(hand, round.dealer.cards[0], actions)
      : '';

  // 键盘：H/S/D/P 对应要牌、停牌、加倍、分牌，焦点在输入框里时不响应。
  const keyRef = useRef(null);
  keyRef.current = (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.repeat) return;
    if (event.target.closest?.('input, textarea, [contenteditable="true"]')) {
      return;
    }
    const action = ACTION_KEYS[event.code];
    if (!action || !playing || busy || !actions.includes(action)) return;
    if ((action === 'double' || action === 'split') && !canRaise) return;
    event.preventDefault();
    send('action', { action });
  };
  useEffect(() => {
    const onKeyDown = (event) => keyRef.current(event);
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  const buttons = [
    { action: 'hit', Icon: Plus, label: t('要牌'), key: 'H' },
    { action: 'stand', Icon: Hand, label: t('停牌'), key: 'S' },
    {
      action: 'double',
      Icon: ChevronsUp,
      label: t('加倍'),
      raise: true,
      key: 'D',
    },
    { action: 'split', Icon: Split, label: t('分牌'), raise: true, key: 'P' },
  ];
  const actionNames = Object.fromEntries(
    buttons.map((item) => [item.action, item.label]),
  );

  const side = info && (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('规则')}>
        <RuleList
          items={[
            t('6 副牌，每局重新洗牌；庄家不到 17 点要牌，17 点(含软 17)停牌'),
            t(
              '黑杰克（首两张 21 点）净赢下注的 1.5 倍；普通获胜派彩 2 倍，平局退回下注，支持小数下注',
            ),
            t('首两张可以加倍，追加同样的下注、只再发一张'),
            t(
              '两张点数相同(10、J、Q、K 都算 10 点)可以分牌一次，分开的 A 各只补一张',
            ),
            t('庄家明牌是 A 或 10 点时先查看底牌，是黑杰克就直接结算'),
            t('分牌后的 21 点不算黑杰克；没有保险和投降'),
            t(
              '键盘 H 要牌、S 停牌、D 加倍、P 分牌；打开策略提示会按基本策略给出建议',
            ),
            info.payback &&
              t('按基本策略玩，理论返还率约 {{rate}}', {
                rate: percent(info.payback, 1),
              }),
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
      className='blackjack-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='blackjack-table'>
          <div className='blackjack-dealer'>
            <div className='blackjack-hand-head'>
              <span className='blackjack-hand-title'>{t('庄家')}</span>
              {round &&
                (round.dealer.hidden ? (
                  <span className='blackjack-total'>
                    {round.dealer.total} + ?
                  </span>
                ) : (
                  <HandTotal
                    total={round.dealer.total}
                    soft={round.dealer.soft}
                  />
                ))}
            </div>
            <div className='blackjack-cards' key={round?.session_id}>
              {round ? (
                <>
                  {round.dealer.cards.map((card, index) => (
                    <BlackjackCard key={index} card={card} index={index} />
                  ))}
                  {round.dealer.hidden && (
                    <BlackjackCard key='hole' faceDown index={1} />
                  )}
                </>
              ) : (
                <>
                  <BlackjackCard faceDown index={0} />
                  <BlackjackCard faceDown index={1} />
                </>
              )}
            </div>
          </div>

          <div className='blackjack-hands' key={round?.session_id}>
            {round ? (
              round.hands.map((item, index) => (
                <BlackjackHand
                  key={index}
                  hand={item}
                  symbol={symbol}
                  title={
                    round.hands.length > 1
                      ? t('第 {{hand}} 手', { hand: index + 1 })
                      : ''
                  }
                />
              ))
            ) : (
              <div className='blackjack-hand is-idle'>
                <div className='blackjack-cards'>
                  <BlackjackCard faceDown index={0} />
                  <BlackjackCard faceDown index={1} />
                </div>
              </div>
            )}
          </div>
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing ? (
          <div className='game-controls'>
            <div className='blackjack-actions'>
              {buttons.map(({ action, Icon, label, raise, key }) => (
                <button
                  key={action}
                  type='button'
                  className={`blackjack-action${suggested === action ? ' is-suggested' : ''}`}
                  disabled={
                    busy || !actions.includes(action) || (raise && !canRaise)
                  }
                  aria-keyshortcuts={key}
                  onClick={() => send('action', { action })}
                >
                  <Icon size={16} />
                  {label}
                  <kbd className='blackjack-key'>{key}</kbd>
                </button>
              ))}
            </div>
            {suggested && (
              <div className='blackjack-advice' role='status'>
                <Lightbulb size={14} />
                {t('基本策略建议：{{action}}', {
                  action: actionNames[suggested],
                })}
              </div>
            )}
            {!canRaise &&
              (actions.includes('double') || actions.includes('split')) && (
                <div className='games-stage-hint'>
                  {t('余额不够追加下注，不能加倍或分牌')}
                </div>
              )}
          </div>
        ) : (
          <div className='game-controls'>
            <BetInput
              value={stake}
              min={minBet}
              max={maxBet}
              symbol={symbol}
              disabled={busy}
              onChange={setBet}
            />
            <button
              type='button'
              className='games-play-button'
              disabled={busy || closed || stake > balance}
              onClick={() => send('deal', { bet: stake }, false)}
            >
              {finished ? t('再来一局') : t('发牌')}
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
        <label className='blackjack-advice-toggle'>
          <Switch size='small' checked={advice} onChange={toggleAdvice} />
          {t('策略提示')}
        </label>
      </div>
    </GameLayout>
  );
};

export default Blackjack;
