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
import { gameGet, gamePost } from './api';

// 分几步完成的下注游戏(21 点、挖宝、高低牌、火箭)共用的对局状态。
//
// 服务端同一时间只允许一局，每一步都带上最新一局的 session_id 与 version。请求被拒绝或没有得到答复时按编号重新读出这一局，
// 页面总是显示服务端的真实状态。结算这一局的响应(带 net_today)还带着余额与今天的输赢，并进页面信息；settledInfo(previous, round)
// 可以再给出这时要更新的游戏自己的字段。
export function useWagerRound(game, setInfo, reload, settledInfo) {
  const { t } = useTranslation();
  const [round, setRound] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // 按钮回调与长轮询里要读最新的一局，不受渲染时机影响。
  const roundRef = useRef(null);
  const busyRef = useRef(false);
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  // 换上响应里的一局。同一局更旧的样子(版本更低、或已经结束之后又收到还在进行的)是迟到的响应，不用。
  const apply = useCallback(
    (data) => {
      const next = data.round || null;
      const current = roundRef.current;
      if (next && current && next.session_id === current.session_id) {
        const regressed =
          current.status === 'finished' && next.status !== 'finished';
        if (regressed || next.version < current.version) return;
      }
      roundRef.current = next;
      setRound(next);
      if (data.balance === undefined) return;
      setInfo((previous) => {
        if (!previous) return previous;
        const fields = { balance: data.balance };
        if (data.net_today !== undefined && next) {
          fields.net_today = data.net_today;
          fields.plays_today = data.plays_today;
          fields.best_win = Math.max(previous.best_win || 0, next.win || 0);
          Object.assign(fields, settledInfo?.(previous, next));
        }
        return { ...previous, ...fields };
      });
    },
    [setInfo, settledInfo],
  );

  // 重新读出一局：sessionId 为 0 时读进行中的一局。读到的一局已经结束、响应里却没有余额(被别的页面或请求结算了)时，
  // 重新读取页面信息。没有读到时保留页面上的内容。
  const refresh = useCallback(
    async (sessionId = 0) => {
      const query = sessionId ? `?session_id=${sessionId}` : '';
      const response = await gameGet(`/api/game/${game}/round${query}`, t);
      if (!aliveRef.current || !response.data?.round) return;
      apply(response.data);
      if (
        response.data.round.status === 'finished' &&
        response.data.balance === undefined
      ) {
        reload(true);
      }
    },
    [game, t, apply, reload],
  );

  // 发一次请求，同一时间只发一个。continuing 表示继续当前这一局，请求带上它的编号与版本。成功时换上新的一局并返回响应；
  // 出错时显示原因并重新读出这一局(开局出错时看看是不是已经有一局没结束)，没有得到答复时还重新读取页面信息。
  // 重新读完之前按钮保持不可用，免得带着过时的版本再点一次。
  const send = useCallback(
    async (action, body = {}, continuing = true) => {
      const current = roundRef.current;
      if (busyRef.current) return null;
      // 已经结束的一局不能再继续(按钮消失之前的最后一下点击)。
      if (continuing && current?.status !== 'playing') return null;
      busyRef.current = true;
      setBusy(true);
      setError('');
      const payload = continuing
        ? { ...body, session_id: current.session_id, version: current.version }
        : body;
      const response = await gamePost(
        `/api/game/${game}/${action}`,
        payload,
        t,
      );
      if (aliveRef.current && response.data) {
        apply(response.data);
      } else if (aliveRef.current) {
        // 这一局已经被别的响应结束(例如长轮询先拿到了爆炸)，迟到的拒绝不用再提示。
        const latest = roundRef.current;
        const alreadyOver =
          continuing &&
          latest?.session_id === current.session_id &&
          latest.status === 'finished';
        if (!alreadyOver) setError(response.error);
        if (response.network) reload(true);
        await refresh(continuing ? current.session_id : 0);
      }
      busyRef.current = false;
      if (aliveRef.current) setBusy(false);
      return response.data || null;
    },
    [game, t, apply, refresh, reload],
  );

  return { round, busy, error, apply, refresh, send };
}
