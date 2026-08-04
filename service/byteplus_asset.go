package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/bytepluserr"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/credentials"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/session"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/universal"
)

// BytePlus Ark `GroupType` values used by the asset library API.
const (
	// ByteplusGroupTypeAIGC is the private virtual avatar (AIGC) library — the existing default.
	ByteplusGroupTypeAIGC = "AIGC"
	// ByteplusGroupTypeLivenessFace is the real-human portrait library, gated behind H5 liveness verification.
	ByteplusGroupTypeLivenessFace = "LivenessFace"
)

// ByteplusAssetConfig holds credentials for BytePlus Asset API calls.
type ByteplusAssetConfig struct {
	AK          string
	SK          string
	Region      string // e.g. "ap-southeast-1"
	ProjectName string // e.g. "default"
}

func newByteplusSession(cfg ByteplusAssetConfig) (*session.Session, error) {
	config := byteplus.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(cfg.AK, cfg.SK, "")).
		WithRegion(cfg.Region)
	return session.NewSession(config)
}

func byteplusCall(cfg ByteplusAssetConfig, action string, body map[string]interface{}) (*map[string]interface{}, error) {
	sess, err := newByteplusSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("create byteplus session: %w", err)
	}
	resp, err := universal.New(sess).DoCall(
		universal.RequestUniversal{
			ServiceName: "ark",
			Action:      action,
			Version:     "2024-01-01",
			HttpMethod:  universal.POST,
			ContentType: universal.ApplicationJSON,
		},
		&body,
	)
	if err != nil {
		return nil, fmt.Errorf("byteplus %s: %w", action, err)
	}
	return resp, nil
}

// ByteplusRawAction transparently forwards an arbitrary Action + request body
// to BytePlus Ark and returns the unprocessed raw response map (including the
// genuine ResponseMetadata BytePlus returns), for callers that need the
// official response shape verbatim — e.g. the Seedance asset-library
// official-mirror endpoint. Unlike the ByteplusXxx wrapper functions in this
// file, it does not narrow the request to named parameters, so
// SortBy/SortOrder/per-call ProjectName and any other officially-supported
// field are preserved.
func ByteplusRawAction(cfg ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
	resp, err := byteplusCall(cfg, action, body)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("byteplus %s: nil response", action)
	}
	return *resp, nil
}

// parseResponse converts the SDK response map to a typed struct via JSON round-trip.
// If the response contains a "Result" key (BytePlus SDK envelope), it unwraps that level first.
func parseResponse(resp *map[string]interface{}, target interface{}) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	m := *resp
	// Unwrap "Result" envelope if present
	if result, ok := m["Result"]; ok {
		if rm, ok := result.(map[string]interface{}); ok {
			m = rm
		}
	}
	data, err := common.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	return common.Unmarshal(data, target)
}

// ---------- Asset Group ----------

// ByteplusAssetGroupInfo represents a BytePlus asset group.
type ByteplusAssetGroupInfo struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	Title       string `json:"Title"`
	Description string `json:"Description"`
	GroupType   string `json:"GroupType"`
	ProjectName string `json:"ProjectName"`
	CreateTime  string `json:"CreateTime"`
	UpdateTime  string `json:"UpdateTime"`
}

// ByteplusCreateAssetGroup creates an asset group of the given type and returns the group ID.
// groupType must be one of ByteplusGroupTypeAIGC or ByteplusGroupTypeLivenessFace.
func ByteplusCreateAssetGroup(cfg ByteplusAssetConfig, name, description, groupType string) (string, error) {
	if groupType == "" {
		groupType = ByteplusGroupTypeAIGC
	}
	body := map[string]interface{}{
		"Name":        name,
		"Description": description,
		"GroupType":   groupType,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "CreateAssetGroup", body)
	if err != nil {
		return "", err
	}

	// Try extracting Id from top level first, then from nested Result
	groupId := extractStringField(resp, "Id")
	if groupId == "" {
		return "", fmt.Errorf("CreateAssetGroup returned empty Id, raw: %v", resp)
	}
	return groupId, nil
}

