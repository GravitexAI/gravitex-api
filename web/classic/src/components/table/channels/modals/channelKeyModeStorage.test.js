import assert from 'node:assert/strict';
import {
  getStoredChannelKeyMode,
  setStoredChannelKeyMode,
} from './channelKeyModeStorage.js';

const values = new Map();
const storage = {
  getItem: (key) => values.get(key) ?? null,
  setItem: (key, value) => values.set(key, value),
};

assert.equal(getStoredChannelKeyMode(320, storage), 'append');
setStoredChannelKeyMode(320, 'replace', storage);
assert.equal(getStoredChannelKeyMode(320, storage), 'replace');
setStoredChannelKeyMode(320, 'invalid', storage);
assert.equal(getStoredChannelKeyMode(320, storage), 'append');
