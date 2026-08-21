package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskPrivateDataPreservesCostDiscountSnapshot(t *testing.T) {
	var privateData TaskPrivateData
	require.NoError(t, common.UnmarshalJsonStr(`{"billing_context":{"cost_discount":0.75}}`, &privateData))

	encoded, err := common.Marshal(privateData)
	require.NoError(t, err)
	assert.JSONEq(t, `{"billing_context":{"cost_discount":0.75}}`, string(encoded))
}

func TestTaskPrivateDataOmitsUnsetCostDiscountSnapshot(t *testing.T) {
	privateData := TaskPrivateData{BillingContext: &TaskBillingContext{OriginModelName: "test-model"}}

	encoded, err := common.Marshal(privateData)
	require.NoError(t, err)
	assert.JSONEq(t, `{"billing_context":{"origin_model_name":"test-model"}}`, string(encoded))
}
