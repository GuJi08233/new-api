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
import { RuleList, SideBox } from '../../common/SideBox';
import { SkillSide } from '../../common/skill';
import { getGameLevelNames } from '../../../../constants/game.constants';
import { WendaoLayout, useWendaoPart, useWendaoShell } from '../shell';
import RiftPanel from './RiftPanel';

// 九幽秘境板块的页面：左边是秘境斗法的一轮，右边是领奖规则、今天的进度与道韵。
export default function Abyss() {
  const { t } = useTranslation();
  const { info, patch, reload } = useWendaoPart('abyss');
  const { refreshAgenda } = useWendaoShell();
  const levelNames = getGameLevelNames(t).wendao;
  return (
    <WendaoLayout
      className='dg-page'
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('打通')}
            levelNames={levelNames}
            extra={
              <>
                <SideBox
                  label={t('道韵')}
                  value={info.meta?.crystals ?? 0}
                  note={t('累计获得 {{n}}', { n: info.meta?.earned ?? 0 })}
                />
                <SideBox label={t('玩法说明')}>
                  <RuleList
                    items={[
                      t(
                        '九幽秘境：一重接一重往下打，打倒首领进入下一重，敌人越来越强；按打通的重数领游戏币',
                      ),
                      t(
                        '道韵：每轮结束按打通的房间和重数得到，用来升级天赋，天赋在开局时带进一轮',
                      ),
                      t(
                        '退路：战斗房发出以后只能打完，中途退出、刷新页面要用一次退路，用完再退出就算倒下',
                      ),
                      t(
                        '道具商店用游戏币买一次性道具，在两间房之间用；花掉的游戏币不退回',
                      ),
                      t('每一步都保存在服务器上，刷新或换设备都能继续'),
                    ]}
                  />
                </SideBox>
              </>
            }
          />
        )
      }
    >
      {info?.meta && (
        <RiftPanel
          info={info}
          patch={patch}
          reload={reload}
          refreshAgenda={refreshAgenda}
        />
      )}
    </WendaoLayout>
  );
}
