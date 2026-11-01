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

import React, { useState } from 'react';
import { Button, InputNumber } from '@douyinfe/semi-ui';
import { FlaskConical, Lock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CostChips, canAfford, useSectText } from './parts';

const MAX_CRAFT = 10;

function scaleCost(cost, count) {
  const scaled = {};
  for (const [key, value] of Object.entries(cost || {})) {
    scaled[key] = value * count;
  }
  return scaled;
}

const Recipe = ({ recipe, sect, alchemy, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const [count, setCount] = useState(1);
  const unlocked = alchemy >= recipe.alchemy;
  const cost = scaleCost(recipe.cost, count);
  const info = text.pills[recipe.key];
  const rareHave = recipe.rare ? sect.rare?.[recipe.rare] || 0 : 0;
  const rareNeed = recipe.rare ? recipe.rare_n * count : 0;
  return (
    <div className={`sc-recipe${unlocked ? '' : ' is-locked'}`}>
      <div className='sc-mission-head'>
        {unlocked ? <FlaskConical size={16} /> : <Lock size={16} />}
        <strong>{info?.name}</strong>
        {recipe.rare && (
          <span className='sc-muted'>
            {t('以{{rare}}入药', { rare: text.rares[recipe.rare] })}
          </span>
        )}
        <span className='sc-stock'>
          {t('库存 {{n}}', { n: sect.pills?.[recipe.key] || 0 })}
        </span>
      </div>
      <p className='sc-building-desc'>{info?.desc}</p>
      {unlocked ? (
        <div className='sc-building-foot'>
          <CostChips cost={cost} have={sect.res} />
          {recipe.rare && (
            <span
              className={`sc-chip is-rare${rareHave < rareNeed ? ' is-short' : ''}`}
            >
              {text.rares[recipe.rare]} {rareNeed} / {rareHave}
            </span>
          )}
          <span className='sc-spacer' />
          <InputNumber
            size='small'
            min={1}
            max={MAX_CRAFT}
            value={count}
            onChange={(value) =>
              setCount(Math.min(MAX_CRAFT, Math.max(1, Number(value) || 1)))
            }
            style={{ width: 76 }}
          />
          <Button
            size='small'
            theme='solid'
            disabled={busy || !canAfford(cost, sect.res) || rareHave < rareNeed}
            onClick={() => act('craft', { pill: recipe.id, count })}
          >
            {t('炼制')}
          </Button>
        </div>
      ) : (
        <span className='sc-hint'>
          {t('丹房 {{level}} 级解锁', { level: recipe.alchemy })}
        </span>
      )}
    </div>
  );
};

const Alchemy = ({ sect, busy, act }) => {
  const { t } = useTranslation();
  const alchemy = sect.buildings.find((b) => b.key === 'alchemy')?.level || 0;
  return (
    <div className='sc-alchemy'>
      <h4 className='sc-heading'>
        <FlaskConical size={16} />
        {alchemy > 0
          ? t('丹房 {{level}} 级 · 每炉 {{pct}}% 几率多出一颗', {
              level: alchemy,
              pct: alchemy * 4,
            })
          : t('还没有建丹房，先在宗门里建造')}
      </h4>
      <div className='sc-mission-list'>
        {sect.recipes.map((recipe) => (
          <Recipe
            key={recipe.id}
            recipe={recipe}
            sect={sect}
            alchemy={alchemy}
            busy={busy}
            act={act}
          />
        ))}
      </div>
    </div>
  );
};

export default Alchemy;
