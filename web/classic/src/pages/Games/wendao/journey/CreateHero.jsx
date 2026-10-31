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
import Portrait from '../art/Portrait';
import StoryDialog from './StoryDialog';
import { Button } from '@douyinfe/semi-ui';
import { Flame, Shield, Swords } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getClassText } from '../../dungeon/text';
import { getPrologue } from './text';

export const CLASS_ICONS = { warrior: Shield, mage: Flame, ranger: Swords };
const CLASSES = ['warrior', 'mage', 'ranger'];

// 第一次进历练：序章与选流派。流派一旦选定不能更改。
const CreateHero = ({ busy, onCreate }) => {
  const { t } = useTranslation();
  const classes = getClassText(t);
  const [picked, setPicked] = useState('warrior');
  const [heard, setHeard] = useState(false);
  if (!heard) {
    return (
      <StoryDialog
        lines={getPrologue(t)}
        chapter={0}
        title={t('序章 · 宗主令')}
        doneLabel={t('选择道途')}
        onDone={() => setHeard(true)}
      />
    );
  }
  return (
    <div className='jn-create'>
      <h3 className='jn-create-title'>{t('选择道途')}</h3>
      <div className='jn-classes' role='radiogroup' aria-label={t('选择流派')}>
        {CLASSES.map((cls) => {
          const Icon = CLASS_ICONS[cls];
          return (
            <button
              type='button'
              key={cls}
              role='radio'
              aria-checked={picked === cls}
              className={`jn-class ${picked === cls ? 'is-picked' : ''}`}
              onClick={() => setPicked(cls)}
            >
              <span className='jn-class-face'>
                <Portrait who={`hero_${cls}`} />
              </span>
              <strong>
                <Icon size={15} />
                {classes[cls].name}
              </strong>
              <span>{classes[cls].desc}</span>
            </button>
          );
        })}
      </div>
      <p className='jn-muted'>
        {t('流派选定后不能更改，九幽秘境里的流派另外解锁')}
      </p>
      <Button
        theme='solid'
        size='large'
        disabled={busy}
        onClick={() => onCreate(picked)}
      >
        {t('踏上修仙路')}
      </Button>
    </div>
  );
};

export default CreateHero;
