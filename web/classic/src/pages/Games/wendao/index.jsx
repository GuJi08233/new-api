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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { gameGet } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { useVisiblePolling } from '../common/useVisiblePolling';
import Abyss from './doufa/Abyss';
import Sect from '../sect';
import Journey from './journey';
import World from './world/World';
import { WENDAO_PARTS, WendaoContext } from './shell';

const PARTS = { journey: Journey, world: World, sect: Sect, abyss: Abyss };

// 顶上的待办每分钟与服务端同步一次；操作之后等这么久再读，连着几步操作只读一次。
const AGENDA_INTERVAL = 60 * 1000;
const AGENDA_SETTLE = 800;

// 问道：历练、世界、宗门与九幽秘境四个板块共用一份存档，页面顶上切换板块，地址里的 ?part= 记住当前板块。
const Wendao = () => {
  const { t } = useTranslation();
  const { info, setInfo, loading, unavailable, reload } = useGameInfo('wendao');
  const [params, setParams] = useSearchParams();
  const requested = params.get('part');
  const part = PARTS[requested] ? requested : 'journey';

  const setPart = useCallback(
    (next) => {
      setParams(
        (previous) => {
          const updated = new URLSearchParams(previous);
          updated.set('part', next);
          return updated;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  // 当前目标与待办：打开页面、切换板块时读一次，之后每分钟一次，板块里操作成功后再读一次。
  const [agenda, setAgenda] = useState(null);
  const [focus, setFocus] = useState(null);
  const settleTimer = useRef(null);
  const loaded = !!info;
  // 只采用最后发出的那一次读取：定时读取与操作后的读取前后脚发出时，先发后到的旧结果不盖掉新的。
  const agendaSeq = useRef(0);
  const loadAgenda = useCallback(async () => {
    agendaSeq.current += 1;
    const mine = agendaSeq.current;
    const response = await gameGet('/api/game/wendao/agenda', t);
    if (mine === agendaSeq.current && response.data) setAgenda(response.data);
  }, [t]);
  const refreshAgenda = useCallback(() => {
    clearTimeout(settleTimer.current);
    settleTimer.current = setTimeout(loadAgenda, AGENDA_SETTLE);
  }, [loadAgenda]);
  useEffect(() => () => clearTimeout(settleTimer.current), []);
  useEffect(() => {
    if (loaded) loadAgenda();
  }, [loaded, part, loadAgenda]);
  useVisiblePolling(loadAgenda, AGENDA_INTERVAL, loaded);

  // 点了目标或待办：切到那个板块，板块页面按 focus 打开页签。自己点板块按钮时清掉 focus，回到板块不会又跳到上次的页签。
  const go = useCallback(
    (target) => {
      setFocus({ ...target, seq: Date.now() });
      setPart(target.part);
    },
    [setPart],
  );
  const choosePart = useCallback(
    (key) => {
      setFocus(null);
      setPart(key);
    },
    [setPart],
  );

  const shell = useMemo(
    () => ({
      info,
      setInfo,
      reload,
      part,
      setPart: choosePart,
      parts: WENDAO_PARTS.filter(({ key }) => PARTS[key]),
      agenda,
      refreshAgenda,
      focus,
      go,
    }),
    [info, setInfo, reload, part, choosePart, agenda, refreshAgenda, focus, go],
  );

  if (loading || unavailable || !info) {
    return (
      <GameLayout
        title={t('问道')}
        loading={loading}
        unavailable={unavailable}
      />
    );
  }
  const Part = PARTS[part];
  return (
    <WendaoContext.Provider value={shell}>
      <Part key={part} />
    </WendaoContext.Provider>
  );
};

export default Wendao;
