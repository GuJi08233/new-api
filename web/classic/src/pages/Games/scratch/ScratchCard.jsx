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

import React, { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { Ticket } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export const CELL_COUNT = 9;
// 每格按 SAMPLE_GRID × SAMPLE_GRID 个采样点估算刮开了多少，刮开的点达到 REVEAL_RATIO 就整格揭开。
const SAMPLE_GRID = 5;
const REVEAL_RATIO = 0.5;
// 笔刷半径，占票面宽度的比例。
const BRUSH_RATIO = 0.06;
// 涂层的圆角，与格子的圆角一致(CSS 像素)。
const CELL_RADIUS = 10;

// 指针在票面上的位置，按票面宽度归一化(票面是正方形)。
function pointerPoint(canvas, event) {
  const rect = canvas.getBoundingClientRect();
  return [
    (event.clientX - rect.left) / rect.width,
    (event.clientY - rect.top) / rect.width,
  ];
}

// 在画布上沿一串点擦掉涂层(只有一个点时擦一个圆)。坐标是归一化的，size 是票面的 CSS 宽度。
function eraseStroke(ctx, points, size) {
  const radius = BRUSH_RATIO * size;
  ctx.globalCompositeOperation = 'destination-out';
  if (points.length === 1) {
    ctx.beginPath();
    ctx.arc(points[0][0] * size, points[0][1] * size, radius, 0, Math.PI * 2);
    ctx.fill();
    return;
  }
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.lineWidth = radius * 2;
  ctx.beginPath();
  points.forEach(([x, y], index) => {
    if (index === 0) ctx.moveTo(x * size, y * size);
    else ctx.lineTo(x * size, y * size);
  });
  ctx.stroke();
}

// 按当前尺寸重画涂层：没揭开的格子盖上涂层，再按这张票刮过的笔画擦掉。笔画按归一化坐标保存，窗口缩放后仍在原处。
function paintCoating(canvas, layout, revealed, strokes, colors) {
  const ratio = window.devicePixelRatio || 1;
  const { size, cells } = layout;
  canvas.width = Math.round(size * ratio);
  canvas.height = Math.round(size * ratio);
  const ctx = canvas.getContext('2d');
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, size, size);
  ctx.globalCompositeOperation = 'source-over';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  cells.forEach((cell, index) => {
    if (revealed[index]) return;
    const x = cell.x * size;
    const y = cell.y * size;
    const w = cell.w * size;
    const h = cell.h * size;
    ctx.fillStyle = colors.coating;
    ctx.beginPath();
    ctx.roundRect(x, y, w, h, CELL_RADIUS);
    ctx.fill();
    ctx.fillStyle = colors.mark;
    ctx.font = `800 ${Math.round(w * 0.36)}px sans-serif`;
    ctx.fillText('?', x + w / 2, y + h / 2);
  });
  strokes.forEach((points) => eraseStroke(ctx, points, size));
}

// 把一段笔画经过的采样点记为已刮开(点到线段的距离不超过笔刷半径)，返回刮开比例达到 REVEAL_RATIO、还没揭开的格子。
function scratchSamples(samples, layout, revealed, from, to) {
  const [ax, ay] = from;
  const dx = to[0] - ax;
  const dy = to[1] - ay;
  const lengthSquared = dx * dx + dy * dy;
  const reach = BRUSH_RATIO * BRUSH_RATIO;
  const opened = [];
  layout.cells.forEach((cell, index) => {
    if (revealed[index]) return;
    const points = samples[index];
    let scratched = 0;
    for (let k = 0; k < points.length; k++) {
      if (!points[k]) {
        const px = cell.x + (cell.w * ((k % SAMPLE_GRID) + 0.5)) / SAMPLE_GRID;
        const py =
          cell.y + (cell.h * (Math.floor(k / SAMPLE_GRID) + 0.5)) / SAMPLE_GRID;
        const along = lengthSquared
          ? Math.min(
              Math.max(((px - ax) * dx + (py - ay) * dy) / lengthSquared, 0),
              1,
            )
          : 0;
        const ex = ax + along * dx - px;
        const ey = ay + along * dy - py;
        points[k] = ex * ex + ey * ey <= reach;
      }
      if (points[k]) scratched++;
    }
    if (scratched >= REVEAL_RATIO * points.length) opened.push(index);
  });
  return opened;
}

