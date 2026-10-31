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

import React, { useEffect, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import ChapterScene from '../art/Scene';
import EnemyArt from '../art/EnemyArt';
import Portrait from '../art/Portrait';
import { getCast } from './text';

// 逐字显示的速度(毫秒一个字)；系统要求减少动态效果时直接整句显示。
const TYPE_MS = 32;

const reducedMotion = () =>
  typeof window !== 'undefined' &&
  window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

// 一段剧情：章节底图上一个对话框，左边是说话人的画像，台词逐字出现；点对话框(或按回车、空格)先补全这一句，再翻到
// 下一句。最后一句显示完后出现 doneLabel 按钮。可以随时跳到最后一句。
const StoryDialog = ({ lines, chapter, title, doneLabel, onDone }) => {
  const { t } = useTranslation();
  const cast = getCast(t);
  const [index, setIndex] = useState(0);
  const [shown, setShown] = useState(0);
  const current = lines[index];
  const full = current.text.length;
  const last = index === lines.length - 1;
  const typing = shown < full;

  useEffect(() => {
    if (reducedMotion()) {
      setShown(full);
      return undefined;
    }
    setShown(0);
    const timer = setInterval(() => {
      setShown((n) => {
        if (n + 1 >= full) clearInterval(timer);
        return Math.min(n + 1, full);
      });
    }, TYPE_MS);
    return () => clearInterval(timer);
  }, [index, full]);

  const advance = () => {
    if (typing) {
      setShown(full);
      return;
    }
    if (!last) setIndex(index + 1);
  };
  const onKey = (event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      advance();
    }
  };

  const speaker = cast[current.who] || cast.narrator;
  const narrator = !speaker.name;
  return (
    <div className='wd-story'>
      <ChapterScene chapter={chapter} className='wd-story-scene' />
      {title && <h3 className='wd-story-title'>{title}</h3>}
      <div
        className={`wd-story-box ${narrator ? 'is-narrator' : ''}`}
        role='button'
        tabIndex={0}
        aria-label={t('下一句')}
        onClick={advance}
        onKeyDown={onKey}
      >
        {!narrator && (
          <span className='wd-story-face' key={current.who}>
            {speaker.enemy ? (
              <EnemyArt id={speaker.enemy} />
            ) : (
              <Portrait who={speaker.portrait} />
            )}
          </span>
        )}
        <div className='wd-story-body'>
          {!narrator && <div className='wd-story-name'>{speaker.name}</div>}
          <p aria-live='polite'>
            {current.text.slice(0, shown)}
            {typing && <span className='wd-story-caret' aria-hidden />}
          </p>
        </div>
        {!typing && !last && (
          <span className='wd-story-more' aria-hidden>
            ▾
          </span>
        )}
      </div>
      <div className='wd-story-actions'>
        <span className='wd-story-count'>
          {index + 1} / {lines.length}
        </span>
        {!last && (
          <Button
            theme='borderless'
            onClick={() => {
              setIndex(lines.length - 1);
            }}
          >
            {t('跳过')}
          </Button>
        )}
        {last && !typing && (
          <Button theme='solid' onClick={onDone}>
            {doneLabel}
          </Button>
        )}
      </div>
    </div>
  );
};

export default StoryDialog;
