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
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { playSound } from '../../common/sound';
import ClassPicker from './ClassPicker';
import { FPS, content, trialSetup } from './engine';
import Fight from './Fight';
import './doufa.css';

const coarsePointer = () =>
  window.matchMedia?.('(hover: none), (pointer: coarse)').matches ?? false;

const formatTime = (ticks) => {
  const seconds = Math.floor(ticks / FPS);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
};

const newSeed = () =>
  (Date.now() ^ Math.floor(Math.random() * 0x7fffffff)) >>> 0 || 1;

// 试剑台：斗法(即时战斗)的练手处。选流派，打一间分四波刷怪、最后一波是首领的房间；只在浏览器里打，不发奖励。
export default function DoufaPractice({ hero }) {
  const { t } = useTranslation();
  const [cls, setCls] = useState(
    hero?.class && content.classes[hero.class] ? hero.class : 'warrior',
  );
  const [phase, setPhase] = useState('pick');
  const [seed, setSeed] = useState(0);
  const [result, setResult] = useState(null);
  const [touch] = useState(coarsePointer);

  const start = () => {
    setSeed(newSeed());
    setResult(null);
    setPhase('fight');
    playSound('start');
  };

  if (phase === 'fight') {
    return (
      <div className='df-panel'>
        <Fight
          key={seed}
          seed={seed}
          setup={trialSetup(cls)}
          onEnd={(next) => {
            setResult(next);
            setPhase('result');
          }}
          onExit={() => setPhase('pick')}
        />
      </div>
    );
  }

  if (phase === 'result' && result) {
    const title = result.won
      ? t('试剑告捷')
      : result.hp <= 0
        ? t('力竭倒下')
        : t('时辰已到');
    return (
      <div className='df-panel'>
        <div className={`df-card df-result ${result.won ? 'is-won' : ''}`}>
          <div className='df-title'>{title}</div>
          <ul className='df-stats'>
            <li>{t('用时 {{time}}', { time: formatTime(result.tick) })}</li>
            <li>{t('击败 {{n}}', { n: result.kills })}</li>
            <li>{t('受伤 {{n}}', { n: result.taken })}</li>
            <li>{t('拾得灵石 {{n}}', { n: result.stones })}</li>
          </ul>
          <p className='df-muted'>{t('试剑台只是练手，拾得的灵石不入库。')}</p>
          <div className='df-actions'>
            <Button theme='solid' onClick={start}>
              {t('再来一次')}
            </Button>
            <Button onClick={() => setPhase('pick')}>{t('换个流派')}</Button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className='df-panel'>
      <div className='df-card'>
        <div className='df-title'>{t('试剑台')}</div>
        <p className='df-lead'>
          {t(
            '斗法是即时的战斗：走位躲开敌人的攻击，近战、远程、法术与身法配合着用。这里是练手的地方，一间房四波敌人，最后一波是首领，不发奖励。',
          )}
        </p>
        <ClassPicker value={cls} onChange={setCls} />
        <p className='df-muted'>
          {touch
            ? t(
                '触屏：左边按住拖动移动，右边的键攻击(自动瞄准)、法术、身法与换法宝。横过来打更宽敞。',
              )
            : t(
                '键盘：WASD 或方向键移动，鼠标瞄准、左键攻击(不用鼠标时按 J 攻击，自动瞄准最近的敌人)，右键或 K 放法术，空格或 Shift 身法(冲刺时不受伤)，Q 换法宝，Esc 暂停。',
              )}
        </p>
        <div className='df-actions'>
          <Button theme='solid' onClick={start}>
            {t('开始试剑')}
          </Button>
        </div>
      </div>
    </div>
  );
}
