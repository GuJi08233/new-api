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

// 像素鸟规则引擎，与服务端 pkg/flappy/flappy.go 逐条对应。浏览器按帧记下每次拍翅膀，结束时上传，
// 服务端用同一个种子重放整局来结算成绩和奖励。两边任何规则差异都会让结算结果和玩家看到的不一致：
// 改规则时必须两边一起改，并用 pkg/flappy/testdata/generate_golden.mjs 重新生成跨端对照记录。
// 只用整数运算，随机数是与服务端逐位一致的 mulberry32。
//
// 尺寸与距离以像素为单位；状态里的坐标与速度以单位计，1 像素 = SCALE 单位，渲染时除以 SCALE。

export const TICKS_PER_SECOND = 60;
// 一局的游戏内时长上限(1 小时)：到了就要结束并提交，更长的记录服务端整局拒绝。
export const MAX_TICKS = 60 * 60 * TICKS_PER_SECOND;

export const SCALE = 100;
// 逻辑画布 288×512，地面上沿在 400，下面 112 像素是地面。
export const SCREEN_WIDTH = 288;
export const SCREEN_HEIGHT = 512;
export const GROUND_Y = 400;

// 鸟的碰撞盒：左沿固定在 BIRD_X，开局上沿在 BIRD_START_Y(可玩区域正中)。
export const BIRD_X = 80;
export const BIRD_WIDTH = 34;
export const BIRD_HEIGHT = 24;
export const BIRD_START_Y = Math.floor((GROUND_Y - BIRD_HEIGHT) / 2);

// 速度以单位/帧计，向下为正：每帧重力加 GRAVITY，最快下落 MAX_FALL_SPEED，拍翅膀直接设为 FLAP_VELOCITY。
export const GRAVITY = 25;
export const FLAP_VELOCITY = -460;
export const MAX_FALL_SPEED = 1000;

// 管道宽 PIPE_WIDTH、缺口高 PIPE_GAP，相邻管道间距 PIPE_SPACING，每帧左移 PIPE_SPEED；
// 第一根的左沿在 FIRST_PIPE_X，缺口上沿在 [GAP_MIN_TOP, GAP_MAX_TOP] 之间随机取整数像素。
export const PIPE_WIDTH = 52;
export const PIPE_GAP = 120;
export const PIPE_SPACING = 180;
export const PIPE_SPEED = 2;
export const FIRST_PIPE_X = 400;
export const GAP_MIN_TOP = 60;
export const GAP_MAX_TOP = GROUND_Y - PIPE_GAP - 60;

// 任意 FLAP_WINDOW_TICKS 帧内拍翅膀超过 HUMAN_FLAP_LIMIT 次，服务端判定为脚本、不发奖励。
export const FLAP_WINDOW_TICKS = TICKS_PER_SECOND;
export const HUMAN_FLAP_LIMIT = 12;

// 规则常量原样导出，只给 generate_golden.mjs 写进跨端对照记录，服务端测试会逐项比对。
export const RULES = {
  ticksPerSecond: TICKS_PER_SECOND,
  maxTicks: MAX_TICKS,
  scale: SCALE,
  screenWidth: SCREEN_WIDTH,
  groundY: GROUND_Y,
  birdX: BIRD_X,
  birdWidth: BIRD_WIDTH,
  birdHeight: BIRD_HEIGHT,
  birdStartY: BIRD_START_Y,
  gravity: GRAVITY,
  flapVelocity: FLAP_VELOCITY,
  maxFallSpeed: MAX_FALL_SPEED,
  pipeWidth: PIPE_WIDTH,
  pipeGap: PIPE_GAP,
  pipeSpacing: PIPE_SPACING,
  pipeSpeed: PIPE_SPEED,
  firstPipeX: FIRST_PIPE_X,
  gapMinTop: GAP_MIN_TOP,
  gapMaxTop: GAP_MAX_TOP,
  flapWindowTicks: FLAP_WINDOW_TICKS,
  humanFlapLimit: HUMAN_FLAP_LIMIT,
};

