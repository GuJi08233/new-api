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

import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { gameGet, gamePost, withTurnstile } from './api';
import { useGameInfo } from './useGameInfo';
import { usePlayTurnstile } from './usePlayTurnstile';

// 按步保存的解谜游戏(熄灯、汉诺塔、数织、24 点、猜数字)共用的对局状态。每一步都由服务端保存并带着版本号，刷新页面与
// 响应丢失后从同一局恢复；同一局更旧的响应不覆盖新的局面。
//
// onStep(previous, next) 在换上新的局面之前调用：页面据此配音效、在同一局刚结束时评价与庆祝。previous 是上一个局面，
// 换了一局或刚打开页面时与 next 不是同一局。
export function useStepPuzzle(gameKey, { onStep } = {}) {
  const { t } = useTranslation();
  const { info, loading, unavailable, patch } = useGameInfo(gameKey);
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [round, setRound] = useState(null);
  const [difficulty, setDifficulty] = useState(1);
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const [error, setError] = useState('');
  const busyRef = useRef(false);
  const aliveRef = useRef(true);
  const currentRef = useRef(null);
  const onStepRef = useRef(onStep);
  onStepRef.current = onStep;
  // 在这个页面上从第 0 步看到的对局才知道开局时刻，用来记本机最短用时；刷新到一半的对局不计时。
  const startRef = useRef({ session: 0, at: 0 });

  const accept = useCallback(
    (next) => {
      if (!aliveRef.current) return;
      const current = currentRef.current;
      if (
        current &&
        next &&
        current.session_id === next.session_id &&
        current.version > next.version
      )
        return;
      if (
        next?.status === 'playing' &&
        next.moves === 0 &&
        startRef.current.session !== next.session_id
      )
        startRef.current = { session: next.session_id, at: Date.now() };
      onStepRef.current?.(current, next);
      currentRef.current = next;
      setRound(next);
      if (next) setDifficulty(next.difficulty);
      if (next?.result) {
        const result = next.result;
        patch({
          balance: result.balance,
          progress: result.progress,
          best_score: result.best_score,
        });
      }
    },
    [patch],
  );

  const restore = useCallback(
    async (id = 0) => {
      const response = await gameGet(
        `/api/game/${gameKey}/state${id ? `?session_id=${id}` : ''}`,
        t,
      );
      if (!aliveRef.current) return;
      if (response.data) {
        accept(response.data.session);
        setError('');
      } else setError(response.error);
      setChecked(true);
    },
    [gameKey, accept, t],
  );

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  const loaded = !!info;
  useEffect(() => {
    if (loaded) restore();
  }, [loaded, restore]);

  // 开局(action 为 start)或在当前一局里做一步。失败时重新读出这一局，再显示失败原因；返回这一步是否成功。
  const send = async (action, fields = {}) => {
    if (busyRef.current || (action === 'start' && !turnstile.ready)) {
      return false;
    }
    const current = currentRef.current;
    if (action !== 'start' && current?.status !== 'playing') return false;
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const starting = action === 'start';
      const response = await gamePost(
        starting
          ? withTurnstile(`/api/game/${gameKey}/start`, turnstile.take())
          : `/api/game/${gameKey}/action`,
        starting
          ? { difficulty }
          : {
              ...fields,
              action,
              session_id: current.session_id,
              version: current.version,
            },
        t,
      );
      if (!aliveRef.current) return false;
      if (response.data) {
        // 重交已结束的一局时公共结算接口直接返回 result。
        if (response.data.progress)
          accept({ ...current, status: 'finished', result: response.data });
        else accept(response.data);
        return true;
      }
      await restore(starting ? 0 : current.session_id);
      if (aliveRef.current) setError(response.error);
      return false;
    } finally {
      busyRef.current = false;
      if (aliveRef.current) setBusy(false);
    }
  };

  // 这一局从开局到现在的秒数；不是从开局看着玩的对局返回 -1。
  const elapsedSeconds = (sessionId) =>
    startRef.current.session === sessionId
      ? Math.max(1, Math.round((Date.now() - startRef.current.at) / 1000))
      : -1;

  return {
    info,
    loading: loading || (loaded && !checked),
    unavailable,
    turnstile,
    round,
    playing: round?.status === 'playing',
    difficulty,
    setDifficulty,
    busy,
    checked,
    error,
    send,
    restore,
    elapsedSeconds,
  };
}
