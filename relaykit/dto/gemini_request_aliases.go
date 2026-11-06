package dto

import (
	"encoding/json"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *FunctionCallingConfig) UnmarshalJSON(data []byte) error {
	type Alias FunctionCallingConfig
	var aux struct {
		Alias
		AllowedFunctionNamesSnake        []string `json:"allowed_function_names,omitempty"`
		StreamFunctionCallArgumentsSnake *bool    `json:"stream_function_call_arguments,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = FunctionCallingConfig(aux.Alias)
	if len(aux.AllowedFunctionNamesSnake) > 0 {
		v.AllowedFunctionNames = aux.AllowedFunctionNamesSnake
	}
	if aux.StreamFunctionCallArgumentsSnake != nil {
		v.StreamFunctionCallArguments = aux.StreamFunctionCallArgumentsSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *RetrievalConfig) UnmarshalJSON(data []byte) error {
	type Alias RetrievalConfig
	var aux struct {
		Alias
		LatLngSnake       *LatLng `json:"lat_lng,omitempty"`
		LanguageCodeSnake string  `json:"language_code,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = RetrievalConfig(aux.Alias)
	if aux.LatLngSnake != nil {
		v.LatLng = aux.LatLngSnake
	}
	if aux.LanguageCodeSnake != "" {
		v.LanguageCode = aux.LanguageCodeSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *FunctionCall) UnmarshalJSON(data []byte) error {
	type Alias FunctionCall
	var aux struct {
		Alias
		PartialArgsSnake  []GeminiPartialArg `json:"partial_args,omitempty"`
		WillContinueSnake *bool              `json:"will_continue,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = FunctionCall(aux.Alias)
	if len(aux.PartialArgsSnake) > 0 {
		v.PartialArgs = aux.PartialArgsSnake
	}
	if aux.WillContinueSnake != nil {
		v.WillContinue = aux.WillContinueSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *GeminiPartialArg) UnmarshalJSON(data []byte) error {
	type Alias GeminiPartialArg
	var aux struct {
		Alias
		JSONPathSnake     string          `json:"json_path,omitempty"`
		NumberValueSnake  *float64        `json:"number_value,omitempty"`
		StringValueSnake  *string         `json:"string_value,omitempty"`
		BoolValueSnake    *bool           `json:"bool_value,omitempty"`
		NullValueSnake    json.RawMessage `json:"null_value,omitempty"`
		WillContinueSnake *bool           `json:"will_continue,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = GeminiPartialArg(aux.Alias)
	if aux.JSONPathSnake != "" {
		v.JSONPath = aux.JSONPathSnake
	}
	if aux.NumberValueSnake != nil {
		v.NumberValue = aux.NumberValueSnake
	}
	if aux.StringValueSnake != nil {
		v.StringValue = aux.StringValueSnake
	}
	if aux.BoolValueSnake != nil {
		v.BoolValue = aux.BoolValueSnake
	}
	if len(aux.NullValueSnake) > 0 {
		v.NullValue = aux.NullValueSnake
	}
	if aux.WillContinueSnake != nil {
		v.WillContinue = aux.WillContinueSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *GeminiFunctionResponse) UnmarshalJSON(data []byte) error {
	type Alias GeminiFunctionResponse
	var aux struct {
		Alias
		WillContinueSnake json.RawMessage `json:"will_continue,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = GeminiFunctionResponse(aux.Alias)
	if len(aux.WillContinueSnake) > 0 {
		v.WillContinue = aux.WillContinueSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *GeminiFileData) UnmarshalJSON(data []byte) error {
	type Alias GeminiFileData
	var aux struct {
		Alias
		MimeTypeSnake string `json:"mime_type,omitempty"`
		FileUriSnake  string `json:"file_uri,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = GeminiFileData(aux.Alias)
	if aux.MimeTypeSnake != "" {
		v.MimeType = aux.MimeTypeSnake
	}
	if aux.FileUriSnake != "" {
		v.FileUri = aux.FileUriSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *GeminiChatTool) UnmarshalJSON(data []byte) error {
	type Alias GeminiChatTool
	var aux struct {
		Alias
		GoogleSearchSnake          any             `json:"google_search,omitempty"`
		GoogleSearchRetrievalSnake any             `json:"google_search_retrieval,omitempty"`
		GoogleMapsSnake            json.RawMessage `json:"google_maps,omitempty"`
		EnterpriseWebSearchSnake   json.RawMessage `json:"enterprise_web_search,omitempty"`
		CodeExecutionSnake         any             `json:"code_execution,omitempty"`
		FunctionDeclarationsSnake  any             `json:"function_declarations,omitempty"`
		URLContextSnake            any             `json:"url_context,omitempty"`
		FileSearchSnake            json.RawMessage `json:"file_search,omitempty"`
		ComputerUseSnake           json.RawMessage `json:"computer_use,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = GeminiChatTool(aux.Alias)
	if aux.GoogleSearchSnake != nil {
		v.GoogleSearch = aux.GoogleSearchSnake
	}
	if aux.GoogleSearchRetrievalSnake != nil {
		v.GoogleSearchRetrieval = aux.GoogleSearchRetrievalSnake
	}
	if len(aux.GoogleMapsSnake) > 0 {
		v.GoogleMaps = aux.GoogleMapsSnake
	}
	if len(aux.EnterpriseWebSearchSnake) > 0 {
		v.EnterpriseWebSearch = aux.EnterpriseWebSearchSnake
	}
	if aux.CodeExecutionSnake != nil {
		v.CodeExecution = aux.CodeExecutionSnake
	}
	if aux.FunctionDeclarationsSnake != nil {
		if declarations, ok := aux.FunctionDeclarationsSnake.([]any); !ok || len(declarations) > 0 {
			v.FunctionDeclarations = aux.FunctionDeclarationsSnake
		}
	}
	if aux.URLContextSnake != nil {
		v.URLContext = aux.URLContextSnake
	}
	if len(aux.FileSearchSnake) > 0 {
		v.FileSearch = aux.FileSearchSnake
	}
	if len(aux.ComputerUseSnake) > 0 {
		v.ComputerUse = aux.ComputerUseSnake
	}
	return nil
}

// UnmarshalJSON accepts snake_case aliases while preserving camelCase output.
func (v *GeminiEmbeddingRequest) UnmarshalJSON(data []byte) error {
	type Alias GeminiEmbeddingRequest
	var aux struct {
		Alias
		TaskTypeSnake             string `json:"task_type,omitempty"`
		OutputDimensionalitySnake *int   `json:"output_dimensionality,omitempty"`
	}
	if err := kitutil.Unmarshal(data, &aux); err != nil {
		return err
	}
	*v = GeminiEmbeddingRequest(aux.Alias)
	if aux.TaskTypeSnake != "" {
		v.TaskType = aux.TaskTypeSnake
	}
	if aux.OutputDimensionalitySnake != nil {
		v.OutputDimensionality = *aux.OutputDimensionalitySnake
	}
	return nil
}
