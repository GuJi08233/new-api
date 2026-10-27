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

export const ITEMS_PER_PAGE = 10; // this value must keep same as the one defined in backend!

export const TABLE_COMPACT_MODES_KEY = 'table_compact_modes';

export const API_ENDPOINTS = [
  '/v1/chat/completions',
  '/v1/responses',
  '/v1/responses/compact',
  '/v1/messages',
  '/v1beta/models',
  '/v1/embeddings',
  '/v1/rerank',
  '/v1/images/generations',
  '/v1/images/edits',
  '/v1/images/variations',
  '/v1/audio/speech',
  '/v1/audio/transcriptions',
  '/v1/audio/translations',
];

export const TASK_ACTION_GENERATE = 'image_to_video';
export const TASK_ACTION_TEXT_GENERATE = 'text_to_video';
export const TASK_ACTION_FIRST_TAIL_GENERATE = 'first_tail_to_video';
export const TASK_ACTION_REFERENCE_GENERATE = 'reference_to_video';
export const TASK_ACTION_REMIX_GENERATE = 'remix';

// 升级前写入的任务仍是旧动作名，展示前统一映射到规范名
const LEGACY_TASK_ACTIONS = {
  generate: TASK_ACTION_GENERATE,
  textGenerate: TASK_ACTION_TEXT_GENERATE,
  firstTailGenerate: TASK_ACTION_FIRST_TAIL_GENERATE,
  referenceGenerate: TASK_ACTION_REFERENCE_GENERATE,
  remixGenerate: TASK_ACTION_REMIX_GENERATE,
};

export const normalizeTaskAction = (action) =>
  LEGACY_TASK_ACTIONS[action] || action;