// mulberry32：Math.imul 与 >>> 让每一步都是 32 位整数运算，与服务端的 uint32 实现逐位一致。
function createRandom(seed) {
  let state = seed | 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t = (t + Math.imul(t ^ (t >>> 7), t | 61)) ^ t;
    return (t ^ (t >>> 14)) >>> 0;
  };
}

export class FlappyGame {
  constructor(seed) {
    this.random = createRandom(seed);
    // 一对上下管道 { x: 左沿, gapTop: 缺口上沿, passed }，单位计，从左到右排列。
    this.pipes = [];
    // 鸟的上沿与速度，单位计，向下为正。
    this.y = BIRD_START_Y * SCALE;
    this.vy = 0;
    this.score = 0;
    // 已推进的帧数，从第一次拍翅膀那一帧算起，包括结束的那一帧。
    this.ticks = 0;
    this.flaps = 0;
    this.over = false;
    this.addPipe(FIRST_PIPE_X * SCALE);
  }

  // 第一次拍翅膀之后才开始：之前鸟悬停、管道不动。
  get started() {
    return this.ticks > 0;
  }

  // 在 x 处放一对管道，缺口上沿用一个随机数取整数像素。
  addPipe(x) {
    const top = GAP_MIN_TOP + (this.random() % (GAP_MAX_TOP - GAP_MIN_TOP + 1));
    this.pipes.push({ x, gapTop: top * SCALE, passed: false });
  }

  // 推进一帧，flap 表示这一帧拍了翅膀(一帧最多一次)。第一次拍翅膀之前什么都不变，帧数也不增加。
  // 每一帧依次是：拍翅膀或重力改变速度、鸟移动、管道左移(移出画面的删掉，最后一根进入画面后在它后面补一根)、
  // 计分、碰撞。拍翅膀要在调用之前按 game.ticks 记进上传的记录。
  tick(flap) {
    if (this.over || (this.ticks === 0 && !flap)) return;
    this.ticks++;
    if (flap) {
      this.vy = FLAP_VELOCITY;
      this.flaps++;
    } else {
      this.vy = Math.min(this.vy + GRAVITY, MAX_FALL_SPEED);
    }
    this.y += this.vy;

    for (const pipe of this.pipes) {
      pipe.x -= PIPE_SPEED * SCALE;
    }
    while (this.pipes.length > 0 && this.pipes[0].x + PIPE_WIDTH * SCALE < 0) {
      this.pipes.shift();
    }
    const last = this.pipes[this.pipes.length - 1];
    if (last.x < SCREEN_WIDTH * SCALE) {
      this.addPipe(last.x + PIPE_SPACING * SCALE);
    }
    for (const pipe of this.pipes) {
      if (!pipe.passed && pipe.x + PIPE_WIDTH * SCALE < BIRD_X * SCALE) {
        pipe.passed = true;
        this.score++;
      }
    }

    if (this.y < 0 || this.y + BIRD_HEIGHT * SCALE >= GROUND_Y * SCALE) {
      this.over = true;
      return;
    }
    for (const pipe of this.pipes) {
      const overlapX =
        pipe.x < (BIRD_X + BIRD_WIDTH) * SCALE &&
        BIRD_X * SCALE < pipe.x + PIPE_WIDTH * SCALE;
      const inGap =
        this.y >= pipe.gapTop &&
        this.y + BIRD_HEIGHT * SCALE <= pipe.gapTop + PIPE_GAP * SCALE;
      if (overlapX && !inGap) {
        this.over = true;
        return;
      }
    }
  }
}

// 记录格式与服务端 flappy.DecodeFlaps 对应：每个数是距上一次拍翅膀的帧数，第一次拍翅膀记在第 0 帧。
// 每帧最多记一次，在调用 tick(true) 之前记下当时的 game.ticks。
export class FlapRecorder {
  constructor() {
    this.flaps = [];
    this.lastTick = 0;
  }

  record(tick) {
    this.flaps.push(tick - this.lastTick);
    this.lastTick = tick;
  }
}
