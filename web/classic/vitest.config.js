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

import path from 'node:path';
import { createRequire } from 'node:module';
import { defineConfig } from 'vitest/config';

const require = createRequire(import.meta.url);
const semiUiDir = path.resolve(
  path.dirname(require.resolve('@douyinfe/semi-ui')),
  '../..',
);

export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/__tests__/*.test.jsx'],
    server: { deps: { inline: [/@douyinfe/, /date-fns-tz/] } },
  },
  resolve: {
    alias: {
      // Match the date-fns v2 compatibility alias used by the classic build.
      'date-fns': path.resolve(
        semiUiDir,
        '../semi-foundation/node_modules/date-fns',
      ),
      'date-fns-tz': path.resolve(
        path.dirname(require.resolve('date-fns-tz')),
        'esm/index.js',
      ),
    },
  },
});
