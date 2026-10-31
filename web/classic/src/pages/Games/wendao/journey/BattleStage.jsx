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
import { Button, Modal, Tooltip } from '@douyinfe/semi-ui';
import { FlaskConical, Layers } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import Combat from '../../dungeon/Combat';
import { RewardScreen } from '../../dungeon/Screens';
import {
  HpBar,
  PileModal,
  RelicChip,
  useDungeonText,
} from '../../dungeon/parts';
import { useSectText } from '../../sect/parts';
import { heroEventLines } from './lines';
import { getNodeText } from './text';

// 历练里的一场仗：顶上是生命、丹符与法宝，台面用九幽秘境的战斗；打完显示胜负与这一场的收获。
const BattleStage = ({ hero, busy, ended, events, send, onFlee, onBack }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const sectText = useSectText();
  const nodes = getNodeText(t);
  const [targeting, setTargeting] = useState(null);
  const [deckOpen, setDeckOpen] = useState(false);

  if (ended) {
    const won = ended.run.result === 'won';
    const summary = heroEventLines(ended.events, t, text, sectText);
    let title = t('败下阵来');
    if (won) title = t('战斗胜利');
    else if (ended.fled) title = t('暂且退走');
    return (
      <div className={`dg-screen dg-end ${won ? 'is-won' : 'is-lost'}`}>
        <h3>{title}</h3>
        {won ? (
          <div className='jn-result'>
            {summary.lines.map((line, index) => (
              <p key={index}>{line}</p>
            ))}
            {summary.gains.length > 0 && (
              <p className='jn-muted'>{summary.gains.join(' · ')}</p>
            )}
          </div>
        ) : (
          <p className='dg-screen-note'>
            {t('没有奖励，也没有损失。修为、参悟功法或换一换牌组之后再来。')}
          </p>
        )}
        <div className='dg-screen-actions'>
          <Button theme='solid' onClick={onBack}>
            {t('返回地图')}
          </Button>
        </div>
      </div>
    );
  }

  const battle = hero.battle;
  const run = battle.run;
  const drink = (slot) => {
    const potion = run.potions[slot];
    const alive = (run.combat?.enemies || [])
      .map((enemy, index) => (!enemy.dead ? index : -1))
      .filter((index) => index >= 0);
    if (potion === 'fire' && alive.length > 1) {
      setTargeting({ potion: slot });
      return;
    }
    send('potion', { potion: slot, target: alive[0] ?? -1 });
  };
  const flee = () =>
    Modal.confirm({
      title: t('退出这场战斗？'),
      content: t('退走算作落败，没有奖励；没用掉的丹符会放回行囊。'),
      onOk: onFlee,
    });

  return (
    <>
      <div className='dg-hud'>
        <div className='dg-hud-row'>
          <span className='dg-mode is-trial'>{nodes[battle.node]?.name}</span>
          <span className='dg-hud-spacer' />
          <Button
            size='small'
            icon={<Layers size={14} />}
            onClick={() => setDeckOpen(true)}
          >
            {t('牌组 {{n}}', { n: run.deck.length })}
          </Button>
          {run.phase === 'combat' && (
            <Button size='small' type='danger' disabled={busy} onClick={flee}>
              {t('退走')}
            </Button>
          )}
        </div>
        <div className='dg-hud-row'>
          <span className='dg-hud-hp'>
            <HpBar hp={run.hp} max={run.max_hp} />
          </span>
          <span className='dg-potions'>
            {Array.from({ length: run.potion_slots }, (_, slot) => {
              const potion = run.potions[slot];
              if (!potion)
                return <span key={slot} className='dg-potion is-empty' />;
              return (
                <Tooltip
                  key={slot}
                  content={`${text.potions[potion]?.name}：${text.potions[potion]?.desc}`}
                >
                  <button
                    type='button'
                    className={`dg-potion is-${potion}`}
                    aria-label={text.potions[potion]?.name}
                    disabled={busy || run.phase !== 'combat'}
                    onClick={() => drink(slot)}
                  >
                    <FlaskConical size={15} />
                  </button>
                </Tooltip>
              );
            })}
          </span>
        </div>
        {run.relics.length > 0 && (
          <div className='dg-hud-row dg-relics'>
            {run.relics.map((id) => (
              <RelicChip key={id} id={id} />
            ))}
          </div>
        )}
      </div>
      <div className='dg-stage-body'>
        {run.phase === 'combat' && (
          <Combat
            run={run}
            busy={busy}
            send={send}
            events={events}
            targeting={targeting}
            setTargeting={setTargeting}
          />
        )}
        {run.phase === 'reward' && (
          <RewardScreen run={run} busy={busy} send={send} />
        )}
      </div>
      <PileModal
        visible={deckOpen}
        title={t('牌组')}
        cards={run.deck}
        onCancel={() => setDeckOpen(false)}
      />
    </>
  );
};

export default BattleStage;
