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
import { Minus, Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';

// 下注数量：减、当前数量、加，以及减半、翻倍、最大。游戏币只有整数，step 是每次加减的数量(21 点为 2，只能下偶数)。
// 数量总在 [min, max] 之内。
const BetInput = ({
  value,
  min = 1,
  max,
  step = 1,
  symbol,
  disabled,
  onChange,
  label,
}) => {
  const { t } = useTranslation();
  const clamp = (next) => {
    const stepped = Math.round(next / step) * step;
    return Math.min(Math.max(stepped, min), max);
  };
  return (
    <div className='game-bet'>
      <span className='game-bet-label'>{label || t('下注')}</span>
      <button
        type='button'
        className='game-bet-button'
        aria-label={t('减少下注')}
        disabled={disabled || value <= min}
        onClick={() => onChange(clamp(value - step))}
      >
        <Minus size={16} />
      </button>
      <span className='game-bet-value'>
        {value}
        <small>{symbol}</small>
      </span>
      <button
        type='button'
        className='game-bet-button'
        aria-label={t('增加下注')}
        disabled={disabled || value >= max}
        onClick={() => onChange(clamp(value + step))}
      >
        <Plus size={16} />
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || value <= min}
        onClick={() => onChange(clamp(Math.floor(value / 2)))}
      >
        ½
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || value >= max}
        onClick={() => onChange(clamp(value * 2))}
      >
        ×2
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || value >= max}
        onClick={() => onChange(clamp(max))}
      >
        {t('最大')}
      </button>
    </div>
  );
};

export default BetInput;