// extractStringField tries to find a string field by key in the response map.
// It checks the top level first, then looks inside "Result" if present.
func extractStringField(resp *map[string]interface{}, key string) string {
	if resp == nil {
		return ""
	}
	m := *resp
	// Top level
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// Nested inside "Result"
	if result, ok := m["Result"]; ok {
		if rm, ok := result.(map[string]interface{}); ok {
			if v, ok := rm[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// ByteplusListAssetGroups lists asset groups with pagination, filtered by group type.
// Pass empty groupType to default to AIGC for backward compatibility.
func ByteplusListAssetGroups(cfg ByteplusAssetConfig, groupType string, groupIds []string, pageNum, pageSize int) ([]ByteplusAssetGroupInfo, int, error) {
	if groupType == "" {
		groupType = ByteplusGroupTypeAIGC
	}
	filter := map[string]interface{}{
		"GroupType": groupType,
	}
	if len(groupIds) > 0 {
		filter["GroupIds"] = groupIds
	}
	body := map[string]interface{}{
		"Filter":     filter,
		"PageNumber": pageNum,
		"PageSize":   pageSize,
	}
	resp, err := byteplusCall(cfg, "ListAssetGroups", body)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Items      []ByteplusAssetGroupInfo `json:"Items"`
		TotalCount int                      `json:"TotalCount"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.TotalCount, nil
}

// ByteplusDeleteAssetGroup deletes an asset group by ID.
func ByteplusDeleteAssetGroup(cfg ByteplusAssetConfig, groupId string) error {
	body := map[string]interface{}{
		"Id":          groupId,
		"ProjectName": cfg.ProjectName,
	}
	_, err := byteplusCall(cfg, "DeleteAssetGroup", body)
	return err
}

// ByteplusUpdateAssetGroup overwrites the Name / Description of an existing
// asset group on BytePlus side. Used after liveness verification creates a
// LivenessFace group with no name/description, so the gateway can backfill
// the user-supplied label into the BytePlus console.
//
// Empty name and description are skipped so callers can update one field at
// a time. ProjectName is always included since BytePlus scopes resources by
// project.
func ByteplusUpdateAssetGroup(cfg ByteplusAssetConfig, groupId, name, description string) error {
	if groupId == "" {
		return fmt.Errorf("UpdateAssetGroup: empty groupId")
	}
	body := map[string]interface{}{
		"Id":          groupId,
		"ProjectName": cfg.ProjectName,
	}
	if name != "" {
		body["Name"] = name
	}
	if description != "" {
		body["Description"] = description
	}
	if len(body) == 2 {
		// Only Id + ProjectName — nothing to update.
		return nil
	}
	_, err := byteplusCall(cfg, "UpdateAssetGroup", body)
	return err
}

// ---------- Asset ----------

// ByteplusAssetInfo represents a single BytePlus asset.
type ByteplusAssetInfo struct {
	Id          string          `json:"Id"`
	GroupId     string          `json:"GroupId"`
	Name        string          `json:"Name"`
	AssetType   string          `json:"AssetType"`       // Image, Video, Audio
	Status      string          `json:"Status"`          // Processing, Active, Failed
	URL         string          `json:"URL"`             // signed URL, valid 12 hrs
	Error       json.RawMessage `json:"Error,omitempty"` // populated when Status == "Failed"; raw JSON to preserve full upstream payload (e.g. {"Code":"...","Message":"..."})
	ProjectName string          `json:"ProjectName"`
	CreateTime  string          `json:"CreateTime"`
	UpdateTime  string          `json:"UpdateTime"`
}

// byteplusAPIURL returns the human-readable endpoint URL for a BytePlus ARK API
// action. The SDK constructs the actual URL internally; this helper is used
// only for logging so operators can correlate our logs with BytePlus audit logs.
func byteplusAPIURL(region, action string) string {
	if region == "" {
		region = "cn-beijing"
	}
	return fmt.Sprintf("https://open.volcengineapi.com/?Action=%s&Version=2024-01-01&Region=%s", action, region)
}

// ByteplusCreateAsset creates an asset in the specified group and returns the
// asset ID. ctx is used for structured logging only — the SDK does not accept
// context for cancellation.
//
// moderationStrategy controls content pre-filter behaviour. Pass "Skip" to
// bypass most non-baseline review policies (requires Secure Mode disabled on
// the BytePlus console). Any other value (including "") uses the default review.
func ByteplusCreateAsset(ctx context.Context, cfg ByteplusAssetConfig, groupId, imageURL, assetType, name, moderationStrategy string) (string, error) {
	if assetType == "" {
		assetType = "Image"
	}
	body := map[string]interface{}{
		"GroupId":     groupId,
		"URL":         imageURL,
		"AssetType":   assetType,
		"ProjectName": cfg.ProjectName,
	}
	if name != "" {
		body["Name"] = name
	}
	if moderationStrategy == "Skip" {
		body["Moderation"] = map[string]interface{}{"Strategy": "Skip"}
	}

	apiURL := byteplusAPIURL(cfg.Region, "CreateAsset")
	if bodyJSON, merr := common.Marshal(body); merr == nil {
		logger.LogInfo(ctx, fmt.Sprintf("[ByteplusCreateAsset] REQUEST url=%s body=%s", apiURL, string(bodyJSON)))
	}

	resp, err := byteplusCall(cfg, "CreateAsset", body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[ByteplusCreateAsset] RESPONSE ERROR url=%s err=%s", apiURL, err.Error()))
		return "", err
	}

	if respJSON, merr := common.Marshal(resp); merr == nil {
		logger.LogInfo(ctx, fmt.Sprintf("[ByteplusCreateAsset] RESPONSE OK url=%s body=%s", apiURL, string(respJSON)))
	}

	assetId := extractStringField(resp, "Id")
	if assetId == "" {
		return "", fmt.Errorf("CreateAsset returned empty Id, raw: %v", resp)
	}
	return assetId, nil
}

// ByteplusGetAsset retrieves a single asset's info.
func ByteplusGetAsset(cfg ByteplusAssetConfig, assetId string) (*ByteplusAssetInfo, error) {
	body := map[string]interface{}{
		"Id":          assetId,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "GetAsset", body)
	if err != nil {
		return nil, err
	}
	var result ByteplusAssetInfo
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ByteplusListAssets lists assets with optional group and status filtering, filtered by group type.
// Pass empty groupType to default to AIGC for backward compatibility.
func ByteplusListAssets(cfg ByteplusAssetConfig, groupType, groupId string, statuses []string, pageNum, pageSize int) ([]ByteplusAssetInfo, int, error) {
	if groupType == "" {
		groupType = ByteplusGroupTypeAIGC
	}
	filter := map[string]interface{}{
		"GroupType": groupType,
	}
	if groupId != "" {
		filter["GroupIds"] = []string{groupId}
	}
	if len(statuses) > 0 {
		filter["Statuses"] = statuses
	}
	body := map[string]interface{}{
		"Filter":     filter,
		"PageNumber": pageNum,
		"PageSize":   pageSize,
		"SortBy":     "CreateTime",
		"SortOrder":  "Desc",
	}
	resp, err := byteplusCall(cfg, "ListAssets", body)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Items      []ByteplusAssetInfo `json:"Items"`
		TotalCount int                 `json:"TotalCount"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.TotalCount, nil
}

// ByteplusDeleteAsset deletes an asset by ID.
func ByteplusDeleteAsset(cfg ByteplusAssetConfig, assetId string) error {
	body := map[string]interface{}{
		"Id":          assetId,
		"ProjectName": cfg.ProjectName,
	}
	_, err := byteplusCall(cfg, "DeleteAsset", body)
	return err
}

// ByteplusUpdateAsset updates the Name of an existing asset.
// Only non-empty name is sent; ProjectName is always included.
func ByteplusUpdateAsset(cfg ByteplusAssetConfig, assetId, name string) error {
	if assetId == "" {
		return fmt.Errorf("ByteplusUpdateAsset: empty assetId")
	}
	body := map[string]interface{}{
		"Id":          assetId,
		"ProjectName": cfg.ProjectName,
	}
	if name != "" {
		body["Name"] = name
	}
	_, err := byteplusCall(cfg, "UpdateAsset", body)
	return err
}

// ByteplusStatusToInternal maps BytePlus asset statuses to the gateway internal statuses.
func ByteplusStatusToInternal(bpStatus string) string {
	switch bpStatus {
	case "Active":
		return "active"
	case "Processing":
		return "pending"
	case "Failed":
		return "failed"
	default:
		return "pending"
	}
}

// ---------- Real-human portrait library: H5 liveness verification ----------

// ByteplusCreateVisualValidateSession launches a BytePlus liveness verification session
// and returns the H5 page link plus a `BytedToken` used to fetch the resulting GroupId
// once the end user finishes verification.
//
// The CallbackURL is invoked by BytePlus with `?bytedToken=...&resultCode=10000` appended
// after verification completes; the gateway must persist the original BytedToken so it can
// later call ByteplusGetVisualValidateResult to materialize the new asset group.
func ByteplusCreateVisualValidateSession(cfg ByteplusAssetConfig, callbackURL string) (h5Link, bytedToken string, err error) {
	body := map[string]interface{}{
		"CallbackURL": callbackURL,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "CreateVisualValidateSession", body)
	if err != nil {
		return "", "", err
	}
	bytedToken = extractStringField(resp, "BytedToken")
	h5Link = extractStringField(resp, "H5Link")
	if bytedToken == "" {
		return "", "", fmt.Errorf("CreateVisualValidateSession returned empty BytedToken, raw: %v", resp)
	}
	if h5Link == "" {
		return "", "", fmt.Errorf("CreateVisualValidateSession returned empty H5Link, raw: %v", resp)
	}
	// Force Simplified Chinese on the H5 page. BytePlus's H5 reads either `lang`
	// (per their public docs) or `lng` (seen in some links they hand back); we
	// set both to be robust to upstream changes. We also intentionally *override*
	// any pre-existing lang/lng so users always see zh-CN on first paint.
	h5Link = ForceH5LinkChinese(h5Link)
	return h5Link, bytedToken, nil
}

// ForceH5LinkChinese rewrites the BytePlus liveness H5 link so the verification
// page defaults to Simplified Chinese instead of inheriting the browser locale
// (which has been observed to land on English even though the docs claim `zh`
// is the default). On parse failure we fall back to a string append so the
// caller still gets a usable link. Exported so the Seedance official-mirror
// endpoint (which calls ByteplusRawAction, bypassing ByteplusCreateVisualValidateSession)
// can apply the same rewrite to its raw H5Link.
func ForceH5LinkChinese(h5Link string) string {
	u, err := url.Parse(h5Link)
	if err != nil {
		// Best-effort fallback: append params raw.
		sep := "?"
		if u != nil && u.RawQuery != "" {
			sep = "&"
		}
		return h5Link + sep + "lang=zh-CN&lng=zh"
	}
	q := u.Query()
	q.Set("lang", "zh-CN")
	q.Set("lng", "zh")
	u.RawQuery = q.Encode()
	return u.String()
}

// ByteplusGetVisualValidateResult exchanges a `BytedToken` for the GroupId created by the
// liveness verification session. Must be called after the end user finishes the H5 flow
// (and within 120 seconds of obtaining the token, per BytePlus docs).
func ByteplusGetVisualValidateResult(cfg ByteplusAssetConfig, bytedToken string) (groupId string, err error) {
	body := map[string]interface{}{
		"BytedToken":  bytedToken,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "GetVisualValidateResult", body)
	if err != nil {
		return "", err
	}
	groupId = extractStringField(resp, "GroupId")
	if groupId == "" {
		return "", fmt.Errorf("GetVisualValidateResult returned empty GroupId, raw: %v", resp)
	}
	return groupId, nil
}

// ---------- Moderation Block Reason Query ----------

// BytePlus `GetModerationResult` Id types — used to tell the upstream how to
// interpret the supplied `Id` (PDF p.2).
const (
	ByteplusModerationIdTypeTaskId    = "task_id"
	ByteplusModerationIdTypeAssetId   = "asset_id"
	ByteplusModerationIdTypeRequestId = "request_id"
)

// ByteplusModerationBlockReason represents a single content moderation block
// reason returned by the upstream `GetModerationResult` API.
//
// `Label` is one of: Safety / Copyright / Celebrity / Deepfake.
// `SubLabel` is a finer-grained classification (e.g. IP / Other / RealHuman).
// `Detail` is a free-form description that may include matched IP / public
// figure names (e.g. "Spider-Man: Homecoming-Peter Parker").
type ByteplusModerationBlockReason struct {
	Label    string `json:"label"`
	SubLabel string `json:"sub_label"`
	Detail   string `json:"detail"`
}

// ByteplusGetModerationResult queries the upstream BytePlus Ark
// `GetModerationResult` API for content moderation block reasons of a single
// task / asset / request ID. The caller must have whitelist access on the
// BytePlus side; otherwise the upstream returns 404 NotFound.Id.
//
// `idType` must be one of ByteplusModerationIdType{TaskId,AssetId,RequestId}.
// Empty `idType` defaults to TaskId.
func ByteplusGetModerationResult(cfg ByteplusAssetConfig, id, idType string) ([]ByteplusModerationBlockReason, error) {
	if id == "" {
		return nil, fmt.Errorf("GetModerationResult: empty id")
	}
	if idType == "" {
		idType = ByteplusModerationIdTypeTaskId
	}
	body := map[string]interface{}{
		"Id":          id,
		"Type":        idType,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "GetModerationResult", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		BlockReasons []ByteplusModerationBlockReason `json:"block_reasons"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return result.BlockReasons, nil
}

// ByteplusGetModerationResultRaw queries the upstream BytePlus Ark
// `GetModerationResult` API and returns the complete upstream response body as
// provided by the SDK map envelope, preserving top-level metadata fields.
func ByteplusGetModerationResultRaw(cfg ByteplusAssetConfig, id, idType string) (map[string]interface{}, error) {
	if id == "" {
		return nil, fmt.Errorf("GetModerationResult: empty id")
	}
	if idType == "" {
		idType = ByteplusModerationIdTypeTaskId
	}
	body := map[string]interface{}{
		"Id":          id,
		"Type":        idType,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "GetModerationResult", body)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("GetModerationResult: nil response")
	}
	return *resp, nil
}

// IsByteplusNotFoundError reports whether the given upstream error corresponds
// to a `NotFound.Id` failure (HTTP 404). For `GetModerationResult` this maps to
// any of the three documented cases (PDF p.3):
//   - invalid ID
//   - request was not blocked by moderation
//   - request exceeds the 14-day query range
//
// Callers should surface a friendly, ambiguous message to the user rather than
// guessing which sub-case applies.
func IsByteplusNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var reqFailure bytepluserr.RequestFailure
	if errors.As(err, &reqFailure) {
		if reqFailure.StatusCode() == 404 {
			return true
		}
		if reqFailure.Code() == "NotFound.Id" {
			return true
		}
	}
	var bpErr bytepluserr.Error
	if errors.As(err, &bpErr) && bpErr.Code() == "NotFound.Id" {
		return true
	}
	return false
}
