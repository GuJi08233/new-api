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
import { useNavigate } from 'react-router-dom';
import {
  Banner,
  Button,
  Select,
  Spin,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Gamepad2, LockKeyhole, RefreshCw, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../constants';
import { GAME_ICONS } from './registry';
import GameLayout from './common/GameLayout';
import { gameGet, gamePost } from './common/api';
import { RoomEntryModal } from './common/pvp/PvpGame';
import { useVisiblePolling } from './common/useVisiblePolling';
import './game-lobby.css';

const { Text } = Typography;
const LOBBY_POLL_MS = 5000;

const RoomCard = ({
  room,
  meta,
  symbol,
  active,
  busy,
  balance,
  onJoin,
  onResume,
}) => {
  const { t } = useTranslation();
  const Icon = GAME_ICONS[room.game] || Gamepad2;
  const participating = active || room.seat >= 0;
  const playing = room.status === 'playing';
  return (
    <article className='game-lobby-room'>
      <div className='game-lobby-room-head'>
        <Icon size={20} aria-hidden />
        <strong>{meta.name}</strong>
        <Tag color={playing ? 'green' : 'blue'}>
          {playing ? t('对局中') : t('等待中')}
        </Tag>
        {room.private && <Tag>{t('邀请房')}</Tag>}
        {room.locked && (
          <LockKeyhole size={16} aria-label={t('需要房间密码')} />
        )}
      </div>
      <div className='game-lobby-room-members'>
        {room.members.map((member) => member.name).join(' / ')}
      </div>
      <div className='game-lobby-room-detail'>
        <span>
          <Users size={14} aria-hidden />
          {t('{{occupied}}/{{seats}} 人', {
            occupied: room.player_count,
            seats: room.seats,
          })}
        </span>
        <span>
          {room.stake > 0
            ? t('押注 {{stake}} {{symbol}}', { stake: room.stake, symbol })
            : t('友谊赛')}
        </span>
      </div>
      <Button
        block
        theme='solid'
        disabled={
          busy || (!participating && (playing || room.escrow > balance))
        }
        onClick={() => (participating ? onResume(room) : onJoin(room))}
      >
        {participating ? t('返回对局') : playing ? t('对局中') : t('加入房间')}
      </Button>
    </article>
  );
};

const GameLobby = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [self, setSelf] = useState(null);
  const [snapshot, setSnapshot] = useState(null);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [error, setError] = useState('');
  const [entryError, setEntryError] = useState('');
  const [entry, setEntry] = useState(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const loadedRef = useRef(false);
  const requestRef = useRef(null);

  const load = useCallback(
    async (quiet = false) => {
      if (busyRef.current || (quiet && requestRef.current)) return;
      requestRef.current?.abort();
      const controller = new AbortController();
      requestRef.current = controller;
      if (!quiet) setLoading(true);
      const query = filter ? `?game=${encodeURIComponent(filter)}` : '';
      const [account, lobby] = await Promise.all([
        gameGet('/api/game/self', t, { signal: controller.signal }),
        gameGet(`/api/game/lobby${query}`, t, { signal: controller.signal }),
      ]);
      // 筛选变化、离开页面或加入房间会取消旧请求，晚到的旧筛选响应不能覆盖当前列表。
      if (requestRef.current !== controller) return;
      requestRef.current = null;
      setLoading(false);
      if (account.data) {
        loadedRef.current = true;
        setSelf(account.data);
        setUnavailable('');
      } else if (!account.network || !loadedRef.current) {
        setUnavailable(account.error);
      }
      if (lobby.data) {
        setSnapshot({ filter, data: lobby.data });
        setError(account.network ? account.error : '');
      } else {
        setError(lobby.error);
      }
    },
    [filter, t],
  );

  useEffect(() => {
    load();
    return () => {
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [load]);

  const poll = useCallback(() => load(true), [load]);
  useVisiblePolling(poll, LOBBY_POLL_MS, !!self);

  const metaMap = getGameMetaMap(t);
  const balance = self?.account?.balance || 0;
  const symbol = self?.token?.symbol || '';
  const games = (self?.games || []).filter(
    (game) => game.kind === 'pvp' && metaMap[game.key],
  );
  const rooms = (snapshot?.filter === filter ? snapshot.data.rooms : []).filter(
    (room) => metaMap[room.game] && room.seat < 0 && !room.own,
  );
  const activeRooms = (snapshot?.data.active_rooms || []).filter(
    (room) => metaMap[room.game],
  );
  const resume = (room) => navigate(`/games/${room.game}`);
  const openEntry = (room) => {
    setEntryError('');
    setEntry({ ...room, type: 'join' });
  };
  const join = async (password) => {
    if (!entry || busyRef.current) return;
    busyRef.current = true;
    requestRef.current?.abort();
    requestRef.current = null;
    setLoading(false);
    setBusy(true);
    setEntryError('');
    try {
      const response = await gamePost(
        `/api/game/rooms/${entry.id}/join`,
        { password },
        t,
      );
      if (response.error) {
        if (response.network) {
          // 加入可能已经扣币入座，只是响应丢失；先读取自己的席位，确认后恢复对局，避免再次提交。
          const recovery = await gameGet('/api/game/lobby', t);
          const joined = recovery.data?.active_rooms.find(
            (room) => room.id === entry.id,
          );
          if (joined) {
            navigate(`/games/${joined.game}`);
            return;
          }
        }
        setEntryError(response.error);
        return;
      }
      navigate(`/games/${response.data.game}`);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  return (
    <GameLayout
      title={t('联机大厅')}
      loading={loading && !self}
      unavailable={unavailable}
      balance={balance}
      symbol={symbol}
      className='game-lobby-page'
    >
      <div className='game-lobby-intro'>
        <Text type='tertiary'>
          {t('在同一个大厅选择不同游戏，找人同桌或回到进行中的对局')}
        </Text>
        <Button
          icon={<RefreshCw size={14} />}
          disabled={loading || busy}
          onClick={() => load()}
        >
          {t('刷新')}
        </Button>
      </div>
      {self?.closed && (
        <Banner
          type='warning'
          closeIcon={null}
          description={t(
            '游戏中心已关闭，不能再开新局。你还有押着游戏币、没结束的对局，请回去把它玩完',
          )}
        />
      )}
      {activeRooms.length > 0 && (
        <section className='game-lobby-section'>
          <h2>{t('我的房间')}</h2>
          <div className='game-lobby-grid'>
            {activeRooms.map((room) => (
              <RoomCard
                key={room.id}
                room={room}
                meta={metaMap[room.game]}
                symbol={symbol}
                active
                busy={busy}
                balance={balance}
                onJoin={openEntry}
                onResume={resume}
              />
            ))}
          </div>
        </section>
      )}
      <section className='game-lobby-section'>
        <div className='game-lobby-filter'>
          <h2>{t('可加入的房间')}</h2>
          <Select
            aria-label={t('选择游戏')}
            value={filter}
            style={{ minWidth: 170 }}
            optionList={[
              { label: t('全部游戏'), value: '' },
              ...games.map((game) => ({
                label: metaMap[game.key].name,
                value: game.key,
              })),
            ]}
            onChange={setFilter}
          />
        </div>
        {error && !entry && <Text type='danger'>{error}</Text>}
        <Spin spinning={loading}>
          {rooms.length > 0 ? (
            <div className='game-lobby-grid'>
              {rooms.map((room) => (
                <RoomCard
                  key={room.id}
                  room={room}
                  meta={metaMap[room.game]}
                  symbol={symbol}
                  busy={busy}
                  balance={balance}
                  onJoin={openEntry}
                  onResume={resume}
                />
              ))}
            </div>
          ) : (
            <div className='game-lobby-empty'>
              {self?.closed
                ? t('游戏中心已关闭，仅可返回已有对局')
                : t('暂无可加入的房间，进入游戏后可以开房或快速匹配')}
            </div>
          )}
        </Spin>
      </section>
      {entry && (
        <RoomEntryModal
          key={entry.id}
          entry={entry}
          gameKey={entry.game}
          title={metaMap[entry.game].name}
          symbol={symbol}
          balance={balance}
          busy={busy}
          error={entryError}
          onSubmit={join}
          onClose={() => {
            if (busyRef.current) return;
            setEntry(null);
            setEntryError('');
          }}
        />
      )}
    </GameLayout>
  );
};

export default GameLobby;
