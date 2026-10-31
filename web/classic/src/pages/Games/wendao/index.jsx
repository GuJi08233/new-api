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

import React, { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { useGameInfo } from '../common/useGameInfo';
import Dungeon from '../dungeon';
import Sect from '../sect';
import Journey from './journey';
import { WENDAO_PARTS, WendaoContext } from './shell';

const PARTS = { journey: Journey, sect: Sect, abyss: Dungeon };

// 问道：历练、宗门与九幽秘境三个板块共用一份存档，页面顶上切换板块，地址里的 ?part= 记住当前板块。
const Wendao = () => {
  const { t } = useTranslation();
  const { info, setInfo, loading, unavailable, reload } = useGameInfo('wendao');
  const [params, setParams] = useSearchParams();
  const requested = params.get('part');
  const part = PARTS[requested] ? requested : 'journey';
  const [badges, setBadges] = useState({});

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

  const shell = useMemo(
    () => ({
      info,
      setInfo,
      reload,
      part,
      setPart,
      parts: WENDAO_PARTS.filter(({ key }) => PARTS[key]),
      badges,
      setBadges,
    }),
    [info, setInfo, reload, part, setPart, badges],
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
