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

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ElementTag, realmLabel, useSectText } from '../../sect/parts';

// readyTeam 是开战时真正带得上的弟子：选了以后又外出或受伤的去掉。
export const readyTeam = (team, companions) =>
  team.filter((id) => companions.some((d) => d.id === id && d.ready));

// 随行弟子：最多 max 名，各带一张援护牌；外出或受伤的不能选。地图节点与问心阶共用。
const TeamPicker = ({ companions, max, team, onChange }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const toggle = (id) => {
    if (team.includes(id)) {
      onChange(team.filter((member) => member !== id));
      return;
    }
    if (team.length < max) onChange([...team, id]);
  };
  return (
    <div className='jn-team'>
      <span className='jn-reward-label'>
        {t('随行弟子（最多 {{n}} 名，各带一张援护牌）', { n: max })}
      </span>
      {companions.length === 0 && (
        <span className='jn-muted'>{t('宗门里还没有弟子')}</span>
      )}
      {companions.map((d) => {
        const picked = team.includes(d.id);
        return (
          <button
            type='button'
            key={d.id}
            className={picked ? 'jn-member is-picked' : 'jn-member'}
            disabled={!d.ready}
            aria-pressed={picked}
            onClick={() => toggle(d.id)}
          >
            <ElementTag element={d.element} />
            <strong>{sectText.name(d.name)}</strong>
            <span>
              {d.ready
                ? realmLabel(sectText, t, d.realm, d.layer)
                : t('外出或受伤')}
            </span>
          </button>
        );
      })}
    </div>
  );
};

export default TeamPicker;
