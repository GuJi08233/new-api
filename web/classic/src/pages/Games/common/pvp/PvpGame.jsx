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
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Button,
  Input,
  InputNumber,
  Modal,
  Typography,
} from '@douyinfe/semi-ui';
import {
  Bot,
  Copy,
  Flag,
  LockKeyhole,
  Swords,
  UserRound,
  Users,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { copy, showSuccess, timestamp2string } from '../../../../helpers';
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
import { PlayTurnstile, usePlayTurnstile } from '../usePlayTurnstile';
import { useGameInfo } from '../useGameInfo';
import { useVisiblePolling } from '../useVisiblePolling';
import { isRoomOpen, usePvpRoom } from './usePvpRoom';
import './pvp.css';

const { Text } = Typography;

// 大厅每隔这么久刷新一次等待中的房间。
const LOBBY_POLL_MS = 5000;
// 押注的快捷档位，超过押注上限的不显示；0 是友谊赛。
const STAKE_CHIPS = [0, 1, 5, 10, 50, 100, 500];

// 对局成员：座位称呼(执黑、执白……)、名字，轮到的一方高亮，extra 是每个座位后面的附加内容(例如计时)。
const Players = ({ names, labels, toMove, seat, extra }) => (
  <div className='pvp-players'>
    {names.map((name, index) => (
      <div
        key={index}
        className={`pvp-player ${toMove === index || toMove === -1 ? 'is-turn' : ''} ${index === seat ? 'is-me' : ''}`}
      >
        <span className='pvp-player-seat'>{labels[index]}</span>
        <span className='pvp-player-name'>{name}</span>
        {extra?.[index]}
      </div>
    ))}
  </div>
);

export const RoomEntryModal = ({
  entry,
  gameKey,
  title,
  stake,
  symbol,
  balance,
  busy,
  error,
  onSubmit,
  onClose,
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [preview, setPreview] = useState(null);
  const [previewRequest, setPreviewRequest] = useState(0);
  const code = entry.code;
  const creating = entry.type === 'create';
  const tooLong = new TextEncoder().encode(password).length > 72;

  // 邀请链接先读房间概要，看到实扣额后再由玩家确认加入；查询本身不会入座或扣币。
  useEffect(() => {
    if (!code) return undefined;
    let alive = true;
    const controller = new AbortController();
    gameGet(`/api/game/rooms/invite?code=${encodeURIComponent(code)}`, t, {
      signal: controller.signal,
    }).then((response) => {
      if (!alive) return;
      if (response.data && response.data.game !== gameKey) {
        navigate(
          `/games/${encodeURIComponent(response.data.game)}?invite=${encodeURIComponent(code)}`,
          { replace: true },
        );
        return;
      }
      setPreview(response);
    });
    return () => {
      alive = false;
      controller.abort();
    };
  }, [code, gameKey, t, navigate, previewRequest]);

  const details = code ? preview?.data : entry;
  const displayedStake = creating ? stake : details?.stake;
  const escrow = creating ? stake : (details?.escrow ?? displayedStake);
  const unavailable =
    !creating && details?.status && details.status !== 'waiting';
  const insufficient = escrow > balance;
  const canSubmit =
    !busy && !tooLong && !insufficient && !unavailable && (!code || !!details);
  return (
    <Modal
      visible
      centered
      width={448}
      height='auto'
      className='pvp-entry-modal'
      style={{ maxWidth: 'calc(100vw - 32px)' }}
      title={creating ? t('开一个房间') : t('加入房间')}
      okText={creating ? t('创建') : t('加入')}
      confirmLoading={busy}
      okButtonProps={{ disabled: !canSubmit }}
      cancelButtonProps={{ disabled: busy }}
      closable={!busy}
      maskClosable={!busy}
      closeOnEsc={!busy}
      onCancel={onClose}
      onOk={() => canSubmit && onSubmit(password)}
    >
      <div className='pvp-entry'>
        <Text strong>{title}</Text>
        {code && !preview && <Text type='tertiary'>{t('加载中...')}</Text>}
        {preview?.error && (
          <>
            <Text type='danger'>{preview.error}</Text>
            <Button
              onClick={() => {
                setPreview(null);
                setPreviewRequest((current) => current + 1);
              }}
            >
              {t('刷新')}
            </Button>
          </>
        )}
        {displayedStake !== undefined && (
          <Text>
            {displayedStake > 0
              ? t('押注 {{stake}} {{symbol}}', {
                  stake: displayedStake,
                  symbol,
                })
              : t('友谊赛')}
          </Text>
        )}
        {!creating && details && (
          <Text type='tertiary'>
            {t('{{occupied}}/{{seats}} 人', {
              occupied: details.player_count,
              seats: details.seats,
            })}
          </Text>
        )}
        {!creating && escrow > 0 && (
          <Text>
            {t('加入时将扣除 {{amount}} {{symbol}}', {
              amount: escrow,
              symbol,
            })}
          </Text>
        )}
        {insufficient && <Text type='danger'>{t('游戏币余额不足')}</Text>}
        {unavailable && (
          <Text type='danger'>{t('房间已开始或关闭，不能加入')}</Text>
        )}
        {creating && entry.private && (
          <Text type='tertiary'>{t('仅通过邀请链接加入，不在大厅展示')}</Text>
        )}
        {(creating || details?.locked) && (
          <>
            <label htmlFor='pvp-room-password'>{t('房间密码')}</label>
            <Input
              id='pvp-room-password'
              mode='password'
              autoComplete='off'
              value={password}
              maxLength={72}
              disabled={busy}
              placeholder={
                creating ? t('可选，留空则无需密码') : t('如有房间密码，请输入')
              }
              onChange={setPassword}
              onEnterPress={() => canSubmit && onSubmit(password)}
            />
          </>
        )}
        {tooLong && (
          <Text type='danger'>{t('房间密码过长，请缩短后重试')}</Text>
        )}
        {error && <Text type='danger'>{error}</Text>}
      </div>
    </Modal>
  );
};

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
        {rule.seats > 2 && (
          <Text type='tertiary'>
            {t('{{seats}} 人桌，所有座位坐满后自动开局', { seats: rule.seats })}
          </Text>
        )}
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
                  {item.locked && (
                    <span title={t('需要房间密码')}>
                      <LockKeyhole size={14} aria-label={t('需要房间密码')} />
                    </span>
                  )}
                  <span className='pvp-room-count'>
                    {t('{{occupied}}/{{seats}} 人', {
                      occupied: item.player_count,
                      seats: item.seats,
                    })}
                  </span>
                  <span className='pvp-room-stake'>
                    {item.stake > 0 ? `${item.stake} ${symbol}` : t('友谊赛')}
                  </span>
                  <Button
                    size='small'
                    theme='solid'
                    disabled={busy || item.own || item.stake > balance}
                    onClick={() => onJoin(item)}
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
          <PlayTurnstile turnstile={turnstile} />
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
  onLeave,
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
  // 服务器时间只精确到秒，刚走完一步时算出来可能比每步限时多一秒。
  const secondsLeft = room.deadline
    ? Math.min(
        Math.max(Math.ceil(room.deadline - (now + clockOffset())), 0),
        room.move_seconds || Infinity,
      )
    : 0;
  const myTurn =
    playing &&
    (!room.deadline || secondsLeft > 0) &&
    (room.to_move === seat || room.to_move === -1);
  const members = room.members;
  const players = Array.from({ length: room.seats }, (_, index) =>
    members.find((member) => member.seat === index),
  );
  const me = players[seat];
  const pot = room.escrow * room.player_count;
  const labels = seatLabels(state);

  let message;
  let kind = '';
  let note = '';
  if (room.status === 'waiting') {
    message = room.private
      ? room.seats > 2
        ? t('把邀请链接发给朋友，等所有座位坐满')
        : t('把邀请链接发给朋友，等对方加入')
      : t('等待其他玩家加入…');
  } else if (room.status === 'cancelled') {
    message =
      room.end_reason === 'expired'
        ? t('等了太久没人加入，房间已关闭，押注已退回')
        : t('房间已关闭，押注已退回');
  } else if (room.status === 'finished') {
    const won = me?.result === 'win';
    if (me?.result === 'draw') {
      message = room.stake > 0 ? t('平局，押注已退回') : t('平局');
    } else if (won) {
      kind = room.stake > 0 ? 'big-win' : 'win';
      message =
        room.stake > 0
          ? t('你赢了！获得 {{win}} {{symbol}}', {
              win: room.escrow + me.net,
              symbol,
            })
          : t('你赢了！');
    } else {
      kind = 'loss';
      message = t('你输了');
    }
    if (room.end_reason === 'timeout') {
      note =
        room.seats > 2
          ? t('本局因超时结束')
          : won
            ? t('对手超时')
            : t('你超时了');
    } else if (room.end_reason === 'resign') {
      note =
        room.seats > 2
          ? t('本局因认输结束')
          : won
            ? t('对手认输')
            : t('你认输了');
    } else {
      note = resultNote?.({ state, seat }) || '';
    }
  } else {
    message =
      statusText?.({ room, state, seat, myTurn }) ||
      (myTurn ? t('轮到你了') : t('等待对手走棋'));
  }

  const names = players.map((player, index) => {
    if (!player) return t('等待加入…');
    return index === seat ? t('我') : player.name;
  });
  const clocks = players.map((_, index) =>
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
        {' · '}
        {t('{{occupied}}/{{seats}} 人', {
          occupied: room.player_count,
          seats: room.seats,
        })}
      </div>
      <Players
        names={names}
        labels={players.map(
          (_, index) =>
            labels[index] || t('座位 {{seat}}', { seat: index + 1 }),
        )}
        toMove={playing ? room.to_move : null}
        seat={seat}
        extra={clocks}
      />
      {renderBoard({
        roomId: room.id,
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
            <Button
              type='danger'
              disabled={busy}
              onClick={seat === 0 ? onCancel : onLeave}
            >
              {seat === 0 ? t('取消房间') : t('离开房间')}
            </Button>
          </>
        )}
        {playing && (
          <Button icon={<Flag size={14} />} disabled={busy} onClick={onResign}>
            {t('认输')}
          </Button>
        )}
        {!isRoomOpen(room) && (
          <button
            type='button'
            className='games-play-button'
            disabled={busy}
            onClick={onBack}
          >
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
            {error && (
              <Button disabled={busy} onClick={onBack}>
                {t('返回大厅')}
              </Button>
            )}
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
            <Button disabled={busy} onClick={onBack}>
              {t('返回大厅')}
            </Button>
          </>
        )}
      </div>
      {ai.over && <PlayTurnstile turnstile={turnstile} />}
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
              ? rule.seats > 2
                ? t(
                    '每位玩家押同样多的游戏币，获胜玩家分配输方押注，平台从净赢额抽取 {{rake}}%（向下取整）；平局退回押注',
                    { rake: rule.rake_percent },
                  )
                : t(
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
                <span className='pvp-recent-opponent'>
                  {item.members
                    .filter((member) => member.seat !== item.seat)
                    .map((member) => member.name)
                    .join(' / ')}
                </span>
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
// 认输，这时引擎的状态不一定是结束的)；玩家落子时调用 onMove(走法)。seatLabels(state) 返回各座位的称呼(例如执黑、执白)；
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
  teamGame = false,
  className = '',
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { loading, unavailable, info, reload, patch } = useGameInfo(gameKey);
  const { room, setRoom, clockOffset } = usePvpRoom(t);
  const [ai, setAi] = useState(null);
  const [aiResult, setAiResult] = useState(null);
  const [lobby, setLobby] = useState(null);
  const [stake, setStake] = useState(0);
  const [aiLevel, setAiLevel] = useState(2);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [entry, setEntry] = useState(null);
  const [params, setParams] = useSearchParams();
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const restoredRef = useRef(false);
  const busyRef = useRef(false);
  const lobbyEpochRef = useRef(0);
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

  // 房间结束或取消后，获胜奖励与退回的押注已经入账；操作响应和长轮询都只带局面，另外刷新余额与最近对局。
  const roomId = room?.id;
  const roomEnded = !!room && !isRoomOpen(room);
  useEffect(() => {
    if (!roomEnded) return undefined;
    let alive = true;
    gameGet(`/api/game/${gameKey}`, t).then((response) => {
      if (!alive || !response.data) return;
      patch({ balance: response.data.balance, recent: response.data.recent });
    });
    return () => {
      alive = false;
    };
  }, [gameKey, roomId, roomEnded, t, patch]);

  const loaded = !!info;
  const inLobby = !room && !ai;
  // 在大厅时定时刷新等待中的房间，页面在后台时不刷新；别人加入了自己开的房间时直接进入对局。
  const pollLobby = useCallback(async () => {
    if (busyRef.current) return;
    const epoch = lobbyEpochRef.current;
    const response = await gameGet(`/api/game/rooms?game=${gameKey}`, t);
    if (!response.data || epoch !== lobbyEpochRef.current) return;
    setLobby(response.data);
    if (response.data.active) setRoom(response.data.active);
  }, [gameKey, t, setRoom]);
  useVisiblePolling(pollLobby, LOBBY_POLL_MS, loaded && inLobby);

  // 邀请保留到成功加入或主动取消，密码错误、网络中断与恢复已有对局都不会丢失邀请码。
  const invite = params.get('invite');
  const roomEntry =
    loaded && inLobby
      ? entry || (invite ? { type: 'join', code: invite } : null)
      : null;
  const closeEntry = () => {
    if (busyRef.current) return;
    setEntry(null);
    setError('');
    if (!invite) return;
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete('invite');
        return next;
      },
      { replace: true },
    );
  };

  const act = useCallback(
    async (url, body, onData, recovery = '') => {
      if (busyRef.current) return;
      busyRef.current = true;
      lobbyEpochRef.current += 1;
      setBusy(true);
      setError('');
      try {
        let response = await gamePost(url, body, t);
        if (response.error) {
          setError(response.error);
          if (recovery === 'room') {
            // 离座可能已成功但响应丢失；先确认自己仍在哪个房间，避免一直轮询已失去席位的旧房间。
            const latest = await reload(true);
            if (!latest) return;
            const active = latest.lobby?.active || null;
            setRoom(active);
            setLobby(latest.lobby);
            setAi(active ? null : latest.ai?.session || null);
            setAiResult(null);
            if (!active) setError('');
            return;
          }
          if (recovery !== 'ai') return;
          // 落子可能已经执行，只是响应丢失；版本冲突也要恢复，不能让下一步一直带着旧版本重试。
          // 恢复失败时保留原局，下一次操作失败后仍可重新读取；恢复结束前始终持有 busy 锁。
          const query = body.session_id ? `?session_id=${body.session_id}` : '';
          const current = await gameGet(`/api/game/${gameKey}/ai${query}`, t);
          if (!current.data) return;
          setAiResult(null);
          if (!current.data.session) {
            setAi(null);
            const latest = await reload(true);
            if (latest) setLobby(latest.lobby);
            return;
          }
          // 指定编号时也能读回已经结算的局，和原操作成功一样显示最终局面、胜负与奖励。
          response = current;
          if (current.data.result) setError('');
        }
        const balance =
          response.data?.balance ?? response.data?.result?.balance;
        if (balance !== undefined) patch({ balance });
        await onData(response.data);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [gameKey, t, patch, reload, setRoom],
  );

  const backToLobby = async () => {
    lobbyEpochRef.current += 1;
    setRoom(null);
    setAi(null);
    setAiResult(null);
    setError('');
    const latest = await reload(true);
    if (latest) setLobby(latest.lobby);
  };

  const submitEntry = (password) => {
    if (!roomEntry) return;
    const creating = roomEntry.type === 'create';
    const url = creating
      ? '/api/game/rooms'
      : roomEntry.code
        ? '/api/game/rooms/join'
        : `/api/game/rooms/${roomEntry.id}/join`;
    const body = creating
      ? {
          game: gameKey,
          stake,
          seats: info.rule.seats,
          private: roomEntry.private,
          password,
        }
      : { code: roomEntry.code, password };
    return act(url, body, (data) => {
      setRoom(data);
      setEntry(null);
      if (roomEntry.code) {
        setParams(
          (current) => {
            const next = new URLSearchParams(current);
            next.delete('invite');
            return next;
          },
          { replace: true },
        );
      }
    });
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
      'ai',
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
            content: teamGame
              ? t('认输会使你的队伍判负，押注按本局规则结算。')
              : room.stake > 0
                ? t('认输后押注归对手。')
                : t('确定认输吗？'),
            onOk: () => act(`/api/game/rooms/${room.id}/resign`, {}, setRoom),
          })
        }
        onCancel={() => act(`/api/game/rooms/${room.id}/cancel`, {}, setRoom)}
        onLeave={() =>
          act(`/api/game/rooms/${room.id}/leave`, {}, backToLobby, 'room')
        }
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
            'ai',
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
                'ai',
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
          act(
            '/api/game/rooms/match',
            { game: gameKey, stake, seats: info.rule.seats },
            setRoom,
          )
        }
        onCreate={(isPrivate) => {
          setError('');
          setEntry({ type: 'create', private: isPrivate });
        }}
        onJoin={(item) => {
          if (!item.locked) {
            return act(`/api/game/rooms/${item.id}/join`, {}, setRoom);
          }
          setError('');
          setEntry({ ...item, type: 'join' });
        }}
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
      {info && (
        <div className='flex justify-end mb-3'>
          <Button
            icon={<Users size={14} />}
            disabled={busy}
            onClick={() => navigate('/games/lobby')}
          >
            {t('联机大厅')}
          </Button>
        </div>
      )}
      {main}
      {roomEntry && (
        <RoomEntryModal
          key={`${roomEntry.type}:${roomEntry.id || roomEntry.code || ''}`}
          entry={roomEntry}
          gameKey={gameKey}
          title={title}
          stake={roomEntry.type === 'create' ? stake : roomEntry.stake}
          symbol={info.symbol}
          balance={info.balance || 0}
          busy={busy}
          error={error}
          onSubmit={submitEntry}
          onClose={closeEntry}
        />
      )}
    </GameLayout>
  );
};

export default PvpGame;
