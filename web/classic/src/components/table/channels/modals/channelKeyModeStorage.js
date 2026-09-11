const DEFAULT_KEY_MODE = 'append';
const STORAGE_PREFIX = 'channel_key_mode_';

const isValidKeyMode = (mode) => mode === 'append' || mode === 'replace';

export function getStoredChannelKeyMode(channelId, storage = globalThis.localStorage) {
  if (!channelId || !storage) {
    return DEFAULT_KEY_MODE;
  }

  try {
    const mode = storage.getItem(`${STORAGE_PREFIX}${channelId}`);
    return isValidKeyMode(mode) ? mode : DEFAULT_KEY_MODE;
  } catch {
    return DEFAULT_KEY_MODE;
  }
}

export function setStoredChannelKeyMode(
  channelId,
  mode,
  storage = globalThis.localStorage,
) {
  if (!channelId || !storage) {
    return;
  }

  try {
    storage.setItem(
      `${STORAGE_PREFIX}${channelId}`,
      isValidKeyMode(mode) ? mode : DEFAULT_KEY_MODE,
    );
  } catch {
    // Ignore storage failures such as private browsing or disabled storage.
  }
}
