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
import { useSearchParams } from 'react-router-dom';
import { Button, InputNumber, Modal, Typography } from '@douyinfe/semi-ui';
import { Bot, Copy, Flag, Swords, UserRound, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  copy,
  showError,
  showSuccess,
  timestamp2string,
} from '../../../../helpers';
import { getGameLevelNames } from '../../../../constants';
import GameLayout from '../GameLayout';
import { RuleList, SideBox, StageMessage } from '../SideBox';
import { gameGet, gamePost, withTurnstile } from '../api';
import {
  formatDuration,
  playTimeNote,
  rewardsLeft,
  skillResultMessage,
} from '../skill';
import { usePlayTurnstile } from '../usePlayTurnstile';
import { useGameInfo } from '../useGameInfo';
import { isRoomOpen, usePvpRoom } from './usePvpRoom';
import './pvp.css';

const { Text } = Typography;

// 大厅每隔这么久刷新一次等待中的房间。
const LOBBY_POLL_MS = 5000;
// 押注的快捷档位，超过押注上限的不显示；0 是友谊赛。
const STAKE_CHIPS = [0, 1, 5, 10, 50, 100, 500];

// 对局双方：座位称呼(执黑、执白……)、名字，轮到的一方高亮，extra 是每个座位后面的附加内容(例如计时)。
const Players = ({ names, labels, toMove, seat, extra }) => (
  <div className='pvp-players'>
    {[0, 1].map((index) => (
      <div
        key={index}
        className={`pvp-player ${toMove === index || toMove === -1 ? 'is-turn' : ''} ${index === seat ? 'is-me' : ''}`}
      >
        <span className='pvp-player-seat'>{labels[index]}</span>
        <span className='pvp-player-name'>{names[index]}</span>
        {extra?.[index]}
      </div>
    ))}
  </div>
);

