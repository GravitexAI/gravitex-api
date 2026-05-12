package system_setting

import "github.com/QuantumNous/new-api/common"

// ByteplusAssetGroupLimit caps the number of asset groups (AIGC + LivenessFace combined)
// a single user is allowed to create on a single upstream BytePlus channel.
//
// BytePlus does not document an explicit limit, but upstream resources are shared across
// all gateway users routed to the same channel, so we cap defensively. The limit can be
// overridden via the BYTEPLUS_ASSET_GROUP_LIMIT environment variable at startup.
var ByteplusAssetGroupLimit = 100

// initialized lazily so tests can adjust the value without env vars
var byteplusAssetInited = false

// GetByteplusAssetGroupLimit returns the current per-user-per-channel asset group cap.
func GetByteplusAssetGroupLimit() int {
	if !byteplusAssetInited {
		// Allow ENV override on first read (covers test/process startup ordering).
		ByteplusAssetGroupLimit = common.GetEnvOrDefault("BYTEPLUS_ASSET_GROUP_LIMIT", ByteplusAssetGroupLimit)
		byteplusAssetInited = true
	}
	if ByteplusAssetGroupLimit <= 0 {
		return ByteplusAssetGroupLimit
	}
	return ByteplusAssetGroupLimit
}
