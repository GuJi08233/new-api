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
import { Minus, Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toCoinCents } from './api';

// 下注数量：减、可以直接输入的数量、加，以及减半、翻倍、最大。下注最多两位小数，按分计算免得浮点误差；step 是每次加减的数量。
// 数量总在 [min, max] 之内，输入的零头不足一分的舍去。
const BetInput = ({
  value,
  min = 0.01,
  max,
  step = 1,
  symbol,
  disabled,
  onChange,
  label,
}) => {
  const { t } = useTranslation();
  // 正在输入的文字；不在输入时显示当前数量。
  const [draft, setDraft] = useState(null);
  const low = Math.max(toCoinCents(min), 1);
  const top = Math.max(toCoinCents(max), low);
  const cents = toCoinCents(value);
  const set = (next) => onChange(Math.min(Math.max(next, low), top) / 100);
  const commit = () => {
    if (
      draft !== null &&
      draft.trim() !== '' &&
      Number.isFinite(Number(draft))
    ) {
      set(toCoinCents(draft));
    }
    setDraft(null);
  };
  return (
    <div className='game-bet'>
      <span className='game-bet-label'>{label || t('下注')}</span>
      <button
        type='button'
        className='game-bet-button'
        aria-label={t('减少下注')}
        disabled={disabled || cents <= low}
        onClick={() => set(cents - toCoinCents(step))}
      >
        <Minus size={16} />
      </button>
      <span className='game-bet-value'>
        <input
          className='game-bet-input'
          inputMode='decimal'
          aria-label={label || t('下注')}
          disabled={disabled}
          value={draft ?? String(value)}
          size={Math.max(String(draft ?? value).length, 1)}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === 'Enter') event.currentTarget.blur();
          }}
        />
        <small>{symbol}</small>
      </span>
      <button
        type='button'
        className='game-bet-button'
        aria-label={t('增加下注')}
        disabled={disabled || cents >= top}
        onClick={() => set(cents + toCoinCents(step))}
      >
        <Plus size={16} />
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || cents <= low}
        onClick={() => set(Math.floor(cents / 2))}
      >
        ½
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || cents >= top}
        onClick={() => set(cents * 2)}
      >
        ×2
      </button>
      <button
        type='button'
        className='game-bet-quick'
        disabled={disabled || cents >= top}
        onClick={() => set(top)}
      >
        {t('最大')}
      </button>
    </div>
  );
};

export default BetInput;