// 大厅：押注、快速匹配、开房间、邀请好友、等待中的房间，以及人机对战的入口。
const Lobby = ({
  info,
  lobby,
  stake,
  setStake,
  aiLevel,
  setAiLevel,
  busy,
  error,
  turnstile,
  onQuickMatch,
  onCreate,
  onJoin,
  onStartAi,
}) => {
  const { t } = useTranslation();
  const symbol = info.symbol;
  const rule = info.rule || {};
  const aiInfo = info.ai;
  const levelNames = getGameLevelNames(t).ai;
  const stakes = STAKE_CHIPS.filter((value) => value <= (rule.max_stake || 0));
  const rooms = lobby?.rooms || [];
  const balance = info.balance || 0;
  return (
    <div className='pvp-lobby'>
      <div className='games-stage pvp-card'>
        <div className='pvp-card-title'>
          <Swords size={18} />
          {t('和玩家对战')}
        </div>
        <div className='pvp-stake'>
          <span className='pvp-stake-label'>{t('押注')}</span>
          <div className='game-chips'>
            {stakes.map((value) => (
              <button
                key={value}
                type='button'
                className={
                  value === stake ? 'game-chip is-active' : 'game-chip'
                }
                onClick={() => setStake(value)}
              >
                {value === 0 ? t('友谊赛') : `${value} ${symbol}`}
              </button>
            ))}
          </div>
          {rule.max_stake > 0 && (
            <InputNumber
              value={stake}
              min={0}
              max={rule.max_stake}
              precision={0}
              suffix={symbol}
              style={{ width: 140 }}
              onChange={(value) =>
                setStake(typeof value === 'number' ? value : 0)
              }
            />
          )}
        </div>
        <div className='pvp-actions'>
          <button
            type='button'
            className='games-play-button'
            disabled={busy || stake > balance}
            onClick={onQuickMatch}
          >
            {t('快速匹配')}
          </button>
          <Button
            disabled={busy || stake > balance}
            onClick={() => onCreate(false)}
          >
            {t('开一个房间')}
          </Button>
          <Button
            disabled={busy || stake > balance}
            icon={<Users size={14} />}
            onClick={() => onCreate(true)}
          >
            {t('邀请好友')}
          </Button>
        </div>
        {stake > balance && (
          <div className='games-stage-hint'>{t('游戏币余额不足')}</div>
        )}
        {error && <StageMessage kind='error'>{error}</StageMessage>}
        <div className='pvp-rooms'>
          <div className='pvp-rooms-title'>
            {t('等待对手的房间')}
            <Text type='tertiary' size='small'>
              {rooms.length}
            </Text>
          </div>
          {rooms.length === 0 ? (
            <div className='pvp-empty'>
              {t('还没有人开房间，快速匹配会开一个房间等别人来')}
            </div>
          ) : (
            <ul className='pvp-room-list'>
              {rooms.map((item) => (
                <li key={item.id}>
                  <UserRound size={14} />
                  <span className='pvp-room-host'>{item.host}</span>
                  <span className='pvp-room-stake'>
                    {item.stake > 0 ? `${item.stake} ${symbol}` : t('友谊赛')}
                  </span>
                  <Button
                    size='small'
                    theme='solid'
                    disabled={busy || item.own || item.stake > balance}
                    onClick={() => onJoin(item.id)}
                  >
                    {item.own ? t('我的房间') : t('加入')}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {rule.ai_enabled && aiInfo && (
        <div className='games-stage pvp-card'>
          <div className='pvp-card-title'>
            <Bot size={18} />
            {t('人机对战')}
          </div>
          <div className='game-chips'>
            {levelNames.map((name, index) => {
              const level = index + 1;
              const tier = [...(aiInfo.rule?.tiers || [])]
                .reverse()
                .find((item) => item.score <= level);
              return (
                <button
                  key={level}
                  type='button'
                  className={
                    level === aiLevel ? 'game-chip is-active' : 'game-chip'
                  }
                  onClick={() => setAiLevel(level)}
                >
                  {name}
                  {tier && ` · ${tier.coins} ${symbol}`}
                </button>
              );
            })}
          </div>
          <div className='pvp-actions'>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || !turnstile.ready}
              onClick={onStartAi}
            >
              {t('开始人机对战')}
            </button>
          </div>
          <div className='games-stage-counter'>
            {aiInfo.rule?.daily_rewards > 0
              ? t('赢了按难度领奖，今天还能领 {{left}} 次', {
                  left: rewardsLeft(aiInfo.rule, aiInfo.progress),
                })
              : t('人机对战暂不发放奖励')}
          </div>
          {turnstile.failed && (
            <div className='games-stage-hint'>
              {t('人机验证加载失败，请刷新页面重试')}
            </div>
          )}
          {turnstile.widget}
        </div>
      )}
    </div>
  );
};

// 押注对局：双方、计时、棋盘、结果与操作。
const RoomView = ({
  room,
  symbol,
  seatLabels,
  statusText,
  resultNote,
  renderBoard,
  clockOffset,
  busy,
  error,
  onMove,
  onResign,
  onCancel,
  onCopyInvite,
  onBack,
}) => {
  const { t } = useTranslation();
  const [now, setNow] = useState(() => Date.now() / 1000);
  const playing = room.status === 'playing';
  // 对局中每半秒走一次钟，显示这一步还剩多少秒。
  useEffect(() => {
    if (!playing) return undefined;
    const timer = setInterval(() => setNow(Date.now() / 1000), 500);
    return () => clearInterval(timer);
  }, [playing]);

  const seat = room.seat;
  const state = room.state;
  const myTurn = playing && (room.to_move === seat || room.to_move === -1);
  // 服务器时间只精确到秒，刚走完一步时算出来可能比每步限时多一秒。
  const secondsLeft = room.deadline
    ? Math.min(
        Math.max(Math.ceil(room.deadline - (now + clockOffset())), 0),
        room.move_seconds || Infinity,
      )
    : 0;
  const pot = room.stake * 2;
  const me = room.players[seat]?.user_id;

  let message;
  let kind = '';
  let note = '';
  if (room.status === 'waiting') {
    message = room.private
      ? t('把邀请链接发给朋友，等对方加入')
      : t('等待对手加入…');
  } else if (room.status === 'cancelled') {
    message =
      room.end_reason === 'expired'
        ? t('等了太久没人加入，房间已关闭，押注已退回')
        : t('房间已关闭，押注已退回');
  } else if (room.status === 'finished') {
    const won = room.winner_id === me;
    if (room.winner_id === 0) {
      message = room.stake > 0 ? t('平局，押注已退回') : t('平局');
    } else if (won) {
      kind = room.stake > 0 ? 'big-win' : 'win';
      message =
        room.stake > 0
          ? t('你赢了！获得 {{win}} {{symbol}}', {
              win: pot - room.rake,
              symbol,
            })
          : t('你赢了！');
    } else {
      kind = 'loss';
      message = t('你输了');
    }
    if (room.end_reason === 'timeout') {
      note = won ? t('对手超时') : t('你超时了');
    } else if (room.end_reason === 'resign') {
      note = won ? t('对手认输') : t('你认输了');
    } else {
      note = resultNote?.({ state, seat }) || '';
    }
  } else {
    message =
      statusText?.({ room, state, seat, myTurn }) ||
      (myTurn ? t('轮到你了') : t('等待对手走棋'));
  }

  const names = [0, 1].map((index) => {
    const player = room.players[index];
    if (!player) return t('等待加入…');
    return index === seat ? t('我') : player.name;
  });
  const clocks = [0, 1].map((index) =>
    playing && (room.to_move === index || room.to_move === -1) ? (
      <span
        key='clock'
        className={secondsLeft <= 10 ? 'pvp-clock is-low' : 'pvp-clock'}
      >
        {secondsLeft}s
      </span>
    ) : null,
  );

  return (
    <div className='games-stage pvp-stage'>
      <div className='pvp-stage-head'>
        {room.stake > 0
          ? t('押注 {{stake}} {{symbol}} · 奖池 {{pot}}', {
              stake: room.stake,
              pot,
              symbol,
            })
          : t('友谊赛')}
      </div>
      <Players
        names={names}
        labels={seatLabels(state)}
        toMove={playing ? room.to_move : null}
        seat={seat}
        extra={clocks}
      />
      {renderBoard({
        state,
        seat,
        canMove: myTurn && !busy,
        busy,
        over: !isRoomOpen(room),
        onMove,
      })}
      <StageMessage kind={kind} note={note}>
        {message}
      </StageMessage>
      {error && <StageMessage kind='error'>{error}</StageMessage>}
      <div className='pvp-actions'>
        {room.status === 'waiting' && (
          <>
            {room.code && (
              <Button icon={<Copy size={14} />} onClick={onCopyInvite}>
                {t('复制邀请链接')}
              </Button>
            )}
            <Button type='danger' disabled={busy} onClick={onCancel}>
              {t('取消房间')}
            </Button>
          </>
        )}
        {playing && (
          <Button icon={<Flag size={14} />} disabled={busy} onClick={onResign}>
            {t('认输')}
          </Button>
        )}
        {!isRoomOpen(room) && (
          <button type='button' className='games-play-button' onClick={onBack}>
            {t('返回大厅')}
          </button>
        )}
      </div>
    </div>
  );
};

// 人机对战的一局：玩家坐座位 0，AI 坐座位 1。
const AiView = ({
  ai,
  result,
  symbol,
  seatLabels,
  statusText,
  resultNote,
  renderBoard,
  busy,
  error,
  turnstile,
  onMove,
  onResign,
  onRestart,
  onBack,
}) => {
  const { t } = useTranslation();
  const levelNames = getGameLevelNames(t).ai;
  const state = ai.state;
  const myTurn = !ai.over && ai.to_move === 0;
  let message =
    statusText?.({ room: null, state, seat: 0, myTurn }) ||
    (myTurn ? t('轮到你了') : t('AI 思考中…'));
  let kind = '';
  let note = '';
  if (ai.over) {
    const reason = resultNote?.({ state, seat: 0 });
    if (ai.winner === 0) {
      const reward = skillResultMessage(result, t, symbol, { byLevel: true });
      kind = result?.reward > 0 ? 'win' : '';
      message = t('你赢了！');
      note = [reason, reward.text, reward.note].filter(Boolean).join(' · ');
    } else if (ai.winner === 1) {
      kind = 'loss';
      message = t('AI 获胜，再接再厉');
      note = reason || '';
    } else {
      message = t('和棋');
      note = reason || '';
    }
  }
  return (
    <div className='games-stage pvp-stage'>
      <Players
        names={[
          t('我'),
          t('AI（{{level}}）', { level: levelNames[ai.level - 1] }),
        ]}
        labels={seatLabels(state)}
        toMove={ai.over ? null : ai.to_move}
        seat={0}
      />
      {renderBoard({
        state,
        seat: 0,
        canMove: myTurn && !busy,
        busy,
        over: ai.over,
        onMove,
      })}
      <StageMessage kind={kind} note={note}>
        {message}
      </StageMessage>
      {error && <StageMessage kind='error'>{error}</StageMessage>}
      <div className='pvp-actions'>
        {!ai.over ? (
          <>
            <Button
              icon={<Flag size={14} />}
              disabled={busy}
              onClick={onResign}
            >
              {t('认输')}
            </Button>
            {/* 操作失败时(例如这一局已经过期)也能回到大厅重新开始，不会卡在这一局上。 */}
            {error && <Button onClick={onBack}>{t('返回大厅')}</Button>}
          </>
        ) : (
          <>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || !turnstile.ready}
              onClick={onRestart}
            >
              {t('再来一局')}
            </button>
            <Button onClick={onBack}>{t('返回大厅')}</Button>
          </>
        )}
      </div>
      {ai.over && turnstile.widget}
    </div>
  );
};

// 信息栏：对战规则、人机对战的奖励与最近的押注对局。
const PvpSide = ({ info, rules, randomFirst }) => {
  const { t } = useTranslation();
  const levelNames = getGameLevelNames(t).ai;
  const symbol = info.symbol;
  const rule = info.rule || {};
  const aiInfo = info.ai;
  const aiTimeNote = aiInfo
    ? playTimeNote(aiInfo.rule, aiInfo.progress, t)
    : '';
  const recent = info.recent || [];
  return (
    <>
      <SideBox label={t('对战规则')}>
        <RuleList
          items={[
            ...rules,
            rule.max_stake > 0
              ? t(
                  '双方押同样多的游戏币，赢家拿走奖池，平台抽取 {{rake}}%（向下取整）；平局退回押注',
                  { rake: rule.rake_percent },
                )
              : t('只能开友谊赛，不押注'),
            rule.max_stake > 0 &&
              t(
                '每人最多押注 {{max}} {{symbol}}；同一 IP 下的账号不能互相押注对战',
                { max: rule.max_stake, symbol },
              ),
            t('每一步限时 {{seconds}} 秒，超时判负', {
              seconds: rule.move_seconds,
            }),
            randomFirst && t('先走的一方随机决定'),
          ]}
        />
      </SideBox>
      {rule.ai_enabled && aiInfo && (
        <SideBox
          label={t('人机对战奖励')}
          value={aiInfo.progress?.earned_today || 0}
          unit={symbol}
          note={
            aiInfo.rule?.daily_rewards > 0
              ? t('今天还能领 {{left}} 次', {
                  left: rewardsLeft(aiInfo.rule, aiInfo.progress),
                })
              : t('人机对战暂不发放奖励')
          }
        >
          {aiTimeNote && <div className='game-box-note'>{aiTimeNote}</div>}
          <RuleList
            items={[
              ...(aiInfo.rule?.tiers || []).map((tier) =>
                t('赢下{{level}}难度奖励 {{coins}} {{symbol}}', {
                  level: levelNames[tier.score - 1],
                  coins: tier.coins,
                  symbol,
                }),
              ),
              aiInfo.rule?.daily_rewards > 0 &&
                t('每天最多领 {{count}} 次，同一 IP 下所有账号合计', {
                  count: aiInfo.rule.daily_rewards,
                }),
              aiInfo.rule?.min_seconds_per_coin > 0 &&
                t(
                  '每个游戏币需要当天和 AI 累计下满 {{time}}，先赢的对局等时长够了再发',
                  { time: formatDuration(aiInfo.rule.min_seconds_per_coin, t) },
                ),
            ]}
          />
        </SideBox>
      )}
      <SideBox label={t('最近的对局')}>
        {recent.length === 0 ? (
          <div className='game-box-note'>{t('还没有下过对局')}</div>
        ) : (
          <ul className='pvp-recent'>
            {recent.map((item) => (
              <li key={item.id} className={`is-${item.result}`}>
                <span className='pvp-recent-result'>
                  {{ win: t('胜'), loss: t('负'), draw: t('平') }[item.result]}
                </span>
                <span className='pvp-recent-opponent'>{item.opponent}</span>
                <span>
                  {item.stake > 0 ? `${item.stake} ${symbol}` : t('友谊赛')}
                </span>
                <span className='pvp-recent-time'>
                  {timestamp2string(item.finished_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </SideBox>
    </>
  );
};

// 对战游戏页的外壳：大厅(快速匹配、开房间、邀请好友、加入房间)、押注对局(长轮询、计时、认输)与人机对战，各游戏只提供棋盘。
// renderBoard({ state, seat, canMove, busy, over, onMove }) 画出棋盘，state 为空时画空棋盘；over 表示对局已经结束(含超时、
// 认输，这时引擎的状态不一定是结束的)；玩家落子时调用 onMove(走法)。seatLabels(state) 返回两个座位的称呼(例如执黑、执白)；
// statusText({ room, state, seat, myTurn }) 可以按游戏改写对局中的提示(人机对战里 room 为空)；resultNote({ state, seat })
// 说明正常下完的一局为什么结束(例如将死、困毙)；rules 是信息栏里的规则，randomFirst 为假时(双方同时出招)不写先走的规则。
const PvpGame = ({
  gameKey,
  title,
  renderBoard,
  seatLabels,
  statusText,
  resultNote,
  rules = [],
  randomFirst = true,
  className = '',
}) => {
  const { t } = useTranslation();
  const { loading, unavailable, info, reload, patch } = useGameInfo(gameKey);
  const { room, setRoom, clockOffset } = usePvpRoom(t);
  const [ai, setAi] = useState(null);
  const [aiResult, setAiResult] = useState(null);
  const [lobby, setLobby] = useState(null);
  const [stake, setStake] = useState(0);
  const [aiLevel, setAiLevel] = useState(2);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [params, setParams] = useSearchParams();
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const restoredRef = useRef(false);
  const labels = (state) =>
    seatLabels ? seatLabels(state) : [t('先手'), t('后手')];

  // 第一次拿到信息时恢复进行中的房间或人机对局。
  useEffect(() => {
    if (!info || restoredRef.current) return;
    restoredRef.current = true;
    setLobby(info.lobby);
    if (info.lobby?.active) {
      setRoom(info.lobby.active);
    } else if (info.ai?.session) {
      setAi(info.ai.session);
    }
  }, [info, setRoom]);

  const loaded = !!info;
  const inLobby = !room && !ai;
  // 在大厅时定时刷新等待中的房间；别人加入了自己开的房间时直接进入对局。
  useEffect(() => {
    if (!loaded || !inLobby) return undefined;
    const timer = setInterval(async () => {
      const response = await gameGet(`/api/game/rooms?game=${gameKey}`, t);
      if (!response.data) return;
      setLobby(response.data);
      if (response.data.active) setRoom(response.data.active);
    }, LOBBY_POLL_MS);
    return () => clearInterval(timer);
  }, [loaded, inLobby, gameKey, t, setRoom]);

  // 朋友发来的邀请链接带着 invite=邀请码：打开页面后直接加入那个房间。
  const invite = params.get('invite');
  useEffect(() => {
    if (!loaded || !invite || room) return;
    setParams({}, { replace: true });
    gamePost('/api/game/rooms/join', { code: invite }, t).then((response) => {
      if (response.error) {
        showError(response.error);
        return;
      }
      setRoom(response.data);
      patch({ balance: response.data.balance });
    });
  }, [loaded, invite, room, setParams, setRoom, patch, t]);

  const act = useCallback(
    async (url, body, onData) => {
      if (busy) return;
      setBusy(true);
      setError('');
      const response = await gamePost(url, body, t);
      setBusy(false);
      if (response.error) {
        setError(response.error);
        return;
      }
      onData(response.data);
      const balance = response.data?.balance ?? response.data?.result?.balance;
      if (balance !== undefined) patch({ balance });
    },
    [busy, t, patch],
  );

  const backToLobby = async () => {
    setRoom(null);
    setAi(null);
    setAiResult(null);
    setError('');
    const latest = await reload(true);
    if (latest) setLobby(latest.lobby);
  };

  // 人机对局结束时结算结果带着新的领奖进度，信息栏里的剩余次数跟着更新。
  const applyAi = (data) => {
    setAi(data.session);
    if (!data.result) return;
    setAiResult(data.result);
    patch({ ai: { ...info.ai, progress: data.result.progress } });
  };
  const startAi = () =>
    act(
      withTurnstile(`/api/game/${gameKey}/ai/start`, turnstile.take()),
      { level: aiLevel },
      (data) => {
        setAi(data.session);
        setAiResult(null);
      },
    );

  let main = null;
  if (info && room) {
    main = (
      <RoomView
        room={room}
        symbol={info.symbol}
        seatLabels={labels}
        statusText={statusText}
        resultNote={resultNote}
        renderBoard={renderBoard}
        clockOffset={clockOffset}
        busy={busy}
        error={error}
        onMove={(value) =>
          act(
            `/api/game/rooms/${room.id}/move`,
            { version: room.version, move: value },
            setRoom,
          )
        }
        onResign={() =>
          Modal.confirm({
            title: t('认输'),
            content:
              room.stake > 0 ? t('认输后押注归对手。') : t('确定认输吗？'),
            onOk: () => act(`/api/game/rooms/${room.id}/resign`, {}, setRoom),
          })
        }
        onCancel={() => act(`/api/game/rooms/${room.id}/cancel`, {}, setRoom)}
        onCopyInvite={async () => {
          const link = `${window.location.origin}/games/${gameKey}?invite=${room.code}`;
          if (await copy(link)) {
            showSuccess(t('邀请链接已复制，发给朋友即可加入'));
          }
        }}
        onBack={backToLobby}
      />
    );
  } else if (info && ai) {
    main = (
      <AiView
        ai={ai}
        result={aiResult}
        symbol={info.symbol}
        seatLabels={labels}
        statusText={statusText}
        resultNote={resultNote}
        renderBoard={renderBoard}
        busy={busy}
        error={error}
        turnstile={turnstile}
        onMove={(value) =>
          act(
            `/api/game/${gameKey}/ai/move`,
            { session_id: ai.session_id, version: ai.version, move: value },
            applyAi,
          )
        }
        onResign={() =>
          Modal.confirm({
            title: t('认输'),
            content: t('认输后这一局按输了结算。'),
            onOk: () =>
              act(
                `/api/game/${gameKey}/ai/resign`,
                { session_id: ai.session_id },
                applyAi,
              ),
          })
        }
        onRestart={startAi}
        onBack={backToLobby}
      />
    );
  } else if (info) {
    main = (
      <Lobby
        info={info}
        lobby={lobby}
        stake={stake}
        setStake={setStake}
        aiLevel={aiLevel}
        setAiLevel={setAiLevel}
        busy={busy}
        error={error}
        turnstile={turnstile}
        onQuickMatch={() =>
          act('/api/game/rooms/match', { game: gameKey, stake }, setRoom)
        }
        onCreate={(isPrivate) =>
          act(
            '/api/game/rooms',
            { game: gameKey, stake, private: isPrivate },
            setRoom,
          )
        }
        onJoin={(id) => act(`/api/game/rooms/${id}/join`, {}, setRoom)}
        onStartAi={startAi}
      />
    );
  }

  return (
    <GameLayout
      title={title}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      side={
        info && <PvpSide info={info} rules={rules} randomFirst={randomFirst} />
      }
      className={`pvp-page ${className}`}
    >
      {main}
    </GameLayout>
  );
};

export default PvpGame;