// 一张刮刮乐：3×3 格金额上盖着涂层，按住拖动刮开，一格刮开一半以上就整格揭开。ticket 为空时是还没买的票，不能刮；
// price 是还没买时票面上显示的票价。revealed 是每格是否已揭开，由页面保存(全部刮开时页面一次揭开)；onReveal(cell) 报告
// 刚刮开的一格；matches 标出中奖的三格。
const ScratchCard = ({
  ticket,
  title,
  price,
  symbol,
  revealed,
  matches,
  onReveal,
}) => {
  const { t } = useTranslation();
  const gridRef = useRef(null);
  const canvasRef = useRef(null);
  const cellRefs = useRef([]);
  // 票面的 CSS 宽度与每格的位置(按宽度归一化)，重画时量出。
  const layoutRef = useRef(null);
  // 这张票刮过的笔画(每一笔是一串归一化的点)、每格的采样点、正在刮的一笔。
  const strokesRef = useRef([]);
  const samplesRef = useRef([]);
  const strokeRef = useRef(null);
  const revealedRef = useRef(revealed);
  const ticketId = ticket?.id || 0;
  const active = !!ticket && !revealed.every(Boolean);

  const paint = useCallback(() => {
    const grid = gridRef.current;
    const canvas = canvasRef.current;
    const size = grid?.clientWidth || 0;
    if (!canvas || !size) return;
    layoutRef.current = {
      size,
      cells: cellRefs.current.map((cell) => ({
        x: cell.offsetLeft / size,
        y: cell.offsetTop / size,
        w: cell.offsetWidth / size,
        h: cell.offsetHeight / size,
      })),
    };
    // 涂层颜色写在样式表里，随明暗主题变化。
    const style = getComputedStyle(grid);
    paintCoating(
      canvas,
      layoutRef.current,
      revealedRef.current,
      strokesRef.current,
      {
        coating: style.getPropertyValue('--scratch-coating').trim(),
        mark: style.getPropertyValue('--scratch-coating-mark').trim(),
      },
    );
  }, []);

  // 换了一张票：清掉上一张的笔画与采样。
  useLayoutEffect(() => {
    strokesRef.current = [];
    samplesRef.current = Array.from({ length: CELL_COUNT }, () =>
      new Array(SAMPLE_GRID * SAMPLE_GRID).fill(false),
    );
    strokeRef.current = null;
  }, [ticketId]);

  // 换了票或揭开了格子：在浏览器绘制之前重画涂层，新票的金额不会先露出来。
  useLayoutEffect(() => {
    revealedRef.current = revealed;
    paint();
  }, [ticketId, revealed, paint]);

  // 票面大小变化(旋转屏幕、调整窗口)或切换明暗主题时重画。
  useEffect(() => {
    const resize = new ResizeObserver(() => paint());
    resize.observe(gridRef.current);
    const theme = new MutationObserver(() => paint());
    theme.observe(document.body, {
      attributes: true,
      attributeFilter: ['theme-mode'],
    });
    return () => {
      resize.disconnect();
      theme.disconnect();
    };
  }, [paint]);

  // 刮过一段(一个点或相邻两个点)：擦掉涂层，刮开一半以上的格子报告给页面。
  const scratch = (points) => {
    const layout = layoutRef.current;
    eraseStroke(canvasRef.current.getContext('2d'), points, layout.size);
    scratchSamples(
      samplesRef.current,
      layout,
      revealed,
      points[0],
      points[points.length - 1],
    ).forEach(onReveal);
  };

  const onPointerDown = (event) => {
    if (!active || !layoutRef.current) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const point = pointerPoint(canvasRef.current, event);
    const points = [point];
    strokeRef.current = { id: event.pointerId, points };
    strokesRef.current.push(points);
    scratch(points);
  };

  const onPointerMove = (event) => {
    const stroke = strokeRef.current;
    if (!active || !stroke || stroke.id !== event.pointerId) return;
    const point = pointerPoint(canvasRef.current, event);
    const last = stroke.points[stroke.points.length - 1];
    stroke.points.push(point);
    scratch([last, point]);
  };

  const onPointerEnd = (event) => {
    if (strokeRef.current?.id === event.pointerId) strokeRef.current = null;
  };

  const shownPrice = ticket ? ticket.price : price;
  return (
    <div className='scratch-card'>
      <div className='scratch-card-head'>
        <Ticket size={16} />
        <span className='scratch-card-title'>{title}</span>
        <span className='scratch-card-price'>
          {t('票价 {{price}} {{symbol}}', { price: shownPrice, symbol })}
        </span>
      </div>
      <div className='scratch-grid' ref={gridRef}>
        {Array.from({ length: CELL_COUNT }, (_, index) => {
          let className = 'scratch-cell';
          if (revealed[index]) className += ' is-revealed';
          if (matches[index]) className += ' is-match';
          return (
            <div
              key={index}
              ref={(element) => {
                cellRefs.current[index] = element;
              }}
              className={className}
              aria-hidden={!revealed[index]}
            >
              {ticket && (
                <>
                  <strong>{ticket.cells[index]}</strong>
                  <small>{symbol}</small>
                </>
              )}
            </div>
          );
        })}
        <canvas
          ref={canvasRef}
          className={active ? 'scratch-coating is-active' : 'scratch-coating'}
          aria-hidden
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerEnd}
          onPointerCancel={onPointerEnd}
        />
      </div>
      <div className='scratch-card-foot'>
        {t('刮出 3 个相同的金额，就中这个金额')}
      </div>
    </div>
  );
};

export default ScratchCard;
