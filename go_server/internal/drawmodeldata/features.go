package drawmodeldata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ExtractFeatures reconstructs the frozen numerical inputs from the same
// label-blind study projection used during model development. Target scores,
// status and result are never read; identity/time only exclude unsafe history.
func ExtractFeatures(data []byte) (map[string]float64, error) {
	var row map[string]interface{}
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("model input must be an object")
	}
	out := make(map[string]float64, 420)
	extractMarketFeatures(row, out)
	extractHistoryFeatures(row, out)
	return out, nil
}

func rawObject(value interface{}) map[string]interface{} {
	if object, ok := value.(map[string]interface{}); ok && object != nil {
		return object
	}
	return map[string]interface{}{}
}

func rawArray(value interface{}) []interface{} {
	values, _ := value.([]interface{})
	return values
}

func rawPath(value interface{}, keys ...string) interface{} {
	for _, key := range keys {
		value = rawObject(value)[key]
	}
	return value
}

func featureNumber(value interface{}) (float64, bool) {
	var result float64
	switch v := value.(type) {
	case float64:
		result = v
	case json.Number:
		var err error
		result, err = v.Float64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		result, err = strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return result, finite(result)
}

func featureTriple(value interface{}, positive bool) []float64 {
	values := rawArray(value)
	if len(values) != 3 {
		return nil
	}
	result := make([]float64, 3)
	for i, item := range values {
		number, valid := featureNumber(item)
		if !valid || (positive && number <= 0) {
			return nil
		}
		result[i] = number
	}
	return result
}

func putFeature(out map[string]float64, name string, value float64) {
	if finite(value) {
		out[name] = value
	}
}

func featureString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func featurePair(out map[string]float64, prefix string, a, b float64) {
	putFeature(out, prefix+".sum", a+b)
	putFeature(out, prefix+".absGap", math.Abs(a-b))
}
