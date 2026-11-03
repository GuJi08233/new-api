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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Maximize2, Minimize2, Pause } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { playSound } from '../../common/sound';
import { getEnemyNames } from '../../dungeon/text';
import { createControls } from './controls';
import {
  BTN_ATTACK,
  BTN_DODGE,
  BTN_SKILL,
  BTN_SWAP,
  FPS,
  angleOf,
  createFight,
  skillOf,
  step,
  summary,
} from './engine';
import { createRenderer } from './render';
import { getSkillNames, getWeaponNames } from './text';

const STEP_MS = 1000 / FPS;
const SOUNDS = {
  swing: 'whoosh',
  shoot: 'flip',
  hit: 'hit',
  die: 'break',
  pick: 'coin',
  hurt: 'drop',
  dodge: 'flap',
  cast: 'sparkle',
  phase: 'bomb',
};
const STICK_RADIUS = 44;

const reducedMotion = () =>
  window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
const coarsePointer = () =>
  window.matchMedia?.('(hover: none), (pointer: coarse)').matches ?? false;

// Fight 打一间房：画布、逐帧取样操作、固定 60 帧走规则、画画面。打完把要点和逐帧输入交给 onEnd(交给服务端核对)。
// 切到后台自动暂停；Esc 暂停；手机上左边摇杆、右边四个键。暂停层上的退出键叫 exitLabel(秘境里是「撤退」)。
export default function Fight({ setup, seed, onEnd, onExit, exitLabel }) {
  const { t } = useTranslation();
  const stageRef = useRef(null);
  const canvasRef = useRef(null);
  const controlsRef = useRef(null);
  const pausedRef = useRef(false);
  const stickRef = useRef(null);
  const [paused, setPaused] = useState(false);
  const [hud, setHud] = useState({ skill: 0, slot: 0, mp: 0 });
  const [knob, setKnob] = useState(null);
  const [full, setFull] = useState(false);
  const [touch] = useState(coarsePointer);
  const onEndRef = useRef(onEnd);
  onEndRef.current = onEnd;

  const setPause = useCallback((next) => {
    pausedRef.current = next;
    setPaused(next);
    controlsRef.current?.reset();
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    const stage = stageRef.current;
    const fight = createFight(seed, setup);
    const names = getEnemyNames(t);
    const renderer = createRenderer(canvas, {
      reduced: reducedMotion(),
      labels: {
        hp: (hp, max) => t('生命 {{hp}}/{{max}}', { hp, max }),
        mp: (n) => t('灵力 {{n}}', { n }),
        noMana: t('灵力不足'),
        wave: (n, total) => t('第 {{n}}/{{total}} 波', { n, total }),
        foe: (kind) => names[kind] || kind,
      },
    });
    const controls = createControls({
      canvas,
      onPause: () => setPause(!pausedRef.current),
    });
    controlsRef.current = controls;
    const inputs = [];
    let raf = 0;
    let last = performance.now();
    let acc = 0;
    let frames = 0;
    let done = false;
    let shown = { skill: -1, slot: -1, mp: -1 };

    const size = () => {
      const box = stage.getBoundingClientRect();
      renderer.resize(box.width, box.height);
      renderer.draw(fight, []);
    };
    size();
    const resizeObserver = new ResizeObserver(size);
    resizeObserver.observe(stage);
    const themeObserver = new MutationObserver(() => {
      renderer.refreshPalette();
      renderer.draw(fight, []);
    });
    themeObserver.observe(document.body, {
      attributes: true,
      attributeFilter: ['theme-mode'],
    });
    const onVisibility = () => {
      if (document.hidden && !done) setPause(true);
    };
    document.addEventListener('visibilitychange', onVisibility);

    const frame = (now) => {
      raf = requestAnimationFrame(frame);
      const dt = now - last;
      last = now;
      if (pausedRef.current) return;
      const events = [];
      if (!done) {
        acc += Math.min(dt, 250);
        while (acc >= STEP_MS && !fight.over) {
          const input = controls.sample(fight, renderer.view);
          inputs.push(input);
          step(fight, input);
          if (fight.events.length) events.push(...fight.events);
          acc -= STEP_MS;
        }
      }
      for (const e of events) if (SOUNDS[e.k]) playSound(SOUNDS[e.k]);
      renderer.draw(fight, events);
      if (++frames % 6 === 0) {
        const next = {
          skill: Math.ceil(fight.p.skillCool / FPS),
          slot: fight.p.slot,
          mp: Math.floor(fight.p.mp / 100),
        };
        if (
          next.skill !== shown.skill ||
          next.slot !== shown.slot ||
          next.mp !== shown.mp
        ) {
          shown = next;
          setHud(next);
        }
      }
      if (fight.over && !done) {
        done = true;
        playSound(fight.won ? 'win' : 'lose');
        setTimeout(() => onEndRef.current({ ...summary(fight), inputs }), 900);
      }
    };
    raf = requestAnimationFrame(frame);
    return () => {
      cancelAnimationFrame(raf);
      resizeObserver.disconnect();
      themeObserver.disconnect();
      document.removeEventListener('visibilitychange', onVisibility);
      controls.dispose();
      controlsRef.current = null;
    };
    // 一局只在种子变了时重开；setup 与文案在一局里不变。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seed]);

  useEffect(() => {
    const onChange = () =>
      setFull(document.fullscreenElement === stageRef.current);
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, []);

  const toggleFull = () => {
    if (document.fullscreenElement) document.exitFullscreen?.();
    else stageRef.current?.requestFullscreen?.().catch(() => {});
  };

  const stickDown = (e) => {
    e.currentTarget.setPointerCapture?.(e.pointerId);
    const box = e.currentTarget.getBoundingClientRect();
    stickRef.current = {
      id: e.pointerId,
      x: e.clientX - box.left,
      y: e.clientY - box.top,
    };
    setKnob({ x: stickRef.current.x, y: stickRef.current.y, dx: 0, dy: 0 });
  };
  const stickMove = (e) => {
    const s = stickRef.current;
    if (!s || s.id !== e.pointerId) return;
    const box = e.currentTarget.getBoundingClientRect();
    let dx = e.clientX - box.left - s.x;
    let dy = e.clientY - box.top - s.y;
    const d = Math.hypot(dx, dy);
    controlsRef.current?.setStick(
      d > 8 ? angleOf(Math.round(dx * 100), Math.round(dy * 100)) : -1,
    );
    if (d > STICK_RADIUS) {
      dx = (dx / d) * STICK_RADIUS;
      dy = (dy / d) * STICK_RADIUS;
    }
    setKnob({ x: s.x, y: s.y, dx, dy });
  };
  const stickUp = (e) => {
    if (stickRef.current?.id !== e.pointerId) return;
    stickRef.current = null;
    controlsRef.current?.setStick(-1);
    setKnob(null);
  };
  const button = (bit) => ({
    onPointerDown: (e) => {
      e.preventDefault();
      e.currentTarget.setPointerCapture?.(e.pointerId);
      controlsRef.current?.press(bit);
    },
    onPointerUp: () => controlsRef.current?.release(bit),
    onPointerCancel: () => controlsRef.current?.release(bit),
    onContextMenu: (e) => e.preventDefault(),
  });

  const weapons = getWeaponNames(t);
  const skills = getSkillNames(t);
  const skill = skillOf(setup);
  const other = setup.weapons.length > 1 ? setup.weapons[1 - hud.slot] : null;

  return (
    <div
      ref={stageRef}
      className={`df-stage ${full ? 'is-full' : ''} ${touch ? 'is-touch' : ''}`}
    >
      <canvas ref={canvasRef} className='df-canvas' aria-label={t('斗法')} />
      <div className='df-tools'>
        <button
          type='button'
          className='df-tool'
          onClick={() => setPause(true)}
          aria-label={t('暂停')}
        >
          <Pause size={16} />
        </button>
        {document.fullscreenEnabled && (
          <button
            type='button'
            className='df-tool'
            onClick={toggleFull}
            aria-label={full ? t('退出全屏') : t('全屏')}
          >
            {full ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
        )}
      </div>
      {touch && (
        <>
          <div
            className='df-stick-zone'
            onPointerDown={stickDown}
            onPointerMove={stickMove}
            onPointerUp={stickUp}
            onPointerCancel={stickUp}
          >
            {knob && (
              <div className='df-stick' style={{ left: knob.x, top: knob.y }}>
                <div
                  className='df-knob'
                  style={{ transform: `translate(${knob.dx}px, ${knob.dy}px)` }}
                />
              </div>
            )}
          </div>
          <div className='df-pad'>
            <button
              type='button'
              className='df-btn df-btn-attack'
              {...button(BTN_ATTACK)}
            >
              {t('攻击')}
            </button>
            <button
              type='button'
              className={`df-btn df-btn-skill ${hud.skill > 0 || hud.mp < skill.cost ? 'is-cooling' : ''}`}
              {...button(BTN_SKILL)}
            >
              {hud.skill > 0 ? hud.skill : skills[skill.kind]}
            </button>
            <button
              type='button'
              className='df-btn df-btn-dodge'
              {...button(BTN_DODGE)}
            >
              {t('身法')}
            </button>
            {other && (
              <button
                type='button'
                className='df-btn df-btn-swap'
                {...button(BTN_SWAP)}
              >
                {weapons[other]}
              </button>
            )}
          </div>
        </>
      )}
      {paused && (
        <div className='df-overlay'>
          <div className='df-overlay-card'>
            <div className='df-overlay-title'>{t('已暂停')}</div>
            <div className='df-overlay-actions'>
              <Button theme='solid' onClick={() => setPause(false)}>
                {t('继续')}
              </Button>
              <Button onClick={onExit}>{exitLabel || t('退出')}</Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
