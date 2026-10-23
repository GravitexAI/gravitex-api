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

it.each(['', 'DeepSeek V4.1 Flash'])(
  'loads the nickname in field order and saves the edited value %j',
  async (editedNickname) => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({
        matches: false,
        addEventListener() {},
        removeEventListener() {},
        addListener() {},
        removeListener() {},
      })),
    );
    const i18n = createInstance();
    await i18n.init({
      lng: 'zh-CN',
      resources: { 'zh-CN': zhCNTranslation },
    });
    API.get.mockImplementation(async (url) => {
      if (url === '/api/models/7') {
        return {
          data: {
            success: true,
            data: {
              id: 7,
              model_name: 'deepseek-v4.1',
              model_nick_name: 'DeepSeek V4.1',
              name_rule: 0,
              status: 1,
              sync_official: 0,
            },
          },
        };
      }
      return { data: { success: true, data: { items: [] } } };
    });
    API.put.mockResolvedValue({ data: { success: true } });
    render(
      <I18nextProvider i18n={i18n}>
        <EditModelModal
          visiable
          editingModel={{ id: 7 }}
          refresh={() => {}}
          handleClose={() => {}}
        />
      </I18nextProvider>,
    );
    const nickname = await screen.findByLabelText('模型昵称');
    await waitFor(() => expect(nickname.value).toBe('DeepSeek V4.1'));
    const name = screen.getByDisplayValue('deepseek-v4.1');
    expect(
      name.compareDocumentPosition(nickname) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      nickname.compareDocumentPosition(screen.getByText('名称匹配类型')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.change(nickname, { target: { value: editedNickname } });
    fireEvent.click(screen.getByRole('button', { name: '提交' }));
    await waitFor(() =>
      expect(API.put).toHaveBeenCalledWith(
        '/api/models/',
        expect.objectContaining({ id: 7, model_nick_name: editedNickname }),
      ),
    );
  },
);
