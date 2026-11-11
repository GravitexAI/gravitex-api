package dto

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

var intValueType = reflect.TypeOf(IntValue(0))

type StringValue string

func (s *StringValue) UnmarshalJSON(data []byte) error {
	var str string
	if err := kitutil.Unmarshal(data, &str); err == nil {
		*s = StringValue(str)
		return nil
	}

	var raw json.Number
	if err := kitutil.Unmarshal(data, &raw); err == nil {
		*s = StringValue(raw.String())
		return nil
	}

	return kitutil.Unmarshal(data, &str)
}

func (s StringValue) MarshalJSON() ([]byte, error) {
	return kitutil.Marshal(string(s))
}

type IntValue int

func (i *IntValue) UnmarshalJSON(b []byte) error {
	var n int
	if err := kitutil.Unmarshal(b, &n); err == nil {
		*i = IntValue(n)
		return nil
	}
	// 部分上游（如 Meta /v1/responses）会把整数编码成浮点（例：1792987007.0）。
	// 仅接受整数值且落在 int 范围内的浮点：非整数浮点（例：2.5）依旧报错，
	// 以保持 doubao/seedance 等计费乘数字段的严格性；超出 int64 范围的
	// 浮点（例：1e300）也报错，避免 int64(f) 溢出为实现定义的垃圾值。
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		if f != math.Trunc(f) || f >= math.MaxInt64 || f < math.MinInt64 {
			return &json.UnmarshalTypeError{Value: "float out of int range or non-integral: " + strconv.FormatFloat(f, 'g', -1, 64), Type: intValueType}
		}
		*i = IntValue(int64(f))
		return nil
	}
	var s string
	if err := kitutil.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*i = IntValue(v)
	return nil
}

func (i IntValue) MarshalJSON() ([]byte, error) {
	return kitutil.Marshal(int(i))
}

type BoolValue bool

func (b *BoolValue) UnmarshalJSON(data []byte) error {
	var boolean bool
	if err := kitutil.Unmarshal(data, &boolean); err == nil {
		*b = BoolValue(boolean)
		return nil
	}
	var str string
	if err := kitutil.Unmarshal(data, &str); err != nil {
		return err
	}
	if str == "true" {
		*b = BoolValue(true)
	} else if str == "false" {
		*b = BoolValue(false)
	} else {
		return kitutil.Unmarshal(data, &boolean)
	}
	return nil
}
func (b BoolValue) MarshalJSON() ([]byte, error) {
	return kitutil.Marshal(bool(b))
}
