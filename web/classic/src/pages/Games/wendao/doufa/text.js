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
  spear: t('长枪'),
  thunder: t('雷印'),
});

export const getWeaponText = (t) => ({
  sword: t('近战挥砍身前一片，不耗灵力，还能打掉扇面里飞来的弹'),
  flying_sword: t('远程，穿透一个敌人，每发耗 {{n}} 点灵力', {
    n: content.weapons.flying_sword.cost,
  }),
  talisman: t('远程，一次三张散开，每次耗 {{n}} 点灵力', {
    n: content.weapons.talisman.cost,
  }),
  spear: t('近战直刺，够得远但扇面窄，不耗灵力'),
  thunder: t('远程，打中炸开、波及周围的敌人，每发耗 {{n}} 点灵力', {
    n: content.weapons.thunder.cost,
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

// 秘境斗法里每一步能进的房间。
export const getRoomText = (t) => ({
  fight: { name: t('战斗'), desc: t('两三波敌人，打完有时给一样祝福') },
  elite: { name: t('精英'), desc: t('敌人更多，最后一波有精英，打完必给祝福') },
  treasure: { name: t('宝库'), desc: t('二选一，换一件法宝') },
  rest: { name: t('静修'), desc: t('回复三成五的生命') },
  shrine: { name: t('祠堂'), desc: t('三选一的祝福，不用打') },
  boss: { name: t('首领'), desc: t('这一重的首领，打倒后回血进下一重') },
});

// 道韵天赋：数值取自 content.json，与服务端算的加成一致。名字沿用旧版秘境的「筑体」「调息」「福缘」。
export const getTalentText = (t) => {
  const talents = Object.fromEntries(content.talents.map((x) => [x.id, x]));
  return {
    vitality: {
      name: t('筑体'),
      desc: t('每级生命上限 +{{n}}', { n: talents.vitality.mods.vigor }),
    },
    might: {
      name: t('锋芒'),
      desc: t('每级伤害 +{{n}}%', { n: talents.might.mods.damage }),
    },
    fleet: {
      name: t('轻身'),
      desc: t('每级移速 +{{n}}%', { n: talents.fleet.mods.speed }),
    },
    spirit: {
      name: t('聚气'),
      desc: t('每级灵力回复 +{{n}}%', { n: talents.spirit.mods.regen }),
    },
    anchor: { name: t('定魂'), desc: t('每级多一次退路') },
    medic: {
      name: t('调息'),
      desc: t('静修多回复 {{n}}% 生命上限', { n: talents.medic.rest }),
    },
    fortune: { name: t('福缘'), desc: t('开局获得一样随机祝福') },
  };
};

// 祝福：数值取自 content.json，与服务端算的加成一致。
export const getBlessingText = (t) => {
  const mods = Object.fromEntries(content.blessings.map((b) => [b.id, b.mods]));
  return {
    edge: { name: t('锋锐'), desc: t('伤害 +{{n}}%', { n: mods.edge.damage }) },
    swift: {
      name: t('迅捷'),
      desc: t('移速 +{{n}}%', { n: mods.swift.speed }),
    },
    haste: {
      name: t('连珠'),
      desc: t('法宝冷却 -{{n}}%', { n: mods.haste.haste }),
    },
    pierce: {
      name: t('穿云'),
      desc: t('飞弹多穿透 {{n}} 个敌人', { n: mods.pierce.pierce }),
    },
    spring: {
      name: t('回元'),
      desc: t('灵力回复 +{{n}}%', { n: mods.spring.regen }),
    },
    vigor: {
      name: t('铁骨'),
      desc: t('生命上限 +{{n}}', { n: mods.vigor.vigor }),
    },
    dash: {
      name: t('疾风'),
      desc: t('身法冷却 -{{n}}%', { n: mods.dash.dodge }),
    },
    focus: {
      name: t('凝神'),
      desc: t('法术冷却 -{{n}}%', { n: mods.focus.focus }),
    },
    reach: {
      name: t('长锋'),
      desc: t('近战范围 +{{n}}%', { n: mods.reach.reach }),
    },
    ward: { name: t('护心'), desc: t('每间房挡掉第一次伤害') },
  };
};
