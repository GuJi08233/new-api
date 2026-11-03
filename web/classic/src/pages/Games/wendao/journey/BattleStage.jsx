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
import { Button, Modal } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useDungeonText } from '../../dungeon/parts';
import { useSectText } from '../../sect/parts';
import { newRequestId } from '../../common/api';
import Fight from '../doufa/Fight';
import { FPS } from '../doufa/engine';
import { heroEventLines } from './lines';
import DefeatReview from './DefeatReview';
import { getNodeText } from './text';
import '../doufa/doufa.css';

// 种子只在开战时发给本页，刷新后只剩概况。打完保留操作记录，核对失败时可以原样重新提交。
const BattleStage = ({
  hero,
  busy,
  fight,
  ended,
  onClear,
  onFlee,
  onBack,
  onGo,
}) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const sectText = useSectText();
  const nodes = getNodeText(t);
  const [replay, setReplay] = useState(null);
  const flee = () => {
    if (document.fullscreenElement) document.exitFullscreen?.();
    Modal.confirm({
      title: t('退出这场战斗？'),
      content: t('退走算作落败，没有奖励；开战时消耗的丹符不退。'),
      onOk: onFlee,
    });
  };

  if (ended) {
    const summary = heroEventLines(ended.events, t, text, sectText);
    const seconds = Math.floor((ended.ticks || 0) / FPS);
    return (
      <div className={`df-card df-result ${ended.won ? 'is-won' : ''}`}>
        <h3>
          {ended.won
            ? t('战斗胜利')
            : ended.fled
              ? t('暂且退走')
              : t('败下阵来')}
        </h3>
        {!ended.fled && (
          <ul className='df-stats'>
            <li>
              {t('用时 {{time}}', {
                time: `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`,
              })}
            </li>
            <li>{t('击败 {{n}}', { n: ended.kills || 0 })}</li>
            <li>{t('打出伤害 {{n}}', { n: ended.damage_dealt || 0 })}</li>
            <li>{t('受到伤害 {{n}}', { n: ended.damage_taken || 0 })}</li>
          </ul>
        )}
        <div className='jn-result'>
          {summary.lines.map((line, index) => (
            <p key={index}>{line}</p>
          ))}
          {summary.gains.length > 0 && (
            <p className='jn-muted'>{summary.gains.join(' · ')}</p>
          )}
        </div>
        {!ended.won && !ended.fled && (
          <DefeatReview events={ended.events} onGo={onGo} />
        )}
        <div className='df-actions'>
          <Button theme='solid' onClick={onBack}>
            {t('返回地图')}
          </Button>
        </div>
      </div>
    );
  }

  const battle = hero.battle;
  const title = battle.trial
    ? t('周天试炼 · 第 {{n}} 关', { n: battle.trial })
    : battle.step
      ? t('问心阶 · 第 {{n}} 级', { n: battle.step })
      : nodes[battle.node]?.name;
  const submit = (run) => {
    const recorded = {
      ...run,
      seed: fight.seed,
      issued: fight.issued,
      token: fight.token,
      requestId: replay?.requestId || newRequestId(),
    };
    setReplay(recorded);
    onClear(recorded);
  };

  return (
    <div className='df-panel'>
      <h4>{title}</h4>
      {replay ? (
        <div className='df-card'>
          <p className='df-muted'>
            {busy
              ? t('正在核对这一间房…')
              : t('操作记录已保留，可以重新提交核对。')}
          </p>
          <div className='df-actions'>
            <Button
              theme='solid'
              disabled={busy}
              onClick={() => onClear(replay)}
            >
              {t('重新提交')}
            </Button>
            <Button disabled={busy} onClick={flee}>
              {t('退走')}
            </Button>
          </div>
        </div>
      ) : fight ? (
        <>
          <Fight
            key={fight.token}
            seed={fight.seed}
            setup={fight.setup}
            onEnd={submit}
            onExit={flee}
            exitLabel={t('退走')}
          />
          <p className='df-muted'>
            {t('离开或刷新后只能退走；打完的操作记录会交给服务端核对。')}
          </p>
        </>
      ) : (
        <div className='df-card'>
          <p className='df-lead'>{t('这一场已经开始过了，只能放弃')}</p>
          <p className='df-muted'>
            {t('离开或刷新后只能退走；打完的操作记录会交给服务端核对。')}
          </p>
          <Button disabled={busy} onClick={flee}>
            {t('退走')}
          </Button>
        </div>
      )}
    </div>
  );
};

export default BattleStage;
