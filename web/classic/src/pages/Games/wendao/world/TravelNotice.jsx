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
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useWendaoShell } from '../shell';
import '../duel/duel.css';

export default function TravelNotice({ active }) {
  const { t } = useTranslation();
  const { go } = useWendaoShell();
  if (!active) return null;
  return (
    <div className='dl-active' role='status'>
      <span>{t('游历期间不能进入斗法，请先回到世界结束游历。')}</span>
      <Button size='small' onClick={() => go({ part: 'world' })}>
        {t('去结束游历')}
      </Button>
    </div>
  );
}
