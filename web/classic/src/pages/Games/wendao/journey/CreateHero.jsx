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
import StoryDialog from './StoryDialog';
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import ClassPicker from '../doufa/ClassPicker';
import '../doufa/doufa.css';
import { getPrologue } from './text';

// 第一次进历练：序章与选流派。流派一旦选定不能更改。
const CreateHero = ({ busy, onCreate }) => {
  const { t } = useTranslation();
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
      <ClassPicker value={picked} onChange={setPicked} />
      <p className='jn-muted'>
        {t('主角流派选定后不能更改，九幽秘境每轮可以另选流派')}
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
