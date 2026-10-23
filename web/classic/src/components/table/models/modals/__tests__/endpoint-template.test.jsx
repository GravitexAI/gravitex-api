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
import { afterEach, expect, it, vi } from 'vitest';
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { createInstance } from 'i18next';
import zhCNTranslation from '../../../../../i18n/locales/zh-CN.json';
import EditModelModal from '../EditModelModal';
import { API } from '../../../../../helpers';

vi.mock('lottie-web', () => ({ default: { loadAnimation: vi.fn() } }));

vi.mock('../../../../../helpers', () => ({
  API: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

it('fills and saves Gemini Interactions alongside the existing Gemini endpoint', async () => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
    })),
  );
  const i18n = createInstance();
  await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': zhCNTranslation } });
  API.get.mockResolvedValue({ data: { success: true, data: { items: [] } } });
  API.post.mockResolvedValue({ data: { success: true } });
  render(
    <I18nextProvider i18n={i18n}>
      <EditModelModal
        visiable
        editingModel={{ model_name: 'gemini-test' }}
        refresh={() => {}}
        handleClose={() => {}}
      />
    </I18nextProvider>,
  );
  fireEvent.click(await screen.findByRole('button', { name: '填入模板' }));
  expect(API.post).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '提交' }));
  await waitFor(() => expect(API.post).toHaveBeenCalled());
  expect(JSON.parse(API.post.mock.calls[0][1].endpoints)).toMatchObject({
    gemini: { path: '/v1beta/models/{model}:generateContent', method: 'POST' },
    'gemini-interactions': { path: '/v1beta/interactions', method: 'POST' },
  });
});
