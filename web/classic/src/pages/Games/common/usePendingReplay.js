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

import {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../../../context/User';
import { API } from '../../../helpers';
import { createReplayStore } from './gameStorage';
import { submitPendingReplay } from './usePendingReplayRequest';

// snapshot 只在结束、切后台与离页时调用；不在动画帧里写盘。恢复仅提交录像，不自动开新局。
export function usePendingReplay(game, { snapshot, onSettled } = {}) {
  const { t } = useTranslation();
  const [userState] = useContext(UserContext);
  const userId = userState?.user?.id;
  const store = useMemo(
    () => createReplayStore(() => localStorage, userId, game),
    [userId, game],
  );
  const [pending, setPending] = useState([]);
  const [storageWarning, setStorageWarning] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const activeRef = useRef(null);
  const snapshotRef = useRef(snapshot);
  const settledRef = useRef(onSettled);
  const sendingRef = useRef(false);
  const identityRef = useRef(userId);
  identityRef.current = userId;
  snapshotRef.current = snapshot;
  settledRef.current = onSettled;

  const refresh = useCallback(() => {
    const loaded = store.list();
    if (!loaded.ok) setStorageWarning(true);
    setPending(
      loaded.value.filter(
        (record) =>
          record.sessionId !== activeRef.current?.sessionId ||
          activeRef.current?.store !== store,
      ),
    );
    return loaded.value;
  }, [store]);

  useEffect(() => {
    activeRef.current = null;
    setStorageWarning(false);
    refresh();
    const changed = () => refresh();
    window.addEventListener('storage', changed);
    return () => window.removeEventListener('storage', changed);
  }, [refresh]);

  const beginSession = useCallback(
    (sessionId) => {
      activeRef.current = { sessionId, startedAt: Date.now(), store };
      setMessage('');
    },
    [store],
  );

  const save = useCallback(
    (payload) => {
      const active = activeRef.current;
      if (
        !payload ||
        active?.store !== store ||
        active.sessionId !== payload.session_id
      )
        return;
      if (!store.save(payload, active.startedAt).ok) setStorageWarning(true);
    },
    [store],
  );

  const send = useCallback(
    async (payload) => {
      if (!userId || identityRef.current !== userId)
        return { error: t('登录已过期，请重新登录'), network: true };
      save(payload);
      const sessionId = payload.session_id;
      const response = await submitPendingReplay(
        store,
        game,
        payload,
        API.post.bind(API),
      );
      if (!response.data && !response.error)
        response.error = t('网络异常，结算没有完成，可以重新提交');
      if (!response.network && !response.limited) {
        if (response.storageFailure) setStorageWarning(true);
        if (
          activeRef.current?.store === store &&
          activeRef.current.sessionId === sessionId
        )
          activeRef.current = null;
        refresh();
      }
      return response;
    },
    [game, save, store, refresh, t, userId],
  );

  const retry = useCallback(
    async (record) => {
      if (
        sendingRef.current ||
        record.userId !== userId ||
        record.game !== game
      )
        return;
      sendingRef.current = true;
      setBusy(true);
      const response = await send(record.payload);
      setMessage(response.error || t('上次对局已结算，奖励和余额已更新'));
      if (response.data) settledRef.current?.(response.data);
      sendingRef.current = false;
      setBusy(false);
    },
    [send, t, userId, game],
  );

  const hasPending = useCallback(() => store.list().value.length > 0, [store]);
  const release = useCallback(() => {
    save(snapshotRef.current?.());
    activeRef.current = null;
    refresh();
  }, [save, refresh]);

  useEffect(() => {
    const checkpoint = () => save(snapshotRef.current?.());
    const visibility = () => {
      if (document.hidden) checkpoint();
    };
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('pagehide', checkpoint);
    window.addEventListener('beforeunload', checkpoint);
    return () => {
      checkpoint();
      document.removeEventListener('visibilitychange', visibility);
      window.removeEventListener('pagehide', checkpoint);
      window.removeEventListener('beforeunload', checkpoint);
    };
  }, [save]);

  return {
    pending,
    storageWarning,
    busy,
    message,
    save,
    send,
    retry,
    beginSession,
    hasPending,
    release,
  };
}
