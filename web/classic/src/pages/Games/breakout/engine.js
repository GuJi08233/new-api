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

// 与 pkg/breakout 使用相同的整数物理规则；变更后需更新并验证 golden 重放。
export const WIDTH = 420;
export const HEIGHT = 540;
export const FPS = 60;
export const MAX_TICKS = 5 * 60 * FPS;
export const RADIUS = 5;
export const PADDLE_Y = 490;
export const COLUMNS = 8;
export const ROWS = 6;
export const BRICK_WIDTH = 44;
export const BRICK_HEIGHT = 18;
export const BRICK_GAP = 5;
export const BRICK_LEFT = 16;
export const BRICK_TOP = 64;

export function createGame(seed) {
  let random = seed >>> 0 || 1;
  const bricks = Array.from({ length: ROWS * COLUMNS }, () => {
    random ^= random << 13;
    random ^= random >>> 17;
    random ^= random << 5;
    random >>>= 0;
    return random % 7 === 0 ? 2 : 1;
  });
  return {
    bricks,
    balls: [{ x: WIDTH / 2, y: PADDLE_Y - RADIUS - 1, vx: 3, vy: -4 }],
    paddle: WIDTH / 2,
    lives: 3,
    score: 0,
    broken: 0,
    ticks: 0,
    wait: 60,
    wide: 0,
    over: false,
    won: false,
  };
}

export function step(game, target) {
  if (game.over) return;
  game.ticks++;
  let half = 36;
  if (game.wide > 0) {
    half = 56;
    game.wide--;
  }
  game.paddle = Math.max(half, Math.min(WIDTH - half, target));
  if (game.wait > 0) {
    game.wait--;
    game.balls[0].x = game.paddle;
  } else {
    const next = [];
    const extras = [];
    for (const current of game.balls) {
      const ball = { ...current };
      const oldY = ball.y;
      ball.x += ball.vx;
      ball.y += ball.vy;
      if (ball.x < RADIUS) {
        ball.x = RADIUS;
        ball.vx = -ball.vx;
      }
      if (ball.x > WIDTH - RADIUS) {
        ball.x = WIDTH - RADIUS;
        ball.vx = -ball.vx;
      }
      if (ball.y < RADIUS) {
        ball.y = RADIUS;
        ball.vy = -ball.vy;
      }
      if (
        ball.vy > 0 &&
        oldY + RADIUS <= PADDLE_Y &&
        ball.y + RADIUS >= PADDLE_Y &&
        ball.x >= game.paddle - half - RADIUS &&
        ball.x <= game.paddle + half + RADIUS
      ) {
        ball.y = PADDLE_Y - RADIUS;
        ball.vx = Math.max(
          -4,
          Math.min(4, Math.trunc(((ball.x - game.paddle) * 4) / half)),
        );
        if (ball.vx === 0) ball.vx = 1;
        ball.vy = -4;
      }
      for (let i = 0; i < game.bricks.length; i++) {
        if (!game.bricks[i]) continue;
        const x = BRICK_LEFT + (i % COLUMNS) * (BRICK_WIDTH + BRICK_GAP);
        const y =
          BRICK_TOP + Math.floor(i / COLUMNS) * (BRICK_HEIGHT + BRICK_GAP);
        if (
          ball.x + RADIUS <= x ||
          ball.x - RADIUS >= x + BRICK_WIDTH ||
          ball.y + RADIUS <= y ||
          ball.y - RADIUS >= y + BRICK_HEIGHT
        )
          continue;
        if (oldY + RADIUS <= y) {
          ball.y = y - RADIUS;
          ball.vy = -ball.vy;
        } else if (oldY - RADIUS >= y + BRICK_HEIGHT) {
          ball.y = y + BRICK_HEIGHT + RADIUS;
          ball.vy = -ball.vy;
        } else {
          ball.x = ball.vx > 0 ? x - RADIUS : x + BRICK_WIDTH + RADIUS;
          ball.vx = -ball.vx;
        }
        game.bricks[i]--;
        game.score += 5;
        if (!game.bricks[i]) {
          game.broken++;
          game.score += 10;
          if (i % 11 === 0) game.wide = FPS * 15;
          if (i % 13 === 0 && game.balls.length + extras.length < 3)
            extras.push({ x: ball.x, y: ball.y, vx: -ball.vx, vy: -4 });
        }
        break;
      }
      if (ball.y - RADIUS < HEIGHT) next.push(ball);
    }
    game.balls = [...next, ...extras];
    if (game.broken === ROWS * COLUMNS) {
      game.won = true;
      game.over = true;
      game.score += 100;
    } else if (!game.balls.length) {
      game.lives--;
      if (!game.lives) game.over = true;
      else {
        game.wait = 60;
        game.balls = [
          { x: game.paddle, y: PADDLE_Y - RADIUS - 1, vx: 3, vy: -4 },
        ];
      }
    }
  }
  if (game.ticks >= MAX_TICKS) game.over = true;
}
