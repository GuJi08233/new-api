/*
Copyright (C) 2023-2026 QuantumNous

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
// The only upstream pricing source (see `controller/ratio_sync.go`). It is a
// wire-protocol identifier and a brand name, so it is never translated.
export const UPSTREAM_PRICING_SOURCE = 'models.dev'

// Labels reuse the existing sentence-case i18n keys defined for form fields
// (e.g. `Model ratio`, `Audio completion ratio`). Do NOT switch to Title Case
// here without updating the i18n catalog; otherwise we end up with two keys per
// ratio type that only differ in capitalization.
export const RATIO_TYPE_OPTIONS = [
  { label: 'Model ratio', value: 'model_ratio' },
  { label: 'Completion ratio', value: 'completion_ratio' },
  { label: 'Cache ratio', value: 'cache_ratio' },
  { label: 'Create cache ratio', value: 'create_cache_ratio' },
  { label: 'Image ratio', value: 'image_ratio' },
  { label: 'Audio ratio', value: 'audio_ratio' },
  { label: 'Audio completion ratio', value: 'audio_completion_ratio' },
  { label: 'Fixed price', value: 'model_price' },
  { label: 'Expression billing', value: 'billing_expr' },
] as const
