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

// 斗法的文案：标识与 pkg/doufa/content.json 一致。敌人的名字沿用九幽秘境的(dungeon/text.js 的 getEnemyNames)。
// 文案写成字面量 t() 调用，i18n 工具才能找到。
import { content } from './engine';

export const getWeaponNames = (t) => ({
  sword: t('长剑'),
  flying_sword: t('飞剑'),
  talisman: t('符箓'),
});

export const getWeaponText = (t) => ({
  sword: t('近战挥砍身前一片，不耗灵力，还能打掉扇面里飞来的弹'),
  flying_sword: t('远程，穿透一个敌人，每发耗 {{n}} 点灵力', {
    n: content.weapons.flying_sword.cost,
  }),
  talisman: t('远程，一次三张散开，每次耗 {{n}} 点灵力', {
    n: content.weapons.talisman.cost,
  }),
});

export const getSkillNames = (t) => ({
  quake: t('震地'),
  nova: t('焚天'),
  blades: t('剑阵'),
});

export const getSkillText = (t) => ({
  quake: t('身边一圈伤害并击退，顺手打掉近处的飞弹'),
  nova: t('向四周放出一圈火球'),
  blades: t('几把飞剑绕身旋转，碰到的敌人受伤，飞弹被打掉'),
});
